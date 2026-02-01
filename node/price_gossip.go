package node

import (
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type PriceUpdate struct {

	ChainID     uint64
	PoolAddress common.Address

	Reserve0    *big.Int
	Reserve1    *big.Int
	BlockNumber uint64
	Timestamp   time.Time

	SourceNode  common.Address

	Signature   []byte
}

type PriceUpdateMessage struct {
	Version     uint8
	ChainID     uint64
	Pool        common.Address
	Reserve0    []byte
	Reserve1    []byte
	BlockNumber uint64
	Timestamp   uint64
	SourceNode  common.Address
	Signature   []byte
}

func (p *PriceUpdate) Hash() common.Hash {
	data := make([]byte, 0, 128)

	chainBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(chainBytes, p.ChainID)
	data = append(data, chainBytes...)

	data = append(data, p.PoolAddress.Bytes()...)

	r0Bytes := make([]byte, 32)
	r1Bytes := make([]byte, 32)
	if p.Reserve0 != nil {
		p.Reserve0.FillBytes(r0Bytes)
	}
	if p.Reserve1 != nil {
		p.Reserve1.FillBytes(r1Bytes)
	}
	data = append(data, r0Bytes...)
	data = append(data, r1Bytes...)

	blockBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(blockBytes, p.BlockNumber)
	data = append(data, blockBytes...)

	tsBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(tsBytes, uint64(p.Timestamp.Unix()))
	data = append(data, tsBytes...)

	return crypto.Keccak256Hash(data)
}

func (p *PriceUpdate) Sign(key *ecdsa.PrivateKey) error {
	hash := p.Hash()
	sig, err := crypto.Sign(hash.Bytes(), key)
	if err != nil {
		return err
	}
	p.Signature = sig
	p.SourceNode = crypto.PubkeyToAddress(key.PublicKey)
	return nil
}

func (p *PriceUpdate) Verify() (common.Address, error) {
	if len(p.Signature) != 65 {
		return common.Address{}, fmt.Errorf("invalid signature length: %d", len(p.Signature))
	}

	hash := p.Hash()
	pubkey, err := crypto.SigToPub(hash.Bytes(), p.Signature)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to recover pubkey: %w", err)
	}

	addr := crypto.PubkeyToAddress(*pubkey)
	if addr != p.SourceNode {
		return common.Address{}, fmt.Errorf("signer mismatch: got %s, expected %s", addr.Hex(), p.SourceNode.Hex())
	}

	return addr, nil
}

func (p *PriceUpdate) Encode() []byte {
	msg := &PriceUpdateMessage{
		Version:     1,
		ChainID:     p.ChainID,
		Pool:        p.PoolAddress,
		Reserve0:    p.Reserve0.Bytes(),
		Reserve1:    p.Reserve1.Bytes(),
		BlockNumber: p.BlockNumber,
		Timestamp:   uint64(p.Timestamp.Unix()),
		SourceNode:  p.SourceNode,
		Signature:   p.Signature,
	}

	buf := make([]byte, 0, 256)
	buf = append(buf, msg.Version)

	chainBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(chainBytes, msg.ChainID)
	buf = append(buf, chainBytes...)

	buf = append(buf, msg.Pool.Bytes()...)

	r0Len := make([]byte, 2)
	binary.BigEndian.PutUint16(r0Len, uint16(len(msg.Reserve0)))
	buf = append(buf, r0Len...)
	buf = append(buf, msg.Reserve0...)

	r1Len := make([]byte, 2)
	binary.BigEndian.PutUint16(r1Len, uint16(len(msg.Reserve1)))
	buf = append(buf, r1Len...)
	buf = append(buf, msg.Reserve1...)

	blockBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(blockBytes, msg.BlockNumber)
	buf = append(buf, blockBytes...)

	tsBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(tsBytes, msg.Timestamp)
	buf = append(buf, tsBytes...)

	buf = append(buf, msg.SourceNode.Bytes()...)
	buf = append(buf, msg.Signature...)

	return buf
}

