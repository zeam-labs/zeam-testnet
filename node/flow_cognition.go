

package node

import (
	"math"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)


type FlowDynamics struct {
	
	Pressure float64

	
	Tension float64

	
	Rhythm float64

	
	Coherence float64

	
	MeasuredAt time.Time
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
	fpd.current.Pressure = normalize(combinedRate, 0, 100) 

	
	fpd.txRateHistory = append(fpd.txRateHistory, combinedRate)
	if len(fpd.txRateHistory) > fpd.maxSamples {
		fpd.txRateHistory = fpd.txRateHistory[1:]
	}

	
	rateVariance := variance(fpd.txRateHistory)
	peerChurn := peerRate
	fpd.current.Tension = normalize(rateVariance+peerChurn*20, 0, 50)

	
	if len(fpd.txTimes) > 1 {
		intervals := make([]float64, 0, len(fpd.txTimes)-1)
		for i := 1; i < len(fpd.txTimes); i++ {
			intervals = append(intervals, fpd.txTimes[i].Sub(fpd.txTimes[i-1]).Seconds())
		}
		intervalVariance := variance(intervals)
		fpd.current.Rhythm = normalize(intervalVariance, 0, 1)
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


func normalize(val, min, max float64) float64 {
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


func variance(vals []float64) float64 {
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


type CognitiveState struct {
	
	Dynamics FlowDynamics
	Entropy  []byte

	
	ActiveConcepts []string
	ConceptWeights map[string]float64

	
	LastInjection     time.Time
	InjectionStrength float64

	
	ReadyToRespond bool
}


type ActiveCognition struct {
	mu sync.RWMutex

	
	harvester *FlowHarvester
	detector  *FlowPatternDetector

	
	state CognitiveState

	
	decayRate float64

	
	processInterval time.Duration
	running         bool
	stopCh          chan struct{}
	wg              sync.WaitGroup

	
	injections chan Injection

	
	outputs chan CognitiveOutput
}


type Injection struct {
	Type     InjectionType
	Content  string
	Concepts []string
	Strength float64 
}


type InjectionType int

const (
	InjectionQuery    InjectionType = iota 
	InjectionContext                       
	InjectionSteer                         
	InjectionFocus                         
)


type CognitiveOutput struct {
	Type       OutputType
	Content    string
	Concepts   []string
	Confidence float64
	Dynamics   FlowDynamics
	Timestamp  time.Time
}


type OutputType int

const (
	OutputResponse   OutputType = iota 
	OutputInsight                      
	OutputAssociation                  
)


func NewActiveCognition(harvester *FlowHarvester) *ActiveCognition {
	return &ActiveCognition{
		harvester:       harvester,
		detector:        NewFlowPatternDetector(),
		state: CognitiveState{
			ConceptWeights: make(map[string]float64),
		},
		decayRate:       0.95, 
		processInterval: 100 * time.Millisecond,
		stopCh:          make(chan struct{}),
		injections:      make(chan Injection, 10),
		outputs:         make(chan CognitiveOutput, 10),
	}
}


func (ac *ActiveCognition) Start() error {
	if ac.running {
		return nil
	}
	ac.running = true

	
	ac.wg.Add(1)
	go ac.processLoop()

	
	ac.wg.Add(1)
	go ac.flowMonitorLoop()

	return nil
}


func (ac *ActiveCognition) flowMonitorLoop() {
	defer ac.wg.Done()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var lastCount uint64

	for {
		select {
		case <-ac.stopCh:
			return
		case <-ticker.C:
			if ac.harvester == nil {
				continue
			}
			flow := ac.harvester.Flow()
			if flow == nil {
				continue
			}

			
			count, _, _ := flow.Stats()
			if count > lastCount {
				newHashes := int(count - lastCount)
				ac.detector.RecordTxBatch(newHashes)
				lastCount = count
			}
		}
	}
}


func (ac *ActiveCognition) Stop() {
	if !ac.running {
		return
	}
	ac.running = false
	close(ac.stopCh)
	ac.wg.Wait()
}


func (ac *ActiveCognition) processLoop() {
	defer ac.wg.Done()

	ticker := time.NewTicker(ac.processInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ac.stopCh:
			return

		case injection := <-ac.injections:
			ac.handleInjection(injection)

		case <-ticker.C:
			ac.processCycle()
		}
	}
}


func (ac *ActiveCognition) processCycle() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	
	ac.state.Dynamics = ac.detector.Dynamics()

	
	if ac.harvester != nil {
		ac.state.Entropy = ac.harvester.Flow().Entropy()
	}

	
	for concept, weight := range ac.state.ConceptWeights {
		newWeight := weight * ac.decayRate
		if newWeight < 0.01 {
			delete(ac.state.ConceptWeights, concept)
		} else {
			ac.state.ConceptWeights[concept] = newWeight
		}
	}

	
	ac.state.ActiveConcepts = ac.topConcepts(10)

	
	ac.state.ReadyToRespond = ac.state.Dynamics.Pressure > 0.3 && len(ac.state.ActiveConcepts) > 0
}


func (ac *ActiveCognition) handleInjection(inj Injection) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	now := time.Now()
	ac.state.LastInjection = now
	ac.state.InjectionStrength = inj.Strength

	
	for _, concept := range inj.Concepts {
		current := ac.state.ConceptWeights[concept]
		
		ac.state.ConceptWeights[concept] = math.Min(1.0, current+inj.Strength)
	}

	
	switch inj.Type {
	case InjectionQuery:
		ac.generateResponse(inj)
	case InjectionContext:
		
	case InjectionSteer:
		
	case InjectionFocus:
		
		for _, concept := range inj.Concepts {
			ac.state.ConceptWeights[concept] = 1.0
		}
	}
}


func (ac *ActiveCognition) generateResponse(inj Injection) {
	
	dynamics := ac.state.Dynamics

	
	confidence := dynamics.Coherence * 0.5
	if len(ac.state.ActiveConcepts) > 0 {
		confidence += 0.5
	}

	output := CognitiveOutput{
		Type:       OutputResponse,
		Content:    inj.Content, 
		Concepts:   ac.topConcepts(5),
		Confidence: confidence,
		Dynamics:   dynamics,
		Timestamp:  time.Now(),
	}

	
	select {
	case ac.outputs <- output:
	default:
		
	}
}


func (ac *ActiveCognition) topConcepts(n int) []string {
	type conceptWeight struct {
		concept string
		weight  float64
	}

	
	cws := make([]conceptWeight, 0, len(ac.state.ConceptWeights))
	for c, w := range ac.state.ConceptWeights {
		cws = append(cws, conceptWeight{c, w})
	}

	
	result := make([]string, 0, n)
	for i := 0; i < n && i < len(cws); i++ {
		maxIdx := i
		for j := i + 1; j < len(cws); j++ {
			if cws[j].weight > cws[maxIdx].weight {
				maxIdx = j
			}
		}
		cws[i], cws[maxIdx] = cws[maxIdx], cws[i]
		result = append(result, cws[i].concept)
	}

	return result
}


func (ac *ActiveCognition) Inject(inj Injection) {
	select {
	case ac.injections <- inj:
	default:
		
	}
}


func (ac *ActiveCognition) Query(content string, concepts []string) {
	ac.Inject(Injection{
		Type:     InjectionQuery,
		Content:  content,
		Concepts: concepts,
		Strength: 0.8,
	})
}


func (ac *ActiveCognition) Steer(concepts []string, strength float64) {
	ac.Inject(Injection{
		Type:     InjectionSteer,
		Concepts: concepts,
		Strength: strength,
	})
}


func (ac *ActiveCognition) Focus(concepts []string) {
	ac.Inject(Injection{
		Type:     InjectionFocus,
		Concepts: concepts,
		Strength: 1.0,
	})
}


func (ac *ActiveCognition) Outputs() <-chan CognitiveOutput {
	return ac.outputs
}


func (ac *ActiveCognition) State() CognitiveState {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	
	state := ac.state
	state.ActiveConcepts = make([]string, len(ac.state.ActiveConcepts))
	copy(state.ActiveConcepts, ac.state.ActiveConcepts)
	state.ConceptWeights = make(map[string]float64)
	for k, v := range ac.state.ConceptWeights {
		state.ConceptWeights[k] = v
	}

	return state
}


func (ac *ActiveCognition) Dynamics() FlowDynamics {
	return ac.detector.Dynamics()
}


func (d FlowDynamics) ToPressureContext() interface{} {
	
	return struct {
		Magnitude float64
		Coherence float64
		Tension   float64
		Density   float64
	}{
		Magnitude: d.Pressure,
		Coherence: d.Coherence,
		Tension:   d.Tension,
		Density:   d.Rhythm,
	}
}


type FlowComputeAdapter struct {
	compute *FlowCompute
}


func NewFlowComputeAdapter(compute *FlowCompute) *FlowComputeAdapter {
	return &FlowComputeAdapter{compute: compute}
}


func (fca *FlowComputeAdapter) NextBytes(n int) []byte {
	return fca.compute.NextBytes(n)
}


func (fca *FlowComputeAdapter) NextUint64() uint64 {
	return fca.compute.NextUint64()
}


func (fca *FlowComputeAdapter) SelectIndex(n int) int {
	return fca.compute.SelectIndex(n)
}


type MixedEntropy struct {
	flowEntropy      []byte
	injectionEntropy []byte
	mixed            []byte
}


func NewMixedEntropy(flow *MempoolFlow, injection string) *MixedEntropy {
	me := &MixedEntropy{
		flowEntropy:      flow.Entropy(),
		injectionEntropy: crypto.Keccak256([]byte(injection)),
	}

	
	me.mixed = make([]byte, 32)
	for i := 0; i < 32; i++ {
		me.mixed[i] = me.flowEntropy[i] ^ me.injectionEntropy[i]
	}

	return me
}


func (me *MixedEntropy) Bytes() []byte {
	return me.mixed
}


func (me *MixedEntropy) Hash() common.Hash {
	return crypto.Keccak256Hash(me.mixed)
}
