

package node

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)


const (
	MsgTypeFactorPollard byte = 0x10 
	MsgTypeFactorECM     byte = 0x11 
	MsgTypeFactorResult  byte = 0x12 
	MsgTypeFactorCollide byte = 0x13 
	MsgTypeKeccakRho     byte = 0x4B 
)


type HybridTransport struct {
	libp2p  *LibP2PTransport
	devp2p  *DevP2PNode
	p2p     *P2PTransport 

	privateKey *ecdsa.PrivateKey
	config     *DevP2PConfig

	
	relayConnections int
	directConnections int
	statsMu          sync.RWMutex

	
	running bool
	stopCh  chan struct{}
}


type HybridConfig struct {
	PrivateKey  *ecdsa.PrivateKey
	DevP2PConfig *DevP2PConfig
	UseRelay    bool 
}


func NewHybridTransport(cfg *HybridConfig) (*HybridTransport, error) {
	h := &HybridTransport{
		privateKey: cfg.PrivateKey,
		config:     cfg.DevP2PConfig,
		stopCh:     make(chan struct{}),
	}

	
	if cfg.UseRelay {
		libp2pTransport, err := NewLibP2PTransport(cfg.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create libp2p transport: %w", err)
		}
		h.libp2p = libp2pTransport
	}

	
	devp2pNode, err := NewDevP2PNode(cfg.DevP2PConfig)
	if err != nil {
		if h.libp2p != nil {
			h.libp2p.Stop()
		}
		return nil, fmt.Errorf("failed to create devp2p node: %w", err)
	}
	h.devp2p = devp2pNode

	return h, nil
}


func (h *HybridTransport) Start() error {
	h.running = true

	
	if h.libp2p != nil {
		if err := h.libp2p.Start(); err != nil {
			return fmt.Errorf("failed to start libp2p: %w", err)
		}

		
		go h.waitForRelays()
	}

	
	if err := h.devp2p.Start(); err != nil {
		return fmt.Errorf("failed to start devp2p: %w", err)
	}

	
	go h.monitorConnections()

	return nil
}


func (h *HybridTransport) waitForRelays() {
	
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			addrs := h.libp2p.GetRelayAddrs()
			if len(addrs) > 0 {
				fmt.Printf("[Hybrid] Got %d relay addresses for NAT traversal\n", len(addrs))
				h.statsMu.Lock()
				h.relayConnections = len(addrs)
				h.statsMu.Unlock()
			}
		}
	}
}


func (h *HybridTransport) monitorConnections() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			devp2pPeers := h.devp2p.PeerCount()

			h.statsMu.Lock()
			h.directConnections = devp2pPeers
			h.statsMu.Unlock()

			if devp2pPeers > 0 {
				fmt.Printf("[Hybrid] L1 peers: %d\n", devp2pPeers)
			}
		}
	}
}


func (h *HybridTransport) Stop() {
	if !h.running {
		return
	}
	h.running = false
	close(h.stopCh)

	if h.devp2p != nil {
		h.devp2p.Stop()
	}
	if h.libp2p != nil {
		h.libp2p.Stop()
	}

	fmt.Printf("[Hybrid] Stopped\n")
}


func (h *HybridTransport) Broadcast(tx *types.Transaction) error {
	return h.devp2p.BroadcastRawTx(tx)
}


func (h *HybridTransport) BroadcastZEAM(data []byte) (common.Hash, error) {
	return h.devp2p.BroadcastZEAM(data)
}


func (h *HybridTransport) PeerCount() int {
	return h.devp2p.PeerCount()
}


func (h *HybridTransport) Stats() (relays, direct int) {
	h.statsMu.RLock()
	defer h.statsMu.RUnlock()
	return h.relayConnections, h.directConnections
}


func (h *HybridTransport) LibP2PPeerID() string {
	if h.libp2p == nil {
		return ""
	}
	return h.libp2p.PeerID()
}


