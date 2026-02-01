package discovery

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type PoolRegistry struct {
	mu sync.RWMutex

	pools map[uint64]map[common.Address]*DiscoveredPool

	tokenPools map[uint64]map[common.Address][]common.Address

	pairPools map[uint64]map[string][]common.Address

	tokens *TokenRegistry

	gossipListener *GossipFactoryListener

	OnPoolAdded   func(pool *DiscoveredPool)
	OnPoolUpdated func(pool *DiscoveredPool)

	config *RegistryConfig

	stats DiscoveryStats

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type RegistryConfig struct {

	ScoreUpdateInterval time.Duration

	PruneInactivePools bool
	InactiveThreshold  time.Duration

	MaxPoolsPerChain  int
	MinScoreThreshold float64
}

func DefaultRegistryConfig() *RegistryConfig {
	return &RegistryConfig{
		ScoreUpdateInterval: 5 * time.Minute,
		PruneInactivePools:  true,
		InactiveThreshold:   24 * time.Hour,
		MaxPoolsPerChain:    10000,
		MinScoreThreshold:   0.01,
	}
}

func NewPoolRegistry(config *RegistryConfig) *PoolRegistry {
	if config == nil {
		config = DefaultRegistryConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	pr := &PoolRegistry{
		pools:      make(map[uint64]map[common.Address]*DiscoveredPool),
		tokenPools: make(map[uint64]map[common.Address][]common.Address),
		pairPools:  make(map[uint64]map[string][]common.Address),
		tokens:     NewTokenRegistry(),
		config:     config,
		ctx:        ctx,
		cancel:     cancel,
		stats: DiscoveryStats{
			PoolsByChain:       make(map[uint64]int),
			PoolsByDEX:         make(map[string]int),
			LastDiscoveryBlock: make(map[uint64]uint64),
		},
	}

	pr.gossipListener = NewGossipFactoryListener()
	pr.gossipListener.OnPoolDiscovered = pr.handleNewPool

	return pr
}

func (pr *PoolRegistry) Start() error {

	count := LoadBootstrapPools(pr)
	fmt.Printf("[Registry] Loaded %d bootstrap pools\n", count)

	pr.wg.Add(1)
	go pr.scoreUpdateLoop()

	if pr.config.PruneInactivePools {
		pr.wg.Add(1)
		go pr.pruneLoop()
	}

	fmt.Printf("[Registry] Started pool registry (PARASITIC - no RPC)\n")
	return nil
}

func (pr *PoolRegistry) Stop() {
	pr.cancel()
	pr.wg.Wait()
	fmt.Printf("[Registry] Stopped pool registry\n")
}

func (pr *PoolRegistry) ProcessTransaction(chainID uint64, tx *types.Transaction) {

	pr.gossipListener.ProcessTransaction(chainID, tx)
}

func (pr *PoolRegistry) handleNewPool(pool *DiscoveredPool) {
	pr.AddPool(pool)
}

func (pr *PoolRegistry) AddPool(pool *DiscoveredPool) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	chainID := pool.ChainID

	if pr.pools[chainID] == nil {
		pr.pools[chainID] = make(map[common.Address]*DiscoveredPool)
	}
	if pr.tokenPools[chainID] == nil {
		pr.tokenPools[chainID] = make(map[common.Address][]common.Address)
	}
	if pr.pairPools[chainID] == nil {
		pr.pairPools[chainID] = make(map[string][]common.Address)
	}

	if _, exists := pr.pools[chainID][pool.Address]; exists {
		return nil
	}

	pr.pools[chainID][pool.Address] = pool

	pr.tokenPools[chainID][pool.Token0] = append(pr.tokenPools[chainID][pool.Token0], pool.Address)
	pr.tokenPools[chainID][pool.Token1] = append(pr.tokenPools[chainID][pool.Token1], pool.Address)

	pairKey := pr.pairKey(pool.Token0, pool.Token1)
	pr.pairPools[chainID][pairKey] = append(pr.pairPools[chainID][pairKey], pool.Address)

	pr.stats.TotalPoolsDiscovered++
	pr.stats.PoolsByChain[chainID]++
	pr.stats.PoolsByDEX[pool.DEXType]++
	pr.stats.LastUpdateTime = time.Now()

	if pr.OnPoolAdded != nil {
		go pr.OnPoolAdded(pool)
	}

	fmt.Printf("[Registry] Added pool %s (%s) on chain %d: %s/%s\n",
		pool.Address.Hex()[:10], pool.DEXType, chainID,
		pool.Token0.Hex()[:10], pool.Token1.Hex()[:10])

	return nil
}

func (pr *PoolRegistry) UpdatePoolFromSwap(chainID uint64, poolAddr common.Address, swap *ParsedSwapCall) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if pr.pools[chainID] == nil {
		return
	}

	pool, exists := pr.pools[chainID][poolAddr]
	if !exists {
		return
	}

	pool.SwapCount++
	pool.UpdateCount++
	pool.LastUpdated = time.Now()

	if swap.AmountIn != nil && swap.AmountOut != nil {

		estimatedReserve := new(big.Int).Mul(swap.AmountIn, big.NewInt(100))
		if pool.Reserve0 == nil || pool.Reserve0.Cmp(estimatedReserve) < 0 {
			pool.Reserve0 = estimatedReserve
		}
		estimatedReserve2 := new(big.Int).Mul(swap.AmountOut, big.NewInt(100))
		if pool.Reserve1 == nil || pool.Reserve1.Cmp(estimatedReserve2) < 0 {
			pool.Reserve1 = estimatedReserve2
		}
	}

	if pr.OnPoolUpdated != nil {
		go pr.OnPoolUpdated(pool)
	}
}

