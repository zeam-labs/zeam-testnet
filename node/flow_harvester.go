

package node

import (
	"encoding/binary"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)


type MempoolFlow struct {
	mu sync.Mutex

	
	pool [32]byte

	
	hashCount    uint64    
	lastHash     time.Time 
	flowRate     float64   
	lastRateCalc time.Time
	recentCount  uint64    

	
	recentHashes []common.Hash
	ringIdx      int
	ringSize     int
}


func NewMempoolFlow(ringSize int) *MempoolFlow {
	if ringSize < 32 {
		ringSize = 32
	}
	return &MempoolFlow{
		recentHashes: make([]common.Hash, ringSize),
		ringSize:     ringSize,
		lastRateCalc: time.Now(),
	}
}


func (mf *MempoolFlow) Ingest(hashes []common.Hash) {
	mf.mu.Lock()
	defer mf.mu.Unlock()

	now := time.Now()

	for _, h := range hashes {
		
		hashBytes := h.Bytes()
		for i := 0; i < 32; i++ {
			mf.pool[i] ^= hashBytes[i]
		}

		
		mf.recentHashes[mf.ringIdx] = h
		mf.ringIdx = (mf.ringIdx + 1) % mf.ringSize

		mf.hashCount++
		mf.recentCount++
	}

	mf.lastHash = now

	
	elapsed := now.Sub(mf.lastRateCalc).Seconds()
	if elapsed >= 1.0 {
		
		instantRate := float64(mf.recentCount) / elapsed
		mf.flowRate = 0.7*mf.flowRate + 0.3*instantRate
		mf.recentCount = 0
		mf.lastRateCalc = now
	}
}


func (mf *MempoolFlow) Entropy() []byte {
	mf.mu.Lock()
	defer mf.mu.Unlock()

	
	result := crypto.Keccak256(mf.pool[:])
	return result
}


func (mf *MempoolFlow) Harvest() (entropy []byte, hashCount uint64) {
	mf.mu.Lock()
	defer mf.mu.Unlock()

	entropy = crypto.Keccak256(mf.pool[:])
	hashCount = mf.hashCount

	
	mf.pool = [32]byte{}

	return
}


func (mf *MempoolFlow) RecentHashes(n int) []common.Hash {
	mf.mu.Lock()
	defer mf.mu.Unlock()

	if n > mf.ringSize {
		n = mf.ringSize
	}
	if n > int(mf.hashCount) {
		n = int(mf.hashCount)
	}

	result := make([]common.Hash, n)
	for i := 0; i < n; i++ {
		idx := (mf.ringIdx - 1 - i + mf.ringSize) % mf.ringSize
		result[i] = mf.recentHashes[idx]
	}
	return result
}


func (mf *MempoolFlow) Stats() (hashCount uint64, flowRate float64, lastHash time.Time) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	return mf.hashCount, mf.flowRate, mf.lastHash
}


type FlowCompute struct {
	flow *MempoolFlow

	
	buffer    []byte
	bufferIdx int
	bufferMu  sync.Mutex

	
	minBuffer int
}


func NewFlowCompute(flow *MempoolFlow) *FlowCompute {
	return &FlowCompute{
		flow:      flow,
		buffer:    make([]byte, 0, 256),
		minBuffer: 64,
	}
}


func (fc *FlowCompute) refillBuffer() {
	entropy := fc.flow.Entropy()
	fc.buffer = append(fc.buffer, entropy...)
}


func (fc *FlowCompute) NextBytes(n int) []byte {
	fc.bufferMu.Lock()
	defer fc.bufferMu.Unlock()

	
	for len(fc.buffer)-fc.bufferIdx < n {
		fc.refillBuffer()
	}

	result := make([]byte, n)
	copy(result, fc.buffer[fc.bufferIdx:fc.bufferIdx+n])
	fc.bufferIdx += n

	
	if fc.bufferIdx > 1024 {
		fc.buffer = fc.buffer[fc.bufferIdx:]
		fc.bufferIdx = 0
	}

	return result
}


func (fc *FlowCompute) NextUint64() uint64 {
	b := fc.NextBytes(8)
	return binary.BigEndian.Uint64(b)
}


func (fc *FlowCompute) NextBigInt(n *big.Int) *big.Int {
	b := fc.NextBytes(32)
	result := new(big.Int).SetBytes(b)
	return result.Mod(result, n)
}


func (fc *FlowCompute) NextFloat() float64 {
	return float64(fc.NextUint64()) / float64(^uint64(0))
}


func (fc *FlowCompute) SelectIndex(n int) int {
	if n <= 0 {
		return 0
	}
	return int(fc.NextUint64() % uint64(n))
}


type FlowHarvester struct {
	transport *HybridTransport
	flow      *MempoolFlow
	compute   *FlowCompute

	
	running bool
	stopCh  chan struct{}
}