func (h *HybridTransport) RelayAddrs() []string {
	if h.libp2p == nil {
		return nil
	}
	return h.libp2p.GetRelayAddrs()
}


func (h *HybridTransport) Address() common.Address {
	return h.devp2p.address
}


func (h *HybridTransport) OnZEAMMessage(fn func(tx *types.Transaction, data []byte)) {
	h.devp2p.OnZEAMMessage = fn
}


func (h *HybridTransport) OnPeerConnect(fn func(peerID string)) {
	h.devp2p.OnPeerConnect = fn
}


func (h *HybridTransport) OnPeerDrop(fn func(peerID string)) {
	h.devp2p.OnPeerDrop = fn
}


func CreateSepoliaHybridTransport(privateKey *ecdsa.PrivateKey, listenAddr string, useRelay bool) (*HybridTransport, error) {
	devp2pConfig := SepoliaConfig(privateKey)
	devp2pConfig.ListenAddr = listenAddr
	devp2pConfig.Bootnodes = SepoliaBootnodes()

	return NewHybridTransport(&HybridConfig{
		PrivateKey:   privateKey,
		DevP2PConfig: devp2pConfig,
		UseRelay:     useRelay,
	})
}


func (h *HybridTransport) ChainID() *big.Int {
	return h.config.ChainID
}


func (h *HybridTransport) NetworkID() uint64 {
	return h.config.NetworkID
}


type FactorBatchResult struct {
	TxHashes []common.Hash
	Seeds    []uint64
	Count    int
	Duration time.Duration
}


func (h *HybridTransport) BroadcastFactorBatch(target *big.Int, batchSize int, msgType byte) (*FactorBatchResult, error) {
	if h.devp2p == nil || !h.devp2p.running {
		return nil, fmt.Errorf("devp2p not running")
	}

	start := time.Now()
	result := &FactorBatchResult{
		TxHashes: make([]common.Hash, 0, batchSize),
		Seeds:    make([]uint64, 0, batchSize),
	}

	
	var randomBase [8]byte
	rand.Read(randomBase[:])
	baseValue := binary.BigEndian.Uint64(randomBase[:])

	
	var counter uint64

	
	targetBytes := target.Bytes()
	if len(targetBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(targetBytes):], targetBytes)
		targetBytes = padded
	}

	
	for i := 0; i < batchSize; i++ {
		
		seed := baseValue + atomic.AddUint64(&counter, 1)

		
		payload := make([]byte, 4+8+32)
		payload[0] = 0x5A 
		payload[1] = 0x45 
		payload[2] = 0x01 
		payload[3] = msgType
		binary.BigEndian.PutUint64(payload[4:12], seed)
		copy(payload[12:], targetBytes)

		
		hash, err := h.devp2p.BroadcastZEAM(payload)
		if err != nil {
			
			fmt.Printf("[Hybrid] Batch tx %d failed: %v\n", i, err)
			continue
		}

		result.TxHashes = append(result.TxHashes, hash)
		result.Seeds = append(result.Seeds, seed)
	}

	result.Count = len(result.TxHashes)
	result.Duration = time.Since(start)

	fmt.Printf("[Hybrid] Batch broadcast: %d txs in %v (%.0f tx/sec)\n",
		result.Count, result.Duration, float64(result.Count)/result.Duration.Seconds())

	return result, nil
}


