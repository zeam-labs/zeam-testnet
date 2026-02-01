package ngac

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"zeam/node"
)

type UnifiedCognition struct {
	mu sync.RWMutex

	feed node.NetworkFeed
	sub  *node.FeedSubscription

	detector *node.FlowPatternDetector

	entropyPool [32]byte
	poolVersion uint64

	concepts       map[string]float64
	conceptDecay   float64
	conceptHistory []string

	dynamics UnifiedDynamics

	pendingWork chan WorkItem

	outputs chan CognitiveOutput

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type UnifiedDynamics struct {
	FlowPressure  float64
	FlowTension   float64
	FlowRhythm    float64
	FlowCoherence float64

	BroadcastPending int
	BroadcastRate    float64
	LastBroadcast    time.Time

	UserActivity  float64
	LastUserInput time.Time
	ActiveQuery   bool
	QueryContent  string

	TotalPressure float64
	ReadyToAct    bool
}

type WorkItem struct {
	Type     WorkType
	Payload  []byte
	Concepts []string
	Priority float64
	Created  time.Time
	Callback func(hashes []common.Hash)
}

type WorkType int

const (
	WorkQuery      WorkType = iota
	WorkGenerate
	WorkAssociate
	WorkBackground
)

func NewUnifiedCognition(feed node.NetworkFeed) *UnifiedCognition {
	return &UnifiedCognition{
		feed:           feed,
		detector:       node.NewFlowPatternDetector(),
		concepts:       make(map[string]float64),
		conceptDecay:   0.95,
		conceptHistory: make([]string, 0, 100),
		pendingWork:    make(chan WorkItem, 100),
		outputs:        make(chan CognitiveOutput, 10),
		stopCh:         make(chan struct{}),
	}
}

func (uc *UnifiedCognition) Start() error {
	if uc.running {
		return nil
	}
	uc.running = true

	uc.sub = uc.feed.Subscribe(256)

	uc.wg.Add(1)
	go uc.flowMonitorLoop()

	uc.wg.Add(1)
	go uc.cognitionLoop()

	uc.wg.Add(1)
	go uc.workLoop()

	return nil
}

func (uc *UnifiedCognition) flowMonitorLoop() {
	defer uc.wg.Done()

	for {
		select {
		case <-uc.stopCh:
			return
		case event, ok := <-uc.sub.C:
			if !ok {
				return
			}
			switch event.Type {
			case node.EventTxHashes:
				uc.detector.RecordTxBatch(len(event.Hashes))
				uc.mu.Lock()
				for _, h := range event.Hashes {
					hBytes := h.Bytes()
					for i := 0; i < 32; i++ {
						uc.entropyPool[i] ^= hBytes[i]
					}
				}
				uc.poolVersion++
				uc.mu.Unlock()
			case node.EventPeerConnect:
				uc.detector.RecordPeerConnect()
			case node.EventPeerDrop:
				uc.detector.RecordPeerDrop()
			}
		}
	}
}

func (uc *UnifiedCognition) Stop() {
	if !uc.running {
		return
	}
	uc.running = false
	close(uc.stopCh)
	if uc.sub != nil {
		uc.sub.Unsubscribe()
	}
	uc.wg.Wait()
}

func (uc *UnifiedCognition) cognitionLoop() {
	defer uc.wg.Done()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-uc.stopCh:
			return
		case <-ticker.C:
			uc.processCycle()
		}
	}
}

func (uc *UnifiedCognition) processCycle() {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	flowDyn := uc.detector.Dynamics()
	uc.dynamics.FlowPressure = flowDyn.Pressure
	uc.dynamics.FlowTension = flowDyn.Tension
	uc.dynamics.FlowRhythm = flowDyn.Rhythm
	uc.dynamics.FlowCoherence = flowDyn.Coherence

	for concept, weight := range uc.concepts {
		newWeight := weight * uc.conceptDecay
		if newWeight < 0.01 {
			delete(uc.concepts, concept)
		} else {
			uc.concepts[concept] = newWeight
		}
	}

	uc.dynamics.TotalPressure = uc.dynamics.FlowPressure*0.4 +
		uc.dynamics.UserActivity*0.4 +
		float64(uc.dynamics.BroadcastPending)*0.1 +
		uc.dynamics.FlowTension*0.1

	uc.dynamics.ReadyToAct = uc.dynamics.TotalPressure > 0.3 &&
		(uc.dynamics.BroadcastPending > 0 || uc.dynamics.ActiveQuery)
}

func (uc *UnifiedCognition) workLoop() {
	defer uc.wg.Done()

	for {
		select {
		case <-uc.stopCh:
			return
		case work := <-uc.pendingWork:
			uc.processWork(work)
		}
	}
}

func (uc *UnifiedCognition) processWork(work WorkItem) {
	uc.mu.Lock()
	uc.dynamics.BroadcastPending--
	uc.mu.Unlock()

	if work.Priority < 0.5 {
		uc.waitForGoodConditions()
	}

	uc.mu.RLock()
	mixedPayload := make([]byte, len(work.Payload)+32)
	copy(mixedPayload, work.Payload)
	copy(mixedPayload[len(work.Payload):], uc.entropyPool[:])
	uc.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	select {
	case <-ctx.Done():
		return
	case <-uc.stopCh:
		return
	default:
	}

	resultCh := uc.feed.Broadcast(mixedPayload)
	select {
	case result := <-resultCh:
		if result.Err != nil {
			return
		}
		uc.ingestBroadcastResult(result.TxHash, work.Concepts)

		uc.mu.Lock()
		uc.dynamics.LastBroadcast = time.Now()
		uc.mu.Unlock()

		if work.Callback != nil {
			work.Callback([]common.Hash{result.TxHash})
		}
	case <-ctx.Done():
	case <-uc.stopCh:
	}
}