func DecodePriceUpdate(data []byte) (*PriceUpdate, error) {
	if len(data) < 1+8+20+2+2+8+8+20+65 {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	offset := 0
	version := data[offset]
	offset++

	if version != 1 {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}

	chainID := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	pool := common.BytesToAddress(data[offset : offset+20])
	offset += 20

	r0Len := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+r0Len > len(data) {
		return nil, fmt.Errorf("invalid r0 length")
	}
	reserve0 := new(big.Int).SetBytes(data[offset : offset+r0Len])
	offset += r0Len

	r1Len := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+r1Len > len(data) {
		return nil, fmt.Errorf("invalid r1 length")
	}
	reserve1 := new(big.Int).SetBytes(data[offset : offset+r1Len])
	offset += r1Len

	if offset+8+8+20+65 > len(data) {
		return nil, fmt.Errorf("data truncated")
	}

	blockNumber := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	timestamp := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	sourceNode := common.BytesToAddress(data[offset : offset+20])
	offset += 20

	signature := make([]byte, 65)
	copy(signature, data[offset:offset+65])

	return &PriceUpdate{
		ChainID:     chainID,
		PoolAddress: pool,
		Reserve0:    reserve0,
		Reserve1:    reserve1,
		BlockNumber: blockNumber,
		Timestamp:   time.Unix(int64(timestamp), 0),
		SourceNode:  sourceNode,
		Signature:   signature,
	}, nil
}

type AggregatedPrice struct {
	ChainID     uint64
	PoolAddress common.Address
	Reserve0    *big.Int
	Reserve1    *big.Int
	BlockNumber uint64
	Timestamp   time.Time

	SourceCount int
	Sources     []common.Address
	Confidence  float64
}

type PriceAggregator struct {
	mu sync.RWMutex

	prices map[uint64]map[common.Address][]*PriceUpdate

	reputation map[common.Address]float64

	config *AggregatorConfig

	OnPriceUpdate func(price *AggregatedPrice)

	updatesReceived uint64
	updatesValid    uint64
	updatesStale    uint64
}

type AggregatorConfig struct {

	MinSources int

	MaxAge time.Duration

	MaxDeviationBPS int64

	PruneInterval time.Duration
}

func DefaultAggregatorConfig() *AggregatorConfig {
	return &AggregatorConfig{
		MinSources:      2,
		MaxAge:          30 * time.Second,
		MaxDeviationBPS: 100,
		PruneInterval:   10 * time.Second,
	}
}

func NewPriceAggregator(config *AggregatorConfig) *PriceAggregator {
	if config == nil {
		config = DefaultAggregatorConfig()
	}

	return &PriceAggregator{
		prices:     make(map[uint64]map[common.Address][]*PriceUpdate),
		reputation: make(map[common.Address]float64),
		config:     config,
	}
}

func (pa *PriceAggregator) AddUpdate(update *PriceUpdate) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	pa.updatesReceived++

	signer, err := update.Verify()
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	if time.Since(update.Timestamp) > pa.config.MaxAge {
		pa.updatesStale++
		return fmt.Errorf("update too old: %v", time.Since(update.Timestamp))
	}

	pa.updatesValid++

	if pa.prices[update.ChainID] == nil {
		pa.prices[update.ChainID] = make(map[common.Address][]*PriceUpdate)
	}

	poolUpdates := pa.prices[update.ChainID][update.PoolAddress]

	filtered := make([]*PriceUpdate, 0, len(poolUpdates))
	for _, u := range poolUpdates {
		if u.SourceNode != signer {
			filtered = append(filtered, u)
		}
	}
	filtered = append(filtered, update)
	pa.prices[update.ChainID][update.PoolAddress] = filtered

	if len(filtered) >= pa.config.MinSources {
		aggregated := pa.aggregate(update.ChainID, update.PoolAddress, filtered)
		if aggregated != nil && pa.OnPriceUpdate != nil {
			pa.OnPriceUpdate(aggregated)
		}
	}

	return nil
}

func (pa *PriceAggregator) aggregate(chainID uint64, pool common.Address, updates []*PriceUpdate) *AggregatedPrice {
	if len(updates) == 0 {
		return nil
	}

	cutoff := time.Now().Add(-pa.config.MaxAge)
	recent := make([]*PriceUpdate, 0, len(updates))
	for _, u := range updates {
		if u.Timestamp.After(cutoff) {
			recent = append(recent, u)
		}
	}

	if len(recent) == 0 {
		return nil
	}

	sort.Slice(recent, func(i, j int) bool {
		return recent[i].BlockNumber > recent[j].BlockNumber
	})

	r0Values := make([]*big.Int, len(recent))
	r1Values := make([]*big.Int, len(recent))
	for i, u := range recent {
		r0Values[i] = u.Reserve0
		r1Values[i] = u.Reserve1
	}

	medianR0 := medianBigInt(r0Values)
	medianR1 := medianBigInt(r1Values)

	confidence := pa.calculateConfidence(r0Values, r1Values, medianR0, medianR1)

	sources := make([]common.Address, len(recent))
	for i, u := range recent {
		sources[i] = u.SourceNode
	}

	return &AggregatedPrice{
		ChainID:     chainID,
		PoolAddress: pool,
		Reserve0:    medianR0,
		Reserve1:    medianR1,
		BlockNumber: recent[0].BlockNumber,
		Timestamp:   time.Now(),
		SourceCount: len(recent),
		Sources:     sources,
		Confidence:  confidence,
	}
}

func (pa *PriceAggregator) calculateConfidence(r0s, r1s []*big.Int, medR0, medR1 *big.Int) float64 {
	if len(r0s) < 2 {
		return 0.5
	}

	maxDev := big.NewInt(pa.config.MaxDeviationBPS)
	agreeing := 0

	for i := range r0s {

		dev0 := deviation(r0s[i], medR0)
		dev1 := deviation(r1s[i], medR1)

		if dev0.Cmp(maxDev) <= 0 && dev1.Cmp(maxDev) <= 0 {
			agreeing++
		}
	}

	return float64(agreeing) / float64(len(r0s))
}