func (h *HybridTransport) BroadcastFactorBatchParallel(target *big.Int, totalCount int, workers int, msgType byte) (*FactorBatchResult, error) {
	if workers < 1 {
		workers = 1
	}

	start := time.Now()
	batchPerWorker := totalCount / workers
	remainder := totalCount % workers

	var wg sync.WaitGroup
	var mu sync.Mutex
	allHashes := make([]common.Hash, 0, totalCount)
	allSeeds := make([]uint64, 0, totalCount)

	for w := 0; w < workers; w++ {
		size := batchPerWorker
		if w < remainder {
			size++ 
		}

		wg.Add(1)
		go func(workerSize int) {
			defer wg.Done()

			result, err := h.BroadcastFactorBatch(target, workerSize, msgType)
			if err != nil {
				fmt.Printf("[Hybrid] Worker failed: %v\n", err)
				return
			}

			mu.Lock()
			allHashes = append(allHashes, result.TxHashes...)
			allSeeds = append(allSeeds, result.Seeds...)
			mu.Unlock()
		}(size)
	}

	wg.Wait()

	result := &FactorBatchResult{
		TxHashes: allHashes,
		Seeds:    allSeeds,
		Count:    len(allHashes),
		Duration: time.Since(start),
	}

	fmt.Printf("[Hybrid] Parallel batch complete: %d txs in %v (%.0f tx/sec, %d workers)\n",
		result.Count, result.Duration, float64(result.Count)/result.Duration.Seconds(), workers)

	return result, nil
}


func (h *HybridTransport) BroadcastFactorBatchUltraFast(target *big.Int, batchSize int, msgType byte) (*FactorBatchResult, error) {
	if h.devp2p == nil || !h.devp2p.running {
		return nil, fmt.Errorf("devp2p not running")
	}

	start := time.Now()

	
	var randomBase [8]byte
	rand.Read(randomBase[:])
	baseValue := binary.BigEndian.Uint64(randomBase[:])

	
	targetBytes := target.Bytes()
	if len(targetBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(targetBytes):], targetBytes)
		targetBytes = padded
	}

	
	payloads := make([][]byte, batchSize)
	seeds := make([]uint64, batchSize)

	for i := 0; i < batchSize; i++ {
		seed := baseValue + uint64(i)
		seeds[i] = seed

		payload := make([]byte, 4+8+32)
		payload[0] = 0x5A 
		payload[1] = 0x45 
		payload[2] = 0x01 
		payload[3] = msgType
		binary.BigEndian.PutUint64(payload[4:12], seed)
		copy(payload[12:], targetBytes)
		payloads[i] = payload
	}

	
	hashes, err := h.devp2p.BroadcastZEAMBatchFast(payloads)
	if err != nil {
		return nil, err
	}

	result := &FactorBatchResult{
		TxHashes: hashes,
		Seeds:    seeds[:len(hashes)],
		Count:    len(hashes),
		Duration: time.Since(start),
	}

	fmt.Printf("[Hybrid] Ultra-fast batch: %d txs in %v (%.0f tx/sec)\n",
		result.Count, result.Duration, float64(result.Count)/result.Duration.Seconds())

	return result, nil
}


func (h *HybridTransport) BroadcastFactorBatchUltraFastParallel(target *big.Int, totalCount int, workers int, msgType byte) (*FactorBatchResult, error) {
	if workers < 1 {
		workers = 1
	}

	start := time.Now()
	batchPerWorker := totalCount / workers
	remainder := totalCount % workers

	var wg sync.WaitGroup
	var mu sync.Mutex
	allHashes := make([]common.Hash, 0, totalCount)
	allSeeds := make([]uint64, 0, totalCount)

	for w := 0; w < workers; w++ {
		size := batchPerWorker
		if w < remainder {
			size++
		}

		wg.Add(1)
		go func(workerSize int) {
			defer wg.Done()

			result, err := h.BroadcastFactorBatchUltraFast(target, workerSize, msgType)
			if err != nil {
				return
			}

			mu.Lock()
			allHashes = append(allHashes, result.TxHashes...)
			allSeeds = append(allSeeds, result.Seeds...)
			mu.Unlock()
		}(size)
	}

	wg.Wait()

	result := &FactorBatchResult{
		TxHashes: allHashes,
		Seeds:    allSeeds,
		Count:    len(allHashes),
		Duration: time.Since(start),
	}

	fmt.Printf("[Hybrid] Ultra-fast parallel: %d txs in %v (%.0f tx/sec, %d workers)\n",
		result.Count, result.Duration, float64(result.Count)/result.Duration.Seconds(), workers)

	return result, nil
}