func NewFlowHarvester(transport *HybridTransport) *FlowHarvester {
	flow := NewMempoolFlow(1000) 
	return &FlowHarvester{
		transport: transport,
		flow:      flow,
		compute:   NewFlowCompute(flow),
		stopCh:    make(chan struct{}),
	}
}


func (fh *FlowHarvester) Start() error {
	if fh.running {
		return nil
	}
	fh.running = true

	if fh.transport == nil || fh.transport.devp2p == nil {
		return nil
	}

	devp2p := fh.transport.devp2p

	
	existingTxHash := devp2p.OnTxHashReceived
	devp2p.OnTxHashReceived = func(hashes []common.Hash) {
		fh.flow.Ingest(hashes)
		if existingTxHash != nil {
			existingTxHash(hashes)
		}
	}

	
	existingConnect := devp2p.OnPeerConnect
	devp2p.OnPeerConnect = func(peerID string) {
		
		
		eventHash := crypto.Keccak256Hash([]byte("connect:" + peerID + ":" + time.Now().String()))
		fh.flow.Ingest([]common.Hash{eventHash})

		if existingConnect != nil {
			existingConnect(peerID)
		}
	}

	
	existingDrop := devp2p.OnPeerDrop
	devp2p.OnPeerDrop = func(peerID string) {
		eventHash := crypto.Keccak256Hash([]byte("drop:" + peerID + ":" + time.Now().String()))
		fh.flow.Ingest([]common.Hash{eventHash})

		if existingDrop != nil {
			existingDrop(peerID)
		}
	}

	return nil
}


func (fh *FlowHarvester) Stop() {
	if !fh.running {
		return
	}
	fh.running = false
	close(fh.stopCh)
}


func (fh *FlowHarvester) Flow() *MempoolFlow {
	return fh.flow
}


func (fh *FlowHarvester) Compute() *FlowCompute {
	return fh.compute
}


func (fh *FlowHarvester) Stats() FlowStats {
	hashCount, flowRate, lastHash := fh.flow.Stats()
	return FlowStats{
		HashCount: hashCount,
		FlowRate:  flowRate,
		LastHash:  lastHash,
	}
}


type FlowStats struct {
	HashCount uint64    
	FlowRate  float64   
	LastHash  time.Time 
}


type FlowWordSelector struct {
	compute *FlowCompute
}


func NewFlowWordSelector(compute *FlowCompute) *FlowWordSelector {
	return &FlowWordSelector{compute: compute}
}


func (fws *FlowWordSelector) Select(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	idx := fws.compute.SelectIndex(len(candidates))
	return candidates[idx]
}


func (fws *FlowWordSelector) SelectWeighted(candidates []string, weights []float64) string {
	if len(candidates) == 0 {
		return ""
	}
	if len(weights) != len(candidates) {
		return fws.Select(candidates)
	}

	
	var total float64
	for _, w := range weights {
		total += w
	}
	if total == 0 {
		return fws.Select(candidates)
	}

	
	target := fws.compute.NextFloat() * total
	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if target <= cumulative {
			return candidates[i]
		}
	}

	return candidates[len(candidates)-1]
}


type FlowGrammar struct {
	harvester *FlowHarvester
	selector  *FlowWordSelector

	
	GetWordsByPOS func(pos string) []string
	GetSynonyms   func(word string) []string
	GetDefinition func(word string) string
}


func NewFlowGrammar(harvester *FlowHarvester) *FlowGrammar {
	return &FlowGrammar{
		harvester: harvester,
		selector:  NewFlowWordSelector(harvester.Compute()),
	}
}


func (fg *FlowGrammar) SelectWord(candidates []string, weights []float64) string {
	if len(weights) > 0 {
		return fg.selector.SelectWeighted(candidates, weights)
	}
	return fg.selector.Select(candidates)
}


func (fg *FlowGrammar) GenerateFromConcepts(concepts []string) string {
	if len(concepts) == 0 {
		return ""
	}

	
	primary := fg.selector.Select(concepts)

	
	var verbs, objects []string
	if fg.GetWordsByPOS != nil {
		verbs = fg.GetWordsByPOS("v")
		objects = fg.GetWordsByPOS("n")
	}

	
	if len(verbs) == 0 {
		verbs = []string{"involves", "concerns", "relates to", "indicates", "represents"}
	}
	if len(objects) == 0 {
		objects = concepts
	}

	
	verb := fg.selector.Select(verbs)
	object := fg.selector.Select(objects)

	
	if object == primary && len(objects) > 1 {
		for _, o := range objects {
			if o != primary {
				object = o
				break
			}
		}
	}

	return "The " + primary + " " + verb + " the " + object + "."
}
