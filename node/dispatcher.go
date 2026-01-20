

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
	transport *HybridTransport

	
	mu         sync.Mutex
	collecting bool
	hashes     map[common.Hash]bool
	hashList   []common.Hash
	hashBytes  [][]byte
	processor  HashProcessor
	stopCh     chan struct{}

	
	collectTimeout time.Duration
}


func NewDispatcher(transport *HybridTransport) *Dispatcher {
	return &Dispatcher{
		transport:      transport,
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
		PeerCount: d.transport.PeerCount(),
	}

	
	originalHandler := d.transport.devp2p.OnTxHashReceived
	d.transport.devp2p.OnTxHashReceived = func(hashes []common.Hash) {
		d.collectHashes(hashes)
		if originalHandler != nil {
			originalHandler(hashes)
		}
	}
	defer func() {
		d.transport.devp2p.OnTxHashReceived = originalHandler
	}()

	
	txHash, err := d.transport.BroadcastZEAM(payload)
	if err != nil {
		return nil, err
	}
	result.BroadcastTx = txHash

	
	d.mu.Lock()
	d.hashes[txHash] = true 
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


var SemanticCategory = map[byte]string{
	
	
}

func init() {
	for i := 0; i < 256; i++ {
		b := byte(i)
		switch {
		case b < 0x20:
			SemanticCategory[b] = "noun.object"
		case b < 0x40:
			SemanticCategory[b] = "verb.action"
		case b < 0x60:
			SemanticCategory[b] = "adj.property"
		case b < 0x80:
			SemanticCategory[b] = "noun.relation"
		case b < 0xA0:
			SemanticCategory[b] = "noun.quantity"
		case b < 0xC0:
			SemanticCategory[b] = "noun.state"
		case b < 0xE0:
			SemanticCategory[b] = "noun.event"
		default:
			SemanticCategory[b] = "noun.abstract"
		}
	}
}


func HashToSemantics(hashBytes []byte, count int) []string {
	if count > len(hashBytes) {
		count = len(hashBytes)
	}
	semantics := make([]string, count)
	for i := 0; i < count; i++ {
		semantics[i] = SemanticCategory[hashBytes[i]]
	}
	return semantics
}


func HashEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}

	freq := make(map[byte]int)
	for _, b := range data {
		freq[b]++
	}

	var entropy float64
	n := float64(len(data))
	for _, count := range freq {
		if count > 0 {
			p := float64(count) / n
			entropy -= p * log2(p)
		}
	}

	return entropy / 8.0 
}

func log2(x float64) float64 {
	if x <= 0 {
		return 0
	}
	return ln(x) / 0.693147180559945
}

func ln(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x == 1 {
		return 0
	}

	ln2 := 0.693147180559945
	reductions := 0
	for x > 2 {
		x /= 2
		reductions++
	}

	y := x - 1
	result := 0.0
	term := y
	for i := 1; i <= 100; i++ {
		if i%2 == 1 {
			result += term / float64(i)
		} else {
			result -= term / float64(i)
		}
		term *= y
		if term < 1e-15 && term > -1e-15 {
			break
		}
	}

	return result + float64(reductions)*ln2
}


type LiveCompute struct {
	Dispatcher *Dispatcher
	Transport  *HybridTransport
}


func NewLiveCompute(transport *HybridTransport) *LiveCompute {
	return &LiveCompute{
		Dispatcher: NewDispatcher(transport),
		Transport:  transport,
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