func (h *HybridTransport) BroadcastFactorBatchMultiKey(target *big.Int, batchSize int, numKeys int, msgType byte) (*FactorBatchResult, error) {
	if h.devp2p == nil || !h.devp2p.running {
		return nil, fmt.Errorf("devp2p not running")
	}

	start := time.Now()

	
	keys := make([]*ecdsa.PrivateKey, numKeys)
	keys[0] = h.privateKey 
	for i := 1; i < numKeys; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate key %d: %w", i, err)
		}
		keys[i] = key
	}

	
	var randomBase [8]byte
	rand.Read(randomBase[:])
	baseValue := binary.BigEndian.Uint64(randomBase[:])

	
	targetBytes := target.Bytes()
	if len(targetBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(targetBytes):], targetBytes)
		targetBytes = padded
	}

	
	payloads := make([][]byte, batchSize)
	seeds := make([]uint64, batchSize)

	for i := 0; i < batchSize; i++ {
		seed := baseValue + uint64(i)
		seeds[i] = seed

		payload := make([]byte, 4+8+32)
		payload[0] = 0x5A 
		payload[1] = 0x45 
		payload[2] = 0x01 
		payload[3] = msgType
		binary.BigEndian.PutUint64(payload[4:12], seed)
		copy(payload[12:], targetBytes)
		payloads[i] = payload
	}

	
	hashes, err := h.devp2p.BroadcastZEAMBatchFastMultiKey(payloads, keys)
	if err != nil {
		return nil, err
	}

	result := &FactorBatchResult{
		TxHashes: hashes,
		Seeds:    seeds[:len(hashes)],
		Count:    len(hashes),
		Duration: time.Since(start),
	}

	fmt.Printf("[Hybrid] Multi-key batch: %d txs in %v (%.0f tx/sec, %d keys)\n",
		result.Count, result.Duration, float64(result.Count)/result.Duration.Seconds(), numKeys)

	return result, nil
}


func (h *HybridTransport) BroadcastFactorBatchMultiKeyParallel(target *big.Int, totalCount int, workers int, keysPerWorker int, msgType byte) (*FactorBatchResult, error) {
	if workers < 1 {
		workers = 1
	}
	if keysPerWorker < 1 {
		keysPerWorker = 1
	}

	start := time.Now()
	batchPerWorker := totalCount / workers
	remainder := totalCount % workers

	var wg sync.WaitGroup
	var mu sync.Mutex
	allHashes := make([]common.Hash, 0, totalCount)
	allSeeds := make([]uint64, 0, totalCount)

	for w := 0; w < workers; w++ {
		size := batchPerWorker
		if w < remainder {
			size++
		}

		wg.Add(1)
		go func(workerSize int) {
			defer wg.Done()

			result, err := h.BroadcastFactorBatchMultiKey(target, workerSize, keysPerWorker, msgType)
			if err != nil {
				return
			}

			mu.Lock()
			allHashes = append(allHashes, result.TxHashes...)
			allSeeds = append(allSeeds, result.Seeds...)
			mu.Unlock()
		}(size)
	}

	wg.Wait()

	totalKeys := workers * keysPerWorker
	result := &FactorBatchResult{
		TxHashes: allHashes,
		Seeds:    allSeeds,
		Count:    len(allHashes),
		Duration: time.Since(start),
	}

	fmt.Printf("[Hybrid] Multi-key parallel: %d txs in %v (%.0f tx/sec, %d workers × %d keys = %d signers)\n",
		result.Count, result.Duration, float64(result.Count)/result.Duration.Seconds(),
		workers, keysPerWorker, totalKeys)

	return result, nil
}


type PackedFactorPayload struct {
	Target    *big.Int
	Seeds     []uint64
	NumSeeds  int
	TxHash    common.Hash
	PayloadSz int
}


