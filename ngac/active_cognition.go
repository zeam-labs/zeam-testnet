package ngac

import (
	"math"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"zeam/node"
)

type CognitiveState struct {
	Dynamics node.FlowDynamics
	Entropy  []byte

	ActiveConcepts []string
	ConceptWeights map[string]float64

	LastInjection     time.Time
	InjectionStrength float64

	ReadyToRespond bool
}

type ActiveCognition struct {
	mu sync.RWMutex

	feed     node.NetworkFeed
	sub      *node.FeedSubscription
	detector *node.FlowPatternDetector

	entropyPool [32]byte

	state CognitiveState

	decayRate       float64
	processInterval time.Duration
	running         bool
	stopCh          chan struct{}
	wg              sync.WaitGroup

	injections chan Injection
	outputs    chan CognitiveOutput
}

type Injection struct {
	Type     InjectionType
	Content  string
	Concepts []string
	Strength float64
}

type InjectionType int

const (
	InjectionQuery   InjectionType = iota
	InjectionContext
	InjectionSteer
	InjectionFocus
)

type CognitiveOutput struct {
	Type       OutputType
	Content    string
	Concepts   []string
	Confidence float64
	Dynamics   node.FlowDynamics
	Timestamp  time.Time
}

type OutputType int

const (
	OutputResponse    OutputType = iota
	OutputInsight
	OutputAssociation
)

func NewActiveCognition(feed node.NetworkFeed) *ActiveCognition {
	return &ActiveCognition{
		feed:     feed,
		detector: node.NewFlowPatternDetector(),
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

	ac.sub = ac.feed.Subscribe(256)

	ac.wg.Add(1)
	go ac.processLoop()

	ac.wg.Add(1)
	go ac.flowMonitorLoop()

	return nil
}

func (ac *ActiveCognition) flowMonitorLoop() {
	defer ac.wg.Done()

	for {
		select {
		case <-ac.stopCh:
			return
		case event, ok := <-ac.sub.C:
			if !ok {
				return
			}
			switch event.Type {
			case node.EventTxHashes:
				ac.detector.RecordTxBatch(len(event.Hashes))

				ac.mu.Lock()
				for _, h := range event.Hashes {
					hBytes := h.Bytes()
					for i := 0; i < 32; i++ {
						ac.entropyPool[i] ^= hBytes[i]
					}
				}
				ac.mu.Unlock()
			case node.EventPeerConnect:
				ac.detector.RecordPeerConnect()
			case node.EventPeerDrop:
				ac.detector.RecordPeerDrop()
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
	if ac.sub != nil {
		ac.sub.Unsubscribe()
	}
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
	ac.state.Entropy = ac.entropyPool[:]

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

	ac.state.LastInjection = time.Now()
	ac.state.InjectionStrength = inj.Strength

	for _, concept := range inj.Concepts {
		current := ac.state.ConceptWeights[concept]
		ac.state.ConceptWeights[concept] = math.Min(1.0, current+inj.Strength)
	}

	switch inj.Type {
	case InjectionQuery:
		ac.generateResponse(inj)
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

func (ac *ActiveCognition) Dynamics() node.FlowDynamics {
	return ac.detector.Dynamics()
}

type FlowComputeAdapter struct {
	compute *node.FlowCompute
}

func NewFlowComputeAdapter(compute *node.FlowCompute) *FlowComputeAdapter {
	return &FlowComputeAdapter{compute: compute}
}

func (fca *FlowComputeAdapter) NextBytes(n int) []byte  { return fca.compute.NextBytes(n) }
func (fca *FlowComputeAdapter) NextUint64() uint64       { return fca.compute.NextUint64() }
func (fca *FlowComputeAdapter) SelectIndex(n int) int    { return fca.compute.SelectIndex(n) }

type MixedEntropy struct {
	flowEntropy      []byte
	injectionEntropy []byte
	mixed            []byte
}

func NewMixedEntropy(flow *node.MempoolFlow, injection string) *MixedEntropy {
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

func (me *MixedEntropy) Bytes() []byte     { return me.mixed }
func (me *MixedEntropy) Hash() common.Hash { return crypto.Keccak256Hash(me.mixed) }