func (pr *PoolRegistry) UpdatePoolFromPriceGossip(chainID uint64, poolAddr common.Address, reserve0, reserve1 *big.Int, blockNum uint64, confidence float64) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if pr.pools[chainID] == nil {
		return
	}

	pool, exists := pr.pools[chainID][poolAddr]
	if !exists {
		return
	}

	if confidence < 0.5 {
		return
	}

	if pool.LastBlock >= blockNum {
		return
	}

	pool.Reserve0 = reserve0
	pool.Reserve1 = reserve1
	pool.LastBlock = blockNum
	pool.UpdateCount++
	pool.LastUpdated = time.Now()

	if pr.OnPoolUpdated != nil {
		go pr.OnPoolUpdated(pool)
	}
}

func (pr *PoolRegistry) pairKey(token0, token1 common.Address) string {

	if token0.Hex() < token1.Hex() {
		return token0.Hex() + ":" + token1.Hex()
	}
	return token1.Hex() + ":" + token0.Hex()
}

func (pr *PoolRegistry) GetPool(chainID uint64, address common.Address) *DiscoveredPool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if chainPools, ok := pr.pools[chainID]; ok {
		return chainPools[address]
	}
	return nil
}

func (pr *PoolRegistry) GetPoolsForToken(chainID uint64, token common.Address) []*DiscoveredPool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	addrs := pr.tokenPools[chainID][token]
	pools := make([]*DiscoveredPool, 0, len(addrs))

	for _, addr := range addrs {
		if pool := pr.pools[chainID][addr]; pool != nil {
			pools = append(pools, pool)
		}
	}

	return pools
}

func (pr *PoolRegistry) GetPoolsForPair(chainID uint64, token0, token1 common.Address) []*DiscoveredPool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	pairKey := pr.pairKey(token0, token1)
	addrs := pr.pairPools[chainID][pairKey]
	pools := make([]*DiscoveredPool, 0, len(addrs))

	for _, addr := range addrs {
		if pool := pr.pools[chainID][addr]; pool != nil {
			pools = append(pools, pool)
		}
	}

	return pools
}

func (pr *PoolRegistry) GetAllPools(chainID uint64) []*DiscoveredPool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	chainPools := pr.pools[chainID]
	pools := make([]*DiscoveredPool, 0, len(chainPools))

	for _, pool := range chainPools {
		pools = append(pools, pool)
	}

	return pools
}