func (uc *UnifiedCognition) waitForGoodConditions() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(2 * time.Second)

	for {
		select {
		case <-uc.stopCh:
			return
		case <-timeout:
			return
		case <-ticker.C:
			uc.mu.RLock()
			ready := uc.dynamics.FlowPressure > 0.2 || uc.dynamics.FlowCoherence > 0.5
			uc.mu.RUnlock()
			if ready {
				return
			}
		}
	}
}

func (uc *UnifiedCognition) ingestBroadcastResult(hash common.Hash, concepts []string) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	hashBytes := hash.Bytes()
	for i := 0; i < 32; i++ {
		uc.entropyPool[i] ^= hashBytes[i]
	}
	uc.poolVersion++

	for _, concept := range concepts {
		uc.concepts[concept] = math.Min(1.0, uc.concepts[concept]+0.3)
	}

	uc.conceptHistory = append(uc.conceptHistory, concepts...)
	if len(uc.conceptHistory) > 100 {
		uc.conceptHistory = uc.conceptHistory[len(uc.conceptHistory)-100:]
	}
}

func (uc *UnifiedCognition) InjectUserInput(text string, concepts []string) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	inputHash := crypto.Keccak256([]byte(text))
	for i := 0; i < 32; i++ {
		uc.entropyPool[i] ^= inputHash[i]
	}
	uc.poolVersion++

	for _, concept := range concepts {
		uc.concepts[concept] = math.Min(1.0, uc.concepts[concept]+0.8)
	}

	uc.dynamics.UserActivity = math.Min(1.0, uc.dynamics.UserActivity+0.5)
	uc.dynamics.LastUserInput = time.Now()
}

func (uc *UnifiedCognition) Query(text string, concepts []string, callback func([]common.Hash)) {
	uc.InjectUserInput(text, concepts)

	uc.mu.Lock()
	uc.dynamics.ActiveQuery = true
	uc.dynamics.QueryContent = text
	uc.dynamics.BroadcastPending++
	uc.mu.Unlock()

	payload := crypto.Keccak256([]byte("query:" + text))

	work := WorkItem{
		Type:     WorkQuery,
		Payload:  payload,
		Concepts: concepts,
		Priority: 0.9,
		Created:  time.Now(),
		Callback: func(hashes []common.Hash) {
			uc.mu.Lock()
			uc.dynamics.ActiveQuery = false
			uc.dynamics.QueryContent = ""
			uc.mu.Unlock()

			if callback != nil {
				callback(hashes)
			}
		},
	}

	select {
	case uc.pendingWork <- work:
	default:
	}
}

func (uc *UnifiedCognition) Steer(concepts []string, strength float64) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	for _, concept := range concepts {
		uc.concepts[concept] = math.Min(1.0, uc.concepts[concept]+strength)
	}
}

func (uc *UnifiedCognition) SubmitWork(workType WorkType, payload []byte, concepts []string, priority float64) {
	uc.mu.Lock()
	uc.dynamics.BroadcastPending++
	uc.mu.Unlock()

	work := WorkItem{
		Type:     workType,
		Payload:  payload,
		Concepts: concepts,
		Priority: priority,
		Created:  time.Now(),
	}

	select {
	case uc.pendingWork <- work:
	default:
	}
}

func (uc *UnifiedCognition) Entropy() []byte {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	result := make([]byte, 32)
	copy(result, uc.entropyPool[:])
	return crypto.Keccak256(result)
}

func (uc *UnifiedCognition) Dynamics() UnifiedDynamics {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	return uc.dynamics
}

func (uc *UnifiedCognition) ActiveConcepts(n int) []string {
	uc.mu.RLock()
	defer uc.mu.RUnlock()

	type cw struct {
		c string
		w float64
	}
	list := make([]cw, 0, len(uc.concepts))
	for c, w := range uc.concepts {
		list = append(list, cw{c, w})
	}

	for i := 0; i < len(list)-1; i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].w > list[i].w {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	result := make([]string, 0, n)
	for i := 0; i < n && i < len(list); i++ {
		result = append(result, list[i].c)
	}
	return result
}

func (uc *UnifiedCognition) ConceptWeight(concept string) float64 {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	return uc.concepts[concept]
}

type UnifiedFlowCompute struct {
	cognition *UnifiedCognition
	buffer    []byte
	bufferIdx int
	mu        sync.Mutex
}

func NewUnifiedFlowCompute(uc *UnifiedCognition) *UnifiedFlowCompute {
	return &UnifiedFlowCompute{
		cognition: uc,
		buffer:    make([]byte, 0, 256),
	}
}

func (ufc *UnifiedFlowCompute) NextBytes(n int) []byte {
	ufc.mu.Lock()
	defer ufc.mu.Unlock()

	for len(ufc.buffer)-ufc.bufferIdx < n {
		entropy := ufc.cognition.Entropy()
		ufc.buffer = append(ufc.buffer, entropy...)
	}

	result := make([]byte, n)
	copy(result, ufc.buffer[ufc.bufferIdx:ufc.bufferIdx+n])
	ufc.bufferIdx += n

	if ufc.bufferIdx > 1024 {
		ufc.buffer = ufc.buffer[ufc.bufferIdx:]
		ufc.bufferIdx = 0
	}
	return result
}

func (ufc *UnifiedFlowCompute) NextUint64() uint64 {
	b := ufc.NextBytes(8)
	var result uint64
	for i := 0; i < 8; i++ {
		result = (result << 8) | uint64(b[i])
	}
	return result
}

func (ufc *UnifiedFlowCompute) SelectIndex(n int) int {
	if n <= 0 {
		return 0
	}
	return int(ufc.NextUint64() % uint64(n))
}
