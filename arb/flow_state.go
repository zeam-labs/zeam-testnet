package arb

import (
	"math/big"
	"sync"
	"time"

	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
)

type FlowSignalType int

const (
	SignalPressureSpike    FlowSignalType = iota
	SignalPressureGradient
	SignalCoherenceDrift
	SignalSwapSequence
	SignalPoolImpact
	SignalFlashblockConfirm
)

type FlowSignal struct {
	Type      FlowSignalType
	Timestamp time.Time
	ChainID   uint64

	Pressure quantum.PressureMetrics
	Gradient PressureGradient

	PendingSwaps  []*PendingSwap
	AffectedPools []common.Address

	PoolImpacts map[common.Address]*PoolImpactForecast

	ChainCoherence map[uint64]float64
}

type PressureGradient struct {
	DPressureDt  float64
	DCoherenceDt float64
	DTensionDt   float64
	DDensityDt   float64
	Window       time.Duration
}

func (g PressureGradient) IsAccelerating() bool {
	return g.DPressureDt > 0
}

type PressureSample struct {
	Timestamp time.Time
	Pressure  quantum.PressureMetrics
	FlowRate  float64
}

type PressureTimeSeries struct {
	mu      sync.Mutex
	samples []PressureSample
	maxAge  time.Duration
}

func NewPressureTimeSeries(maxAge time.Duration) *PressureTimeSeries {
	return &PressureTimeSeries{
		samples: make([]PressureSample, 0, 256),
		maxAge:  maxAge,
	}
}

func (ts *PressureTimeSeries) Add(s PressureSample) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.samples = append(ts.samples, s)
	ts.prune(s.Timestamp)
}

func (ts *PressureTimeSeries) Gradient() PressureGradient {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	n := len(ts.samples)
	if n < 2 {
		return PressureGradient{Window: ts.maxAge}
	}

	first := ts.samples[0].Timestamp
	last := ts.samples[n-1].Timestamp
	window := last.Sub(first)
	if window <= 0 {
		return PressureGradient{Window: ts.maxAge}
	}

	var sumX, sumP, sumC, sumT, sumD float64
	for _, s := range ts.samples {
		x := s.Timestamp.Sub(first).Seconds()
		sumX += x
		sumP += s.Pressure.Hadamard
		sumC += s.Pressure.PauliX
		sumT += s.Pressure.PauliZ
		sumD += s.Pressure.Phase
	}

	fn := float64(n)
	meanX := sumX / fn
	meanP := sumP / fn
	meanC := sumC / fn
	meanT := sumT / fn
	meanD := sumD / fn

	var numP, numC, numT, numD, denomX float64
	for _, s := range ts.samples {
		dx := s.Timestamp.Sub(first).Seconds() - meanX
		denomX += dx * dx
		numP += dx * (s.Pressure.Hadamard - meanP)
		numC += dx * (s.Pressure.PauliX - meanC)
		numT += dx * (s.Pressure.PauliZ - meanT)
		numD += dx * (s.Pressure.Phase - meanD)
	}

	if denomX == 0 {
		return PressureGradient{Window: window}
	}

	return PressureGradient{
		DPressureDt:  numP / denomX,
		DCoherenceDt: numC / denomX,
		DTensionDt:   numT / denomX,
		DDensityDt:   numD / denomX,
		Window:       window,
	}
}

func (ts *PressureTimeSeries) Latest() PressureSample {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.samples) == 0 {
		return PressureSample{}
	}
	return ts.samples[len(ts.samples)-1]
}

func (ts *PressureTimeSeries) prune(now time.Time) {
	cutoff := now.Add(-ts.maxAge)
	i := 0
	for i < len(ts.samples) && ts.samples[i].Timestamp.Before(cutoff) {
		i++
	}
	if i > 0 {
		copy(ts.samples, ts.samples[i:])
		ts.samples = ts.samples[:len(ts.samples)-i]
	}
}

