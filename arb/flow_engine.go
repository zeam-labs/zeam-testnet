package arb

import (
	"log"
	"math/big"
	"sync"
	"time"

	"zeam/node"
	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type FlowEngineConfig struct {

	GradientWindow time.Duration

	PressureSpikeThreshold  float64
	GradientThreshold       float64
	CoherenceDriftThreshold float64
	PoolImpactThresholdBPS  float64

	MaxPendingAge     time.Duration
	MaxPendingPerPool int

	FlowBufferSize int

	EnableLogging bool
}

func DefaultFlowEngineConfig() *FlowEngineConfig {
	return &FlowEngineConfig{
		GradientWindow:          2 * time.Second,
		PressureSpikeThreshold:  0.7,
		GradientThreshold:       0.3,
		CoherenceDriftThreshold: 0.4,
		PoolImpactThresholdBPS:  50,
		MaxPendingAge:           30 * time.Second,
		MaxPendingPerPool:       100,
		FlowBufferSize:          1024,
		EnableLogging:           false,
	}
}

type FlowPressureEngine struct {
	mu sync.RWMutex

	feed    node.NetworkFeed
	feedSub *node.FeedSubscription

	parser         *MempoolParser
	quantumService *quantum.QuantumService

	pendingSequence *PendingSwapSequence
	pressureHistory *PressureTimeSeries
	poolForecasts   map[common.Address]*PoolImpactForecast
	chainCoherence  map[uint64]float64

	chainPressure map[uint64]quantum.PressureMetrics

	subscriberMu sync.RWMutex
	subscribers  []chan<- FlowSignal

	config *FlowEngineConfig

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func NewFlowPressureEngine(
	feed node.NetworkFeed,
	parser *MempoolParser,
	qs *quantum.QuantumService,
	config *FlowEngineConfig,
) *FlowPressureEngine {
	if config == nil {
		config = DefaultFlowEngineConfig()
	}

	return &FlowPressureEngine{
		feed:            feed,
		parser:          parser,
		quantumService:  qs,
		pendingSequence: NewPendingSwapSequence(config.MaxPendingAge),
		pressureHistory: NewPressureTimeSeries(config.GradientWindow),
		poolForecasts:   make(map[common.Address]*PoolImpactForecast),
		chainCoherence:  make(map[uint64]float64),
		chainPressure:   make(map[uint64]quantum.PressureMetrics),
		config:          config,
		stopCh:          make(chan struct{}),
	}
}

func (e *FlowPressureEngine) Start() error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = true
	e.mu.Unlock()

	e.feedSub = e.feed.Subscribe(e.config.FlowBufferSize)

	e.wg.Add(1)
	go e.eventLoop()

	if e.config.EnableLogging {
		log.Println("[FlowEngine] Started")
	}
	return nil
}

func (e *FlowPressureEngine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	close(e.stopCh)
	e.mu.Unlock()

	if e.feedSub != nil {
		e.feedSub.Unsubscribe()
	}
	e.wg.Wait()

	e.subscriberMu.Lock()
	for _, ch := range e.subscribers {
		close(ch)
	}
	e.subscribers = nil
	e.subscriberMu.Unlock()

	if e.config.EnableLogging {
		log.Println("[FlowEngine] Stopped")
	}
}

func (e *FlowPressureEngine) Subscribe(bufSize int) <-chan FlowSignal {
	ch := make(chan FlowSignal, bufSize)
	e.subscriberMu.Lock()
	e.subscribers = append(e.subscribers, ch)
	e.subscriberMu.Unlock()
	return ch
}

func (e *FlowPressureEngine) GetState() *FlowState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	forecasts := make(map[common.Address]*PoolImpactForecast, len(e.poolForecasts))
	for k, v := range e.poolForecasts {
		forecasts[k] = v
	}

	coherence := make(map[uint64]float64, len(e.chainCoherence))
	for k, v := range e.chainCoherence {
		coherence[k] = v
	}

	latest := e.pressureHistory.Latest()

	return &FlowState{
		Timestamp:      time.Now(),
		Pressure:       latest.Pressure,
		Gradient:       e.pressureHistory.Gradient(),
		PendingByChain: e.pendingSequence.CountByChain(),
		PoolForecasts:  forecasts,
		ChainCoherence: coherence,
		FlowRate:       latest.FlowRate,
	}
}