func (pr *PoolRegistry) GetFilteredPools(chainID uint64, filter *PoolFilter) []*DiscoveredPool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	var pools []*DiscoveredPool

	for _, pool := range pr.pools[chainID] {
		if filter.Matches(pool) {
			pools = append(pools, pool)
		}
	}

	return pools
}

func (pr *PoolRegistry) GetTopPoolsByScore(chainID uint64, n int) []*DiscoveredPool {
	pools := pr.GetAllPools(chainID)

	sort.Slice(pools, func(i, j int) bool {
		return pools[i].TotalScore > pools[j].TotalScore
	})

	if n > len(pools) {
		n = len(pools)
	}

	return pools[:n]
}

func (pr *PoolRegistry) GetUniqueTokens(chainID uint64) []common.Address {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	tokens := make([]common.Address, 0, len(pr.tokenPools[chainID]))
	for token := range pr.tokenPools[chainID] {
		tokens = append(tokens, token)
	}

	return tokens
}

func (pr *PoolRegistry) scoreUpdateLoop() {
	defer pr.wg.Done()

	ticker := time.NewTicker(pr.config.ScoreUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pr.ctx.Done():
			return
		case <-ticker.C:
			pr.updateAllScores()
		}
	}
}

func (pr *PoolRegistry) updateAllScores() {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	for _, chainPools := range pr.pools {
		for _, pool := range chainPools {
			pr.calculatePoolScore(pool)
		}
	}
}

func (pr *PoolRegistry) calculatePoolScore(pool *DiscoveredPool) {

	depthScore := 0.0
	if pool.Reserve0 != nil && pool.Reserve1 != nil {

		r0 := new(big.Float).SetInt(pool.Reserve0)
		r1 := new(big.Float).SetInt(pool.Reserve1)
		product := new(big.Float).Mul(r0, r1)

		productF, _ := product.Float64()
		if productF > 0 {
			depthScore = min(1.0, productF/1e36)
		}
	}

	activityScore := min(1.0, float64(pool.SwapCount)/1000.0)

	ageHours := time.Since(pool.DiscoveredAt).Hours()
	ageScore := 1.0 / (1.0 + ageHours/168.0)

	pool.DepthScore = depthScore
	pool.ActivityScore = activityScore
	pool.TotalScore = 0.5*depthScore + 0.3*activityScore + 0.2*ageScore
}

func (pr *PoolRegistry) pruneLoop() {
	defer pr.wg.Done()

	ticker := time.NewTicker(pr.config.InactiveThreshold / 2)
	defer ticker.Stop()

	for {
		select {
		case <-pr.ctx.Done():
			return
		case <-ticker.C:
			pr.pruneInactivePools()
		}
	}
}

func (pr *PoolRegistry) pruneInactivePools() {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	threshold := time.Now().Add(-pr.config.InactiveThreshold)

	for chainID, chainPools := range pr.pools {
		for addr, pool := range chainPools {

			if pool.SwapCount == 0 && pool.UpdateCount == 0 {
				continue
			}

			if pool.LastUpdated.Before(threshold) && pool.TotalScore < pr.config.MinScoreThreshold {
				delete(chainPools, addr)
				pr.stats.PoolsByChain[chainID]--
				pr.stats.PoolsByDEX[pool.DEXType]--
			}
		}
	}
}

func (pr *PoolRegistry) Stats() DiscoveryStats {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	uniqueTokens := make(map[common.Address]bool)
	for _, chainTokens := range pr.tokenPools {
		for token := range chainTokens {
			uniqueTokens[token] = true
		}
	}
	pr.stats.UniqueTokens = len(uniqueTokens)

	return pr.stats
}

func (pr *PoolRegistry) ImportPools(pools []*DiscoveredPool) int {
	imported := 0
	for _, pool := range pools {
		if pr.AddPool(pool) == nil {
			imported++
		}
	}
	return imported
}

func (pr *PoolRegistry) ExportPools(chainID uint64) []*DiscoveredPool {
	return pr.GetAllPools(chainID)
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
