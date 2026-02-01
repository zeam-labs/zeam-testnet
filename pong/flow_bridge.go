package pong

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	ErrNoFlowConnection = errors.New("no network flow - Web4 requires network")
	ErrComputeTimeout   = errors.New("compute timeout - network unavailable")
	ErrNoHashes         = errors.New("no hashes received from network")
)

var ErrNoL1Connection = ErrNoFlowConnection

type LiveBridge struct {
	liveCompute LiveComputeInterface
	ctx         context.Context

	mu           sync.Mutex
	dispatches   uint64
	hashesUsed   uint64
	lastDispatch time.Time
	lastHash     common.Hash
	errors       uint64

	tickTimeout time.Duration
}

type LiveComputeInterface interface {
	Dispatch(ctx context.Context, payload []byte) (LiveDispatchResult, error)
	SetTimeout(timeout time.Duration)
}

type LiveDispatchResult interface {
	GetHashes() []common.Hash
	GetHashBytes() [][]byte
	GetDuration() time.Duration
}

func NewLiveBridge(liveCompute LiveComputeInterface) (*LiveBridge, error) {
	if liveCompute == nil {
		return nil, ErrNoFlowConnection
	}
	return &LiveBridge{
		liveCompute: liveCompute,
		ctx:         context.Background(),
		tickTimeout: 2 * time.Second,
	}, nil
}

func (b *LiveBridge) SetContext(ctx context.Context) {
	b.ctx = ctx
}

func (b *LiveBridge) SetTickTimeout(timeout time.Duration) {
	b.tickTimeout = timeout
	b.liveCompute.SetTimeout(timeout)
}

func (b *LiveBridge) ComputeHash(payload []byte) (common.Hash, error) {
	ctx, cancel := context.WithTimeout(b.ctx, b.tickTimeout)
	defer cancel()

	result, err := b.liveCompute.Dispatch(ctx, payload)
	if err != nil {
		b.mu.Lock()
		b.errors++
		b.mu.Unlock()
		return common.Hash{}, ErrComputeTimeout
	}

	hashBytes := result.GetHashBytes()
	if len(hashBytes) == 0 {
		b.mu.Lock()
		b.errors++
		b.mu.Unlock()
		return common.Hash{}, ErrNoHashes
	}

	combined := make([]byte, 32)
	for _, hb := range hashBytes {
		for i := 0; i < 32 && i < len(hb); i++ {
			combined[i] ^= hb[i]
		}
	}
	finalHash := common.BytesToHash(crypto.Keccak256(combined))

	b.mu.Lock()
	b.dispatches++
	b.hashesUsed += uint64(len(hashBytes))
	b.lastDispatch = time.Now()
	b.lastHash = finalHash
	b.mu.Unlock()

	return finalHash, nil
}

func (b *LiveBridge) Stats() (dispatches, hashesUsed, errors uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dispatches, b.hashesUsed, b.errors
}

type FlowBridge struct {
	flowCompute FlowComputeInterface
	mu          sync.Mutex
}

type FlowComputeInterface interface {
	NextBytes(n int) []byte

	Stats() (hashCount uint64, flowRate float64)
}

func NewFlowBridge(flowCompute FlowComputeInterface) (*FlowBridge, error) {
	if flowCompute == nil {
		return nil, ErrNoFlowConnection
	}
	return &FlowBridge{flowCompute: flowCompute}, nil
}

func (b *FlowBridge) ComputeHash(payload []byte) (common.Hash, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	hashCount, flowRate := b.flowCompute.Stats()
	if hashCount == 0 || flowRate == 0 {
		return common.Hash{}, ErrNoFlowConnection
	}

	flowEntropy := b.flowCompute.NextBytes(32)
	combined := make([]byte, len(payload)+32)
	copy(combined, payload)
	copy(combined[len(payload):], flowEntropy)

	return common.BytesToHash(crypto.Keccak256(combined)), nil
}

type DistributedBridge struct {
	computer DistributedComputerInterface
	mu       sync.Mutex
}

type DistributedComputerInterface interface {
	Broadcast(payload []byte) [][]byte
	Stats() (broadcasts int, ops uint64, solutions int)
}

func NewDistributedBridge(computer DistributedComputerInterface) (*DistributedBridge, error) {
	if computer == nil {
		return nil, ErrNoFlowConnection
	}
	return &DistributedBridge{computer: computer}, nil
}

func (b *DistributedBridge) ComputeHash(payload []byte) (common.Hash, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	results := b.computer.Broadcast(payload)
	if len(results) == 0 {
		return common.Hash{}, ErrNoHashes
	}

	combined := make([]byte, 32)
	for _, result := range results {
		for i := 0; i < 32 && i < len(result); i++ {
			combined[i] ^= result[i]
		}
	}

	return common.BytesToHash(crypto.Keccak256(combined)), nil
}

type ComputeFunc func(payload []byte) (common.Hash, error)

func MustCompute(fn ComputeFunc) func([]byte) common.Hash {
	return func(payload []byte) common.Hash {
		hash, err := fn(payload)
		if err != nil {
			panic("network compute failed: " + err.Error())
		}
		return hash
	}
}

type PongTask struct {
	tick   *Tick
	result common.Hash
	solved bool
	mu     sync.Mutex
}

func NewPongTask(tick *Tick) *PongTask {
	return &PongTask{tick: tick}
}

func (pt *PongTask) Encode() []byte {
	return pt.tick.Encode()
}

func (pt *PongTask) ProcessResult(nodeMAC []byte, result []byte) bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	for i := 0; i < 32 && i < len(result); i++ {
		pt.result[i] ^= result[i]
	}
	pt.solved = true
	return true
}

func (pt *PongTask) GetSolution() interface{} {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.result
}

func (pt *PongTask) Description() string {
	return "Pong tick computation"
}

func NewLiveGame(liveCompute LiveComputeInterface) (*Engine, error) {
	bridge, err := NewLiveBridge(liveCompute)
	if err != nil {
		return nil, err
	}
	return NewEngineWithError(bridge.ComputeHash), nil
}

func NewFlowGame(flowCompute FlowComputeInterface) (*Engine, error) {
	bridge, err := NewFlowBridge(flowCompute)
	if err != nil {
		return nil, err
	}
	return NewEngineWithError(bridge.ComputeHash), nil
}

func NewDistributedGame(computer DistributedComputerInterface) (*Engine, error) {
	bridge, err := NewDistributedBridge(computer)
	if err != nil {
		return nil, err
	}
	return NewEngineWithError(bridge.ComputeHash), nil
}
