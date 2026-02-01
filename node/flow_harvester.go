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

		for i := 0; i < 4; i++ {
			offset := i * 8

			p := uint64(mf.pool[offset]) | uint64(mf.pool[offset+1])<<8 |
				uint64(mf.pool[offset+2])<<16 | uint64(mf.pool[offset+3])<<24 |
				uint64(mf.pool[offset+4])<<32 | uint64(mf.pool[offset+5])<<40 |
				uint64(mf.pool[offset+6])<<48 | uint64(mf.pool[offset+7])<<56

			hc := uint64(hashBytes[offset]) | uint64(hashBytes[offset+1])<<8 |
				uint64(hashBytes[offset+2])<<16 | uint64(hashBytes[offset+3])<<24 |
				uint64(hashBytes[offset+4])<<32 | uint64(hashBytes[offset+5])<<40 |
				uint64(hashBytes[offset+6])<<48 | uint64(hashBytes[offset+7])<<56

			x := p ^ hc
			mf.pool[offset] = byte(x)
			mf.pool[offset+1] = byte(x >> 8)
			mf.pool[offset+2] = byte(x >> 16)
			mf.pool[offset+3] = byte(x >> 24)
			mf.pool[offset+4] = byte(x >> 32)
			mf.pool[offset+5] = byte(x >> 40)
			mf.pool[offset+6] = byte(x >> 48)
			mf.pool[offset+7] = byte(x >> 56)
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

	maxRefills := (n / 32) + 10
	refillCount := 0
	for len(fc.buffer)-fc.bufferIdx < n {
		fc.refillBuffer()
		refillCount++
		if refillCount > maxRefills {

			break
		}
	}

	result := make([]byte, n)
	available := len(fc.buffer) - fc.bufferIdx
	if available >= n {
		copy(result, fc.buffer[fc.bufferIdx:fc.bufferIdx+n])
		fc.bufferIdx += n
	} else if available > 0 {

		copy(result, fc.buffer[fc.bufferIdx:])
		fc.bufferIdx = len(fc.buffer)
	}

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

func (fc *FlowCompute) Stats() FlowStats {
	hashCount, flowRate, lastHash := fc.flow.Stats()
	return FlowStats{
		HashCount: hashCount,
		FlowRate:  flowRate,
		LastHash:  lastHash,
	}
}

type FlowHarvester struct {
	feed    NetworkFeed
	flow    *MempoolFlow
	compute *FlowCompute
	sub     *FeedSubscription

	running bool
	stopCh  chan struct{}
}

func NewFlowHarvester(feed NetworkFeed) *FlowHarvester {
	flow := NewMempoolFlow(1000)
	return &FlowHarvester{
		feed:    feed,
		flow:    flow,
		compute: NewFlowCompute(flow),
		stopCh:  make(chan struct{}),
	}
}

func (fh *FlowHarvester) Start() error {
	if fh.running {
		return nil
	}
	fh.running = true

	if fh.feed == nil {
		return nil
	}

	fh.sub = fh.feed.Subscribe(256)

	go func() {
		for {
			select {
			case <-fh.stopCh:
				return
			case event, ok := <-fh.sub.C:
				if !ok {
					return
				}
				switch event.Type {
				case EventTxHashes:
					fh.flow.Ingest(event.Hashes)
				case EventPeerConnect:
					eventHash := crypto.Keccak256Hash([]byte("connect:" + event.PeerID + ":" + time.Now().String()))
					fh.flow.Ingest([]common.Hash{eventHash})
				case EventPeerDrop:
					eventHash := crypto.Keccak256Hash([]byte("drop:" + event.PeerID + ":" + time.Now().String()))
					fh.flow.Ingest([]common.Hash{eventHash})
				}
			}
		}
	}()

	return nil
}

func (fh *FlowHarvester) Stop() {
	if !fh.running {
		return
	}
	fh.running = false
	close(fh.stopCh)
	if fh.sub != nil {
		fh.sub.Unsubscribe()
	}
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