func deviation(a, b *big.Int) *big.Int {
	if b.Sign() == 0 {
		return big.NewInt(10000)
	}

	diff := new(big.Int).Sub(a, b)
	diff.Abs(diff)
	diff.Mul(diff, big.NewInt(10000))
	diff.Div(diff, b)
	return diff
}

func medianBigInt(vals []*big.Int) *big.Int {
	if len(vals) == 0 {
		return big.NewInt(0)
	}

	sorted := make([]*big.Int, len(vals))
	copy(sorted, vals)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Cmp(sorted[j]) < 0
	})

	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {

		sum := new(big.Int).Add(sorted[mid-1], sorted[mid])
		return sum.Div(sum, big.NewInt(2))
	}
	return new(big.Int).Set(sorted[mid])
}

func (pa *PriceAggregator) GetPrice(chainID uint64, pool common.Address) *AggregatedPrice {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	if pa.prices[chainID] == nil {
		return nil
	}

	updates := pa.prices[chainID][pool]
	if len(updates) == 0 {
		return nil
	}

	return pa.aggregate(chainID, pool, updates)
}

func (pa *PriceAggregator) Prune() int {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	cutoff := time.Now().Add(-pa.config.MaxAge * 2)
	removed := 0

	for chainID, chainPools := range pa.prices {
		for pool, updates := range chainPools {
			filtered := make([]*PriceUpdate, 0, len(updates))
			for _, u := range updates {
				if u.Timestamp.After(cutoff) {
					filtered = append(filtered, u)
				} else {
					removed++
				}
			}
			pa.prices[chainID][pool] = filtered
		}
	}

	return removed
}

func (pa *PriceAggregator) Stats() map[string]interface{} {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	poolCount := 0
	for _, chainPools := range pa.prices {
		poolCount += len(chainPools)
	}

	return map[string]interface{}{
		"updates_received": pa.updatesReceived,
		"updates_valid":    pa.updatesValid,
		"updates_stale":    pa.updatesStale,
		"pools_tracked":    poolCount,
		"sources_known":    len(pa.reputation),
	}
}

type PriceBroadcaster struct {
	mu sync.Mutex

	privateKey *ecdsa.PrivateKey
	nodeAddr   common.Address

	broadcast func(chainID uint64, data []byte) error

	lastBroadcast map[string]time.Time

	minInterval time.Duration

	broadcastCount uint64
}

func NewPriceBroadcaster(key *ecdsa.PrivateKey, broadcastFn func(chainID uint64, data []byte) error) *PriceBroadcaster {
	return &PriceBroadcaster{
		privateKey:    key,
		nodeAddr:      crypto.PubkeyToAddress(key.PublicKey),
		broadcast:     broadcastFn,
		lastBroadcast: make(map[string]time.Time),
		minInterval:   5 * time.Second,
	}
}

func (pb *PriceBroadcaster) BroadcastPrice(chainID uint64, pool common.Address, r0, r1 *big.Int, blockNum uint64) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	key := fmt.Sprintf("%d:%s", chainID, pool.Hex())
	if last, ok := pb.lastBroadcast[key]; ok {
		if time.Since(last) < pb.minInterval {
			return nil
		}
	}

	update := &PriceUpdate{
		ChainID:     chainID,
		PoolAddress: pool,
		Reserve0:    r0,
		Reserve1:    r1,
		BlockNumber: blockNum,
		Timestamp:   time.Now(),
	}

	if err := update.Sign(pb.privateKey); err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	data := update.Encode()
	if err := pb.broadcast(chainID, data); err != nil {
		return fmt.Errorf("failed to broadcast: %w", err)
	}

	pb.lastBroadcast[key] = time.Now()
	pb.broadcastCount++

	return nil
}

func (pb *PriceBroadcaster) Stats() map[string]interface{} {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	return map[string]interface{}{
		"node_address":    pb.nodeAddr.Hex(),
		"broadcast_count": pb.broadcastCount,
		"pools_tracked":   len(pb.lastBroadcast),
	}
}

type PoolReserveUpdater interface {
	UpdatePoolFromPriceGossip(chainID uint64, poolAddr common.Address, reserve0, reserve1 *big.Int, blockNum uint64, confidence float64)
}

func (pa *PriceAggregator) WireToPoolRegistry(registry PoolReserveUpdater) {
	pa.OnPriceUpdate = func(price *AggregatedPrice) {
		registry.UpdatePoolFromPriceGossip(
			price.ChainID,
			price.PoolAddress,
			price.Reserve0,
			price.Reserve1,
			price.BlockNumber,
			price.Confidence,
		)
	}
}
