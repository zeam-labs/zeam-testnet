package arb

import (
	"encoding/binary"
	"math/big"
	"sync"
	"time"

	"zeam/node"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type ShotgunFilter struct {
	mu sync.RWMutex

	hashPool    []common.Hash
	poolMaxSize int
	poolIdx     int

	accumulator [32]byte

	threshold float64

	totalIngested uint64
	totalAccepted uint64
	totalRejected uint64

	lastIngest time.Time
}

func NewShotgunFilter(poolSize int, threshold float64) *ShotgunFilter {
	if poolSize <= 0 {
		poolSize = 1000
	}
	if threshold <= 0 || threshold > 1 {
		threshold = 0.7
	}

	return &ShotgunFilter{
		hashPool:    make([]common.Hash, poolSize),
		poolMaxSize: poolSize,
		threshold:   threshold,
	}
}

func (sf *ShotgunFilter) IngestHashes(hashes []common.Hash) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	for _, h := range hashes {

		sf.hashPool[sf.poolIdx] = h
		sf.poolIdx = (sf.poolIdx + 1) % sf.poolMaxSize

		for i := 0; i < 32; i++ {
			sf.accumulator[i] ^= h[i]
		}

		sf.totalIngested++
	}

	sf.lastIngest = time.Now()
}

func (sf *ShotgunFilter) Accept(opportunityID [32]byte) bool {
	sf.mu.RLock()
	defer sf.mu.RUnlock()

	if sf.totalIngested == 0 || time.Since(sf.lastIngest) > 30*time.Second {
		return true
	}

	var position [32]byte
	for i := 0; i < 32; i++ {
		position[i] = opportunityID[i] ^ sf.accumulator[i]
	}

	positionVal := binary.BigEndian.Uint64(position[:8])
	normalized := float64(positionVal) / float64(^uint64(0))

	accepted := normalized < sf.threshold

	if accepted {
		sf.mu.RUnlock()
		sf.mu.Lock()
		sf.totalAccepted++
		sf.mu.Unlock()
		sf.mu.RLock()
	} else {
		sf.mu.RUnlock()
		sf.mu.Lock()
		sf.totalRejected++
		sf.mu.Unlock()
		sf.mu.RLock()
	}

	return accepted
}

func (sf *ShotgunFilter) AcceptWithScore(opportunityID [32]byte) (bool, float64) {
	sf.mu.RLock()
	defer sf.mu.RUnlock()

	if sf.totalIngested == 0 {
		return true, 0.5
	}

	var position [32]byte
	for i := 0; i < 32; i++ {
		position[i] = opportunityID[i] ^ sf.accumulator[i]
	}

	positionVal := binary.BigEndian.Uint64(position[:8])
	normalized := float64(positionVal) / float64(^uint64(0))

	score := 1.0 - normalized

	accepted := normalized < sf.threshold

	return accepted, score
}

func (sf *ShotgunFilter) MustAccept(opportunityID [32]byte) float64 {
	_, score := sf.AcceptWithScore(opportunityID)
	return score
}

func (sf *ShotgunFilter) SetThreshold(threshold float64) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if threshold > 0 && threshold <= 1 {
		sf.threshold = threshold
	}
}

func (sf *ShotgunFilter) GetThreshold() float64 {
	sf.mu.RLock()
	defer sf.mu.RUnlock()
	return sf.threshold
}

func (sf *ShotgunFilter) GetStats() ShotgunStats {
	sf.mu.RLock()
	defer sf.mu.RUnlock()

	acceptRate := float64(0)
	if sf.totalAccepted+sf.totalRejected > 0 {
		acceptRate = float64(sf.totalAccepted) / float64(sf.totalAccepted+sf.totalRejected)
	}

	return ShotgunStats{
		TotalIngested: sf.totalIngested,
		TotalAccepted: sf.totalAccepted,
		TotalRejected: sf.totalRejected,
		AcceptRate:    acceptRate,
		Threshold:     sf.threshold,
		PoolSize:      sf.poolMaxSize,
		LastIngest:    sf.lastIngest,
	}
}

type ShotgunStats struct {
	TotalIngested uint64
	TotalAccepted uint64
	TotalRejected uint64
	AcceptRate    float64
	Threshold     float64
	PoolSize      int
	LastIngest    time.Time
}

func (sf *ShotgunFilter) Reset() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	sf.hashPool = make([]common.Hash, sf.poolMaxSize)
	sf.poolIdx = 0
	sf.accumulator = [32]byte{}
	sf.totalIngested = 0
	sf.totalAccepted = 0
	sf.totalRejected = 0
}

func (sf *ShotgunFilter) BatchFilter(opportunityIDs [][32]byte) [][32]byte {
	accepted := make([][32]byte, 0, len(opportunityIDs))

	for _, id := range opportunityIDs {
		if sf.Accept(id) {
			accepted = append(accepted, id)
		}
	}

	return accepted
}

func (sf *ShotgunFilter) RankedFilter(opportunityIDs [][32]byte) []RankedOpportunity {
	sf.mu.RLock()
	accumulator := sf.accumulator
	sf.mu.RUnlock()

	ranked := make([]RankedOpportunity, len(opportunityIDs))

	for i, id := range opportunityIDs {

		var position [32]byte
		for j := 0; j < 32; j++ {
			position[j] = id[j] ^ accumulator[j]
		}

		positionVal := binary.BigEndian.Uint64(position[:8])
		score := 1.0 - float64(positionVal)/float64(^uint64(0))

		ranked[i] = RankedOpportunity{
			ID:       id,
			Score:    score,
			Accepted: score > (1.0 - sf.threshold),
		}
	}

	for i := 0; i < len(ranked)-1; i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].Score > ranked[i].Score {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}

	return ranked
}

type RankedOpportunity struct {
	ID       [32]byte
	Score    float64
	Accepted bool
}

func (sf *ShotgunFilter) ConnectToTransport(transport *node.MultiChainTransport) {
	originalHandler := transport.OnTxReceived
	transport.OnTxReceived = func(chainID uint64, hashes []common.Hash) {

		sf.IngestHashes(hashes)

		if originalHandler != nil {
			originalHandler(chainID, hashes)
		}
	}
}

func ComputeOpportunityID(
	buyChain, sellChain uint64,
	tokenIn, tokenOut common.Address,
	amountIn *big.Int,
	timestamp time.Time,
) [32]byte {
	data := make([]byte, 0, 128)

	chainBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(chainBuf, buyChain)
	data = append(data, chainBuf...)
	binary.BigEndian.PutUint64(chainBuf, sellChain)
	data = append(data, chainBuf...)

	data = append(data, tokenIn.Bytes()...)
	data = append(data, tokenOut.Bytes()...)

	if amountIn != nil {
		data = append(data, amountIn.Bytes()...)
	}

	timeBucket := timestamp.Unix() / 10
	binary.BigEndian.PutUint64(chainBuf, uint64(timeBucket))
	data = append(data, chainBuf...)

	return crypto.Keccak256Hash(data)
}