func (e *FlowPressureEngine) GetPoolForecast(pool common.Address) *PoolImpactForecast {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.poolForecasts[pool]
}

func (e *FlowPressureEngine) GetPressureGradient() PressureGradient {
	return e.pressureHistory.Gradient()
}

func (e *FlowPressureEngine) eventLoop() {
	defer e.wg.Done()

	var lastPrune time.Time
	eventCount := 0

	for event := range e.feedSub.C {

		switch event.Type {
		case node.EventTxFull:
			e.handleFullTxs(event.ChainID, event.Txs)

		case node.EventTxHashes:
			e.handleTxHashes(event.ChainID, event.Hashes)

		case node.EventL1Block:
			e.handleBlock(event.ChainID, event.L1BlockNumber)

		case node.EventL2Block:
			if event.L2Block != nil {
				e.handleBlock(event.ChainID, event.L2Block.BlockNumber)
			}
		}

		e.samplePressure()
		e.checkGradientSignals()

		eventCount++
		now := time.Now()
		if eventCount >= 100 || now.Sub(lastPrune) > 10*time.Second {
			e.checkCoherenceDrift()
			e.pendingSequence.Prune(now)
			lastPrune = now
			eventCount = 0
		}
	}
}

func (e *FlowPressureEngine) handleFullTxs(chainID uint64, txs []*types.Transaction) {
	if e.parser == nil {
		return
	}

	for _, tx := range txs {
		swap := e.parser.ParseTransaction(chainID, tx)
		if swap == nil {
			continue
		}

		e.pendingSequence.Add(swap)
		e.updatePoolForecast(swap)

		if swap.PoolAddress != (common.Address{}) {
			e.checkPoolImpactSignal(swap.PoolAddress, chainID)
		}

		if swap.PoolAddress != (common.Address{}) {
			poolSwaps := e.pendingSequence.ForPool(swap.PoolAddress)
			if len(poolSwaps) >= 2 {
				e.emitSignal(FlowSignal{
					Type:          SignalSwapSequence,
					Timestamp:     time.Now(),
					ChainID:       chainID,
					Pressure:      e.currentPressure(),
					Gradient:      e.pressureHistory.Gradient(),
					PendingSwaps:  poolSwaps,
					AffectedPools: []common.Address{swap.PoolAddress},
				})
			}
		}
	}
}

func (e *FlowPressureEngine) handleTxHashes(chainID uint64, hashes []common.Hash) {

	if e.quantumService != nil {
		netType := quantum.ChainIDToNetworkType(chainID)
		for _, h := range hashes {
			e.quantumService.OnHashReceived(netType, h[:])
		}
	}
}

func (e *FlowPressureEngine) handleBlock(chainID uint64, blockNumber uint64) {

	if e.quantumService != nil {
		e.mu.Lock()
		e.chainPressure[chainID] = e.quantumService.GetPressure()
		e.mu.Unlock()
	}
}

