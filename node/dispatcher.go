package node

import (
	"context"
	"encoding/binary"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type DispatchResult struct {
	Payload     []byte
	BroadcastTx common.Hash
	Hashes      []common.Hash
	HashBytes   [][]byte
	StartTime   time.Time
	Duration    time.Duration
	PeerCount   int
}

type HashProcessor func(hash common.Hash, hashBytes []byte) bool

type Dispatcher struct {
	feed NetworkFeed

	mu         sync.Mutex
	collecting bool
	hashes     map[common.Hash]bool
	hashList   []common.Hash
	hashBytes  [][]byte
	processor  HashProcessor
	stopCh     chan struct{}

	collectTimeout time.Duration
}

func NewDispatcher(feed NetworkFeed) *Dispatcher {
	return &Dispatcher{
		feed:           feed,
		hashes:         make(map[common.Hash]bool),
		collectTimeout: 30 * time.Second,
	}
}

func (d *Dispatcher) SetTimeout(timeout time.Duration) {
	d.collectTimeout = timeout
}

func (d *Dispatcher) Dispatch(ctx context.Context, payload []byte, processor HashProcessor) (*DispatchResult, error) {
	d.mu.Lock()
	if d.collecting {
		d.mu.Unlock()
		return nil, ErrDispatchInProgress
	}

	d.collecting = true
	d.hashes = make(map[common.Hash]bool)
	d.hashList = nil
	d.hashBytes = nil
	d.processor = processor
	d.stopCh = make(chan struct{})
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.collecting = false
		d.mu.Unlock()
	}()

	result := &DispatchResult{
		Payload:   payload,
		StartTime: time.Now(),
	}

	sub := d.feed.Subscribe(256)
	defer sub.Unsubscribe()

	go func() {
		for {
			select {
			case event, ok := <-sub.C:
				if !ok {
					return
				}
				if event.Type == EventTxHashes {
					d.collectHashes(event.Hashes)
				}
			case <-d.stopCh:
				return
			}
		}
	}()

	broadcastCh := d.feed.Broadcast(payload)
	broadcastResult := <-broadcastCh
	if broadcastResult.Err != nil {
		return nil, broadcastResult.Err
	}
	result.BroadcastTx = broadcastResult.TxHash

	d.mu.Lock()
	d.hashes[result.BroadcastTx] = true
	d.mu.Unlock()

	timeout := time.After(d.collectTimeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var firstHashTime time.Time
	settleTime := 200 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			goto done
		case <-d.stopCh:
			goto done
		case <-timeout:
			goto done
		case <-ticker.C:
			d.mu.Lock()
			collecting := d.collecting
			hashCount := len(d.hashList)
			d.mu.Unlock()

			if !collecting {
				goto done
			}

			if hashCount > 0 {
				if firstHashTime.IsZero() {
					firstHashTime = time.Now()
				} else if time.Since(firstHashTime) > settleTime {

					goto done
				}
			}
		}
	}

done:
	d.mu.Lock()
	result.Hashes = d.hashList
	result.HashBytes = d.hashBytes
	result.Duration = time.Since(result.StartTime)
	d.mu.Unlock()

	return result, nil
}

func (d *Dispatcher) collectHashes(hashes []common.Hash) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.collecting {
		return
	}

	for _, hash := range hashes {
		if d.hashes[hash] {
			continue
		}

		hashBytes := hash.Bytes()
		d.hashes[hash] = true
		d.hashList = append(d.hashList, hash)
		d.hashBytes = append(d.hashBytes, hashBytes)

		if d.processor != nil {
			if !d.processor(hash, hashBytes) {
				close(d.stopCh)
				return
			}
		}
	}
}

func (d *Dispatcher) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.collecting && d.stopCh != nil {
		select {
		case <-d.stopCh:
		default:
			close(d.stopCh)
		}
	}
}

func (d *Dispatcher) HashCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.hashList)
}

func HashMod(hashBytes []byte, n *big.Int) *big.Int {
	h := new(big.Int).SetBytes(hashBytes)
	return h.Mod(h, n)
}

func HashToUint64(hashBytes []byte) uint64 {
	if len(hashBytes) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(hashBytes[:8])
}

func HashToByte(hashBytes []byte) byte {
	if len(hashBytes) == 0 {
		return 0
	}
	return hashBytes[0]
}

func HashXOR(a, b []byte) []byte {
	if len(a) != len(b) {
		return nil
	}
	result := make([]byte, len(a))
	for i := range a {
		result[i] = a[i] ^ b[i]
	}
	return result
}

func CombineHashes(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return nil
	}
	combined := make([]byte, 32)
	copy(combined, hashes[0])
	for i := 1; i < len(hashes); i++ {
		for j := 0; j < 32 && j < len(hashes[i]); j++ {
			combined[j] ^= hashes[i][j]
		}
	}
	return crypto.Keccak256(combined)
}

func HashToCoordinate(hash common.Hash) *big.Int {
	return new(big.Int).SetBytes(hash.Bytes())
}

type LiveCompute struct {
	Dispatcher *Dispatcher
	Feed       NetworkFeed
}

func NewLiveCompute(feed NetworkFeed) *LiveCompute {
	return &LiveCompute{
		Dispatcher: NewDispatcher(feed),
		Feed:       feed,
	}
}

func (lc *LiveCompute) SetTimeout(timeout time.Duration) {
	lc.Dispatcher.SetTimeout(timeout)
}

func (lc *LiveCompute) Dispatch(ctx context.Context, payload []byte) (*DispatchResult, error) {
	return lc.Dispatcher.Dispatch(ctx, payload, nil)
}

func (lc *LiveCompute) DispatchWithProcessor(ctx context.Context, payload []byte, processor HashProcessor) (*DispatchResult, error) {
	return lc.Dispatcher.Dispatch(ctx, payload, processor)
}

var (
	ErrDispatchInProgress = &dispatchError{"dispatch already in progress"}
)

type dispatchError struct {
	msg string
}

func (e *dispatchError) Error() string {
	return e.msg
}