type PendingSwapSequence struct {
	mu      sync.RWMutex
	byChain map[uint64][]*PendingSwap
	byPool  map[common.Address][]*PendingSwap
	maxAge  time.Duration
	seqNum  uint64
}

func NewPendingSwapSequence(maxAge time.Duration) *PendingSwapSequence {
	return &PendingSwapSequence{
		byChain: make(map[uint64][]*PendingSwap),
		byPool:  make(map[common.Address][]*PendingSwap),
		maxAge:  maxAge,
	}
}

func (pss *PendingSwapSequence) Add(swap *PendingSwap) {
	pss.mu.Lock()
	defer pss.mu.Unlock()

	pss.seqNum++
	pss.byChain[swap.ChainID] = append(pss.byChain[swap.ChainID], swap)
	if swap.PoolAddress != (common.Address{}) {
		pss.byPool[swap.PoolAddress] = append(pss.byPool[swap.PoolAddress], swap)
	}
}

func (pss *PendingSwapSequence) ForPool(pool common.Address) []*PendingSwap {
	pss.mu.RLock()
	defer pss.mu.RUnlock()
	out := make([]*PendingSwap, len(pss.byPool[pool]))
	copy(out, pss.byPool[pool])
	return out
}

func (pss *PendingSwapSequence) ForChain(chainID uint64) []*PendingSwap {
	pss.mu.RLock()
	defer pss.mu.RUnlock()
	out := make([]*PendingSwap, len(pss.byChain[chainID]))
	copy(out, pss.byChain[chainID])
	return out
}

func (pss *PendingSwapSequence) CountByChain() map[uint64]int {
	pss.mu.RLock()
	defer pss.mu.RUnlock()
	counts := make(map[uint64]int, len(pss.byChain))
	for k, v := range pss.byChain {
		counts[k] = len(v)
	}
	return counts
}

func (pss *PendingSwapSequence) Prune(now time.Time) {
	pss.mu.Lock()
	defer pss.mu.Unlock()

	cutoff := now.Add(-pss.maxAge)

	for chainID, swaps := range pss.byChain {
		pss.byChain[chainID] = pruneSwaps(swaps, cutoff)
	}
	for pool, swaps := range pss.byPool {
		pss.byPool[pool] = pruneSwaps(swaps, cutoff)
	}
}

func (pss *PendingSwapSequence) ConfirmTx(txHash common.Hash) {
	pss.mu.Lock()
	defer pss.mu.Unlock()

	for chainID, swaps := range pss.byChain {
		pss.byChain[chainID] = removeTx(swaps, txHash)
	}
	for pool, swaps := range pss.byPool {
		pss.byPool[pool] = removeTx(swaps, txHash)
	}
}

func pruneSwaps(swaps []*PendingSwap, cutoff time.Time) []*PendingSwap {
	i := 0
	for i < len(swaps) && swaps[i].DetectedAt.Before(cutoff) {
		i++
	}
	if i == 0 {
		return swaps
	}
	return swaps[i:]
}

func removeTx(swaps []*PendingSwap, txHash common.Hash) []*PendingSwap {
	for i, s := range swaps {
		if s.TxHash == txHash {
			return append(swaps[:i], swaps[i+1:]...)
		}
	}
	return swaps
}

type PoolImpactForecast struct {
	Pool                common.Address
	ChainID             uint64
	CurrentReserves     [2]*big.Int
	PredictedReserves   [2]*big.Int
	PendingSwapCount    int
	CumulativeImpactBPS float64
	PredictedPrice      *big.Float
	Confidence          float64
	LastUpdated         time.Time
}

type FlowState struct {
	Timestamp      time.Time
	Pressure       quantum.PressureMetrics
	Gradient       PressureGradient
	PendingByChain map[uint64]int
	PoolForecasts  map[common.Address]*PoolImpactForecast
	ChainCoherence map[uint64]float64
	FlowRate       float64
}