func (e *FlowPressureEngine) updatePoolForecast(swap *PendingSwap) {
	if swap.PoolAddress == (common.Address{}) {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	forecast, exists := e.poolForecasts[swap.PoolAddress]
	if !exists {
		forecast = &PoolImpactForecast{
			Pool:    swap.PoolAddress,
			ChainID: swap.ChainID,
		}
		e.poolForecasts[swap.PoolAddress] = forecast
	}

	forecast.PendingSwapCount++
	forecast.CumulativeImpactBPS += swap.EstimatedPriceImpact
	forecast.LastUpdated = time.Now()

	if forecast.PendingSwapCount <= 1 {
		forecast.Confidence = 0.9
	} else if forecast.PendingSwapCount <= 3 {
		forecast.Confidence = 0.7
	} else if forecast.PendingSwapCount <= 5 {
		forecast.Confidence = 0.5
	} else {
		forecast.Confidence = 0.3
	}

	if forecast.CurrentReserves[0] != nil && forecast.CurrentReserves[1] != nil &&
		forecast.CurrentReserves[0].Sign() > 0 {

		currentPrice := new(big.Float).Quo(
			new(big.Float).SetInt(forecast.CurrentReserves[1]),
			new(big.Float).SetInt(forecast.CurrentReserves[0]),
		)

		shift := 1.0 + forecast.CumulativeImpactBPS/10000.0
		forecast.PredictedPrice = new(big.Float).Mul(currentPrice, big.NewFloat(shift))
	}
}

func (e *FlowPressureEngine) samplePressure() {
	pressure := e.currentPressure()
	flowRate := e.currentFlowRate()

	e.pressureHistory.Add(PressureSample{
		Timestamp: time.Now(),
		Pressure:  pressure,
		FlowRate:  flowRate,
	})

	if pressure.Hadamard > e.config.PressureSpikeThreshold {
		e.emitSignal(FlowSignal{
			Type:      SignalPressureSpike,
			Timestamp: time.Now(),
			Pressure:  pressure,
			Gradient:  e.pressureHistory.Gradient(),
		})
	}
}

func (e *FlowPressureEngine) checkGradientSignals() {
	gradient := e.pressureHistory.Gradient()

	if gradient.DPressureDt > e.config.GradientThreshold ||
		gradient.DPressureDt < -e.config.GradientThreshold {
		e.emitSignal(FlowSignal{
			Type:      SignalPressureGradient,
			Timestamp: time.Now(),
			Pressure:  e.currentPressure(),
			Gradient:  gradient,
		})
	}
}

func (e *FlowPressureEngine) checkCoherenceDrift() {
	e.mu.RLock()
	chains := make(map[uint64]quantum.PressureMetrics, len(e.chainPressure))
	for k, v := range e.chainPressure {
		chains[k] = v
	}
	e.mu.RUnlock()

	if len(chains) < 2 {
		return
	}

	var totalCoherence float64
	for _, p := range chains {
		totalCoherence += p.PauliX
	}
	meanCoherence := totalCoherence / float64(len(chains))

	coherenceMap := make(map[uint64]float64, len(chains))
	drifting := false
	for chainID, p := range chains {
		coherenceMap[chainID] = p.PauliX
		if p.PauliX < e.config.CoherenceDriftThreshold && meanCoherence > e.config.CoherenceDriftThreshold {
			drifting = true
		}
	}

	e.mu.Lock()
	e.chainCoherence = coherenceMap
	e.mu.Unlock()

	if drifting {
		e.emitSignal(FlowSignal{
			Type:           SignalCoherenceDrift,
			Timestamp:      time.Now(),
			Pressure:       e.currentPressure(),
			Gradient:       e.pressureHistory.Gradient(),
			ChainCoherence: coherenceMap,
		})
	}
}

func (e *FlowPressureEngine) checkPoolImpactSignal(pool common.Address, chainID uint64) {
	e.mu.RLock()
	forecast := e.poolForecasts[pool]
	e.mu.RUnlock()

	if forecast == nil {
		return
	}

	if forecast.CumulativeImpactBPS >= e.config.PoolImpactThresholdBPS {
		impacts := map[common.Address]*PoolImpactForecast{pool: forecast}
		e.emitSignal(FlowSignal{
			Type:          SignalPoolImpact,
			Timestamp:     time.Now(),
			ChainID:       chainID,
			Pressure:      e.currentPressure(),
			Gradient:      e.pressureHistory.Gradient(),
			AffectedPools: []common.Address{pool},
			PoolImpacts:   impacts,
		})
	}
}

func (e *FlowPressureEngine) currentPressure() quantum.PressureMetrics {
	if e.quantumService != nil {
		return e.quantumService.GetPressure()
	}
	return quantum.PressureMetrics{Hadamard: 0.5, PauliX: 0.5, PauliZ: 0.3, Phase: 0.5}
}

func (e *FlowPressureEngine) currentFlowRate() float64 {
	stats := e.feed.Stats()
	return stats.MempoolRate
}

func (e *FlowPressureEngine) emitSignal(signal FlowSignal) {
	e.subscriberMu.RLock()
	defer e.subscriberMu.RUnlock()

	for _, ch := range e.subscribers {
		select {
		case ch <- signal:
		default:

		}
	}
}