func (h *HybridTransport) BroadcastFactorPacked(target *big.Int, numSeeds int, msgType byte) (*PackedFactorPayload, error) {
	if h.devp2p == nil || !h.devp2p.running {
		return nil, fmt.Errorf("devp2p not running")
	}

	
	var randomBase [8]byte
	rand.Read(randomBase[:])
	baseValue := binary.BigEndian.Uint64(randomBase[:])

	
	targetBytes := target.Bytes()
	if len(targetBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(targetBytes):], targetBytes)
		targetBytes = padded
	}

	
	headerSize := 2 + 1 + 1 + 4 + 32 
	seedsSize := numSeeds * 8
	payload := make([]byte, headerSize+seedsSize)

	
	payload[0] = 0x5A 
	payload[1] = 0x45 
	payload[2] = 0x02 
	payload[3] = msgType
	binary.BigEndian.PutUint32(payload[4:8], uint32(numSeeds))
	copy(payload[8:40], targetBytes)

	
	seeds := make([]uint64, numSeeds)
	for i := 0; i < numSeeds; i++ {
		seed := baseValue + uint64(i)
		seeds[i] = seed
		binary.BigEndian.PutUint64(payload[40+i*8:40+(i+1)*8], seed)
	}

	
	hash, err := h.devp2p.BroadcastZEAM(payload)
	if err != nil {
		return nil, err
	}

	result := &PackedFactorPayload{
		Target:    target,
		Seeds:     seeds,
		NumSeeds:  numSeeds,
		TxHash:    hash,
		PayloadSz: len(payload),
	}

	fmt.Printf("[Hybrid] Packed broadcast: 1 tx with %d seeds (%d bytes) → hash %s\n",
		numSeeds, len(payload), hash.Hex()[:18])

	return result, nil
}


func (h *HybridTransport) BroadcastFactorPackedMulti(target *big.Int, totalSeeds int, maxSeedsPerTx int) ([]*PackedFactorPayload, error) {
	if maxSeedsPerTx < 1 {
		maxSeedsPerTx = 10000 
	}

	numTxs := (totalSeeds + maxSeedsPerTx - 1) / maxSeedsPerTx
	results := make([]*PackedFactorPayload, 0, numTxs)

	remaining := totalSeeds
	for i := 0; i < numTxs; i++ {
		seedsThisTx := maxSeedsPerTx
		if remaining < seedsThisTx {
			seedsThisTx = remaining
		}

		result, err := h.BroadcastFactorPacked(target, seedsThisTx, MsgTypeFactorPollard)
		if err != nil {
			return results, err
		}

		results = append(results, result)
		remaining -= seedsThisTx
	}

	totalPayload := 0
	totalSeedsActual := 0
	for _, r := range results {
		totalPayload += r.PayloadSz
		totalSeedsActual += r.NumSeeds
	}

	fmt.Printf("[Hybrid] Packed multi: %d txs, %d total seeds, %d bytes total\n",
		len(results), totalSeedsActual, totalPayload)

	return results, nil
}


type KeccakRhoPayload struct {
	TxHash   common.Hash
	Target   *big.Int
	WalkID   uint32
	X        *big.Int
	PayloadSz int
}


func KeccakStep(n, x *big.Int) *big.Int {
	
	data := make([]byte, 64)
	nBytes := n.Bytes()
	xBytes := x.Bytes()
	copy(data[32-len(nBytes):32], nBytes)
	copy(data[64-len(xBytes):64], xBytes)

	
	hash := crypto.Keccak256(data)

	
	result := new(big.Int).SetBytes(hash)
	return result.Mod(result, n)
}


