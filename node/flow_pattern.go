package node

import (
	"sync"
	"time"
)

type FlowDynamics struct {

	Pressure float64

	Tension float64

	Rhythm float64

	Coherence float64

	MeasuredAt time.Time
}

func (d FlowDynamics) ToPressureContext() interface{} {
	return struct {
		Hadamard float64
		PauliX   float64
		PauliZ   float64
		Phase    float64
	}{
		Hadamard: d.Pressure,
		PauliX:   d.Coherence,
		PauliZ:   d.Tension,
		Phase:    d.Rhythm,
	}
}

type FlowPatternDetector struct {
	mu sync.Mutex

	txTimes    []time.Time
	peerEvents []time.Time

	windowSize    time.Duration
	sampleCount   int
	maxSamples    int

	lastTxRate    float64
	txRateHistory []float64

	connectCount  int
	dropCount     int
	peerHistory   []int

	current FlowDynamics
}

func NewFlowPatternDetector() *FlowPatternDetector {
	return &FlowPatternDetector{
		txTimes:       make([]time.Time, 0, 1000),
		peerEvents:    make([]time.Time, 0, 100),
		windowSize:    5 * time.Second,
		maxSamples:    100,
		txRateHistory: make([]float64, 0, 100),
		peerHistory:   make([]int, 0, 100),
	}
}

func (fpd *FlowPatternDetector) RecordTxBatch(count int) {
	fpd.mu.Lock()
	defer fpd.mu.Unlock()

	now := time.Now()
	for i := 0; i < count; i++ {
		fpd.txTimes = append(fpd.txTimes, now)
	}

	fpd.trimOldEvents(now)
	fpd.updateDynamics(now)
}

func (fpd *FlowPatternDetector) RecordPeerConnect() {
	fpd.mu.Lock()
	defer fpd.mu.Unlock()

	now := time.Now()
	fpd.connectCount++
	fpd.peerEvents = append(fpd.peerEvents, now)
	fpd.trimOldEvents(now)
	fpd.updateDynamics(now)
}

func (fpd *FlowPatternDetector) RecordPeerDrop() {
	fpd.mu.Lock()
	defer fpd.mu.Unlock()

	now := time.Now()
	fpd.dropCount++
	fpd.peerEvents = append(fpd.peerEvents, now)
	fpd.trimOldEvents(now)
	fpd.updateDynamics(now)
}

func (fpd *FlowPatternDetector) trimOldEvents(now time.Time) {
	cutoff := now.Add(-fpd.windowSize)

	newTxTimes := make([]time.Time, 0, len(fpd.txTimes))
	for _, t := range fpd.txTimes {
		if t.After(cutoff) {
			newTxTimes = append(newTxTimes, t)
		}
	}
	fpd.txTimes = newTxTimes

	newPeerEvents := make([]time.Time, 0, len(fpd.peerEvents))
	for _, t := range fpd.peerEvents {
		if t.After(cutoff) {
			newPeerEvents = append(newPeerEvents, t)
		}
	}
	fpd.peerEvents = newPeerEvents
}

func (fpd *FlowPatternDetector) updateDynamics(now time.Time) {
	windowSecs := fpd.windowSize.Seconds()

	txRate := float64(len(fpd.txTimes)) / windowSecs
	peerRate := float64(len(fpd.peerEvents)) / windowSecs
	combinedRate := txRate + (peerRate * 50)
	fpd.current.Pressure = flowNormalize(combinedRate, 0, 100)

	fpd.txRateHistory = append(fpd.txRateHistory, combinedRate)
	if len(fpd.txRateHistory) > fpd.maxSamples {
		fpd.txRateHistory = fpd.txRateHistory[1:]
	}

	rateVariance := flowVariance(fpd.txRateHistory)
	peerChurn := peerRate
	fpd.current.Tension = flowNormalize(rateVariance+peerChurn*20, 0, 50)

	if len(fpd.txTimes) > 1 {
		intervals := make([]float64, 0, len(fpd.txTimes)-1)
		for i := 1; i < len(fpd.txTimes); i++ {
			intervals = append(intervals, fpd.txTimes[i].Sub(fpd.txTimes[i-1]).Seconds())
		}
		intervalVariance := flowVariance(intervals)
		fpd.current.Rhythm = flowNormalize(intervalVariance, 0, 1)
	}

	fpd.current.Coherence = 1.0 - fpd.current.Tension
	if fpd.current.Coherence < 0 {
		fpd.current.Coherence = 0
	}

	fpd.current.MeasuredAt = now
	fpd.lastTxRate = txRate
}

func (fpd *FlowPatternDetector) Dynamics() FlowDynamics {
	fpd.mu.Lock()
	defer fpd.mu.Unlock()
	return fpd.current
}

func flowNormalize(val, min, max float64) float64 {
	if max <= min {
		return 0
	}
	n := (val - min) / (max - min)
	if n < 0 {
		return 0
	}
	if n > 1 {
		return 1
	}
	return n
}

func flowVariance(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	var sum, sumSq float64
	for _, v := range vals {
		sum += v
		sumSq += v * v
	}
	n := float64(len(vals))
	mean := sum / n
	return (sumSq / n) - (mean * mean)
}