func IsKeccakDP(x *big.Int, bits int) bool {
	if bits == 0 {
		return true
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return new(big.Int).And(x, mask).Sign() == 0
}


func EncodeKeccakRhoPayload(n, x *big.Int, walkID uint32) []byte {
	
	payload := make([]byte, 72)

	
	payload[0] = 0x5A 
	payload[1] = 0x45 
	payload[2] = 0x01 
	payload[3] = MsgTypeKeccakRho

	
	binary.BigEndian.PutUint32(payload[4:8], walkID)

	
	nBytes := n.Bytes()
	copy(payload[40-len(nBytes):40], nBytes)

	
	xBytes := x.Bytes()
	copy(payload[72-len(xBytes):72], xBytes)

	return payload
}


func DecodeKeccakRhoPayload(data []byte) (walkID uint32, n, x *big.Int, ok bool) {
	if len(data) < 72 {
		return 0, nil, nil, false
	}

	
	if data[0] != 0x5A || data[1] != 0x45 || data[3] != MsgTypeKeccakRho {
		return 0, nil, nil, false
	}

	walkID = binary.BigEndian.Uint32(data[4:8])
	n = new(big.Int).SetBytes(data[8:40])
	x = new(big.Int).SetBytes(data[40:72])

	return walkID, n, x, true
}


func (h *HybridTransport) BroadcastKeccakRhoStep(target, x *big.Int, walkID uint32) (*KeccakRhoPayload, error) {
	if h.devp2p == nil || !h.devp2p.running {
		return nil, fmt.Errorf("devp2p not running")
	}

	payload := EncodeKeccakRhoPayload(target, x, walkID)
	txHash, err := h.BroadcastZEAM(payload)
	if err != nil {
		return nil, err
	}

	return &KeccakRhoPayload{
		TxHash:    txHash,
		Target:    target,
		WalkID:    walkID,
		X:         x,
		PayloadSz: len(payload),
	}, nil
}


type KeccakRhoBatchResult struct {
	Payloads  []*KeccakRhoPayload
	DPs       []*big.Int 
	TotalSteps int
	Duration  time.Duration
}


func (h *HybridTransport) BroadcastKeccakRhoWalk(target *big.Int, walkID uint32, steps int, dpBits int) (*KeccakRhoBatchResult, error) {
	if h.devp2p == nil || !h.devp2p.running {
		return nil, fmt.Errorf("devp2p not running")
	}

	start := time.Now()
	result := &KeccakRhoBatchResult{
		Payloads: make([]*KeccakRhoPayload, 0),
		DPs:      make([]*big.Int, 0),
	}

	
	xBytes := make([]byte, 32)
	rand.Read(xBytes)
	x := new(big.Int).SetBytes(xBytes)
	x.Mod(x, target)

	for i := 0; i < steps; i++ {
		
		if IsKeccakDP(x, dpBits) {
			result.DPs = append(result.DPs, new(big.Int).Set(x))
		}

		
		if i%100 == 0 {
			payload, err := h.BroadcastKeccakRhoStep(target, x, walkID)
			if err != nil {
				
				fmt.Printf("[KeccakRho] Broadcast error at step %d: %v\n", i, err)
			} else {
				result.Payloads = append(result.Payloads, payload)
			}
		}

		
		x = KeccakStep(target, x)
		result.TotalSteps++
	}

	result.Duration = time.Since(start)
	return result, nil
}


func (h *HybridTransport) BroadcastKeccakRhoParallel(target *big.Int, numWalks int, stepsPerWalk int, dpBits int) ([]*KeccakRhoBatchResult, error) {
	results := make([]*KeccakRhoBatchResult, numWalks)
	var wg sync.WaitGroup
	var errCount int32

	for i := 0; i < numWalks; i++ {
		wg.Add(1)
		go func(walkID int) {
			defer wg.Done()
			result, err := h.BroadcastKeccakRhoWalk(target, uint32(walkID), stepsPerWalk, dpBits)
			if err != nil {
				atomic.AddInt32(&errCount, 1)
				return
			}
			results[walkID] = result
		}(i)
	}

	wg.Wait()

	
	var totalDPs, totalSteps int
	var totalDuration time.Duration
	for _, r := range results {
		if r != nil {
			totalDPs += len(r.DPs)
			totalSteps += r.TotalSteps
			totalDuration += r.Duration
		}
	}

	fmt.Printf("[KeccakRho] %d walks, %d total steps, %d DPs found, %v total time\n",
		numWalks, totalSteps, totalDPs, totalDuration)

	return results, nil
}
