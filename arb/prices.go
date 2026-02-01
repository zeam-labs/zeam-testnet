package arb

import (
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"zeam/arb/dex"
	"zeam/node"

	"github.com/ethereum/go-ethereum/common"
)

type PriceFeed struct {
	mu sync.RWMutex

	prices map[string][]*PricePoint

	dexRegistry *dex.Registry

	pools map[common.Address]*dex.PoolInfo

	transport *node.MultiChainTransport

	config *PriceFeedConfig

	OnPriceUpdate func(*PricePoint)
	OnSpreadAlert func(pair string, buyChain, sellChain uint64, spreadBPS int64)

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type PriceFeedConfig struct {
	MaxPriceAge      time.Duration
	MaxPricesPerPair int
	SpreadAlertBPS   int64
}

func DefaultPriceFeedConfig() *PriceFeedConfig {
	return &PriceFeedConfig{
		MaxPriceAge:      30 * time.Second,
		MaxPricesPerPair: 100,
		SpreadAlertBPS:   10,
	}
}

func NewPriceFeed(transport *node.MultiChainTransport, config *PriceFeedConfig) *PriceFeed {
	if config == nil {
		config = DefaultPriceFeedConfig()
	}

	pf := &PriceFeed{
		prices:      make(map[string][]*PricePoint),
		dexRegistry: dex.NewRegistry(),
		pools:       make(map[common.Address]*dex.PoolInfo),
		transport:   transport,
		config:      config,
		stopCh:      make(chan struct{}),
	}

	pf.dexRegistry.RegisterRouter(dex.DEXUniswapV2, dex.NewUniswapV2Router())
	pf.dexRegistry.RegisterRouter(dex.DEXUniswapV3, dex.NewUniswapV3Router())

	for _, pools := range dex.KnownPools() {
		for _, pool := range pools {
			pf.pools[pool.Address] = pool
			pf.dexRegistry.RegisterPool(pool)
		}
	}

	return pf
}

func (pf *PriceFeed) Start() error {
	pf.mu.Lock()
	if pf.running {
		pf.mu.Unlock()
		return nil
	}
	pf.running = true
	pf.mu.Unlock()

	return nil
}

func (pf *PriceFeed) Stop() {
	pf.mu.Lock()
	if !pf.running {
		pf.mu.Unlock()
		return
	}
	pf.running = false
	pf.mu.Unlock()
}

func (pf *PriceFeed) handleTxHashes(chainID uint64, hashes []common.Hash) {

}

func (pf *PriceFeed) pruneOldPrices() {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	cutoff := time.Now().Add(-pf.config.MaxPriceAge)

	for pairID, prices := range pf.prices {

		valid := make([]*PricePoint, 0, len(prices))
		for _, p := range prices {
			if p.Timestamp.After(cutoff) {
				valid = append(valid, p)
			}
		}

		if len(valid) > pf.config.MaxPricesPerPair {
			valid = valid[len(valid)-pf.config.MaxPricesPerPair:]
		}

		if len(valid) == 0 {
			delete(pf.prices, pairID)
		} else {
			pf.prices[pairID] = valid
		}
	}
}

func (pf *PriceFeed) IngestPrice(price *PricePoint) {
	pf.mu.Lock()
	defer pf.mu.Unlock()

	pairID := price.Pair.ID()

	prices := pf.prices[pairID]
	prices = append(prices, price)

	cutoff := time.Now().Add(-pf.config.MaxPriceAge)
	valid := prices[:0]
	for _, p := range prices {
		if p.Timestamp.After(cutoff) {
			valid = append(valid, p)
		}
	}
	prices = valid

	sort.Slice(prices, func(i, j int) bool {
		return prices[i].Timestamp.Before(prices[j].Timestamp)
	})

	if len(prices) > pf.config.MaxPricesPerPair {
		prices = prices[len(prices)-pf.config.MaxPricesPerPair:]
	}

	pf.prices[pairID] = prices

	if pf.OnPriceUpdate != nil {
		go pf.OnPriceUpdate(price)
	}

	pf.checkSpreadAlert(pairID)
}

func (pf *PriceFeed) IngestSwapEvent(swap *SwapEvent) {
	pool, ok := pf.pools[swap.Pool]
	if !ok {
		return
	}

	var price *big.Float
	if swap.Token0In.Sign() > 0 && swap.Token1Out.Sign() > 0 {

		price = new(big.Float).Quo(
			new(big.Float).SetInt(swap.Token1Out),
			new(big.Float).SetInt(swap.Token0In),
		)
	} else if swap.Token1In.Sign() > 0 && swap.Token0Out.Sign() > 0 {

		price = new(big.Float).Quo(
			new(big.Float).SetInt(swap.Token0Out),
			new(big.Float).SetInt(swap.Token1In),
		)

		price = new(big.Float).Quo(big.NewFloat(1), price)
	}

	if price == nil {
		return
	}

	tokens := CommonTokens()
	var token0Info, token1Info TokenInfo
	if chainTokens, ok := tokens[swap.ChainID]; ok {
		for _, t := range chainTokens {
			if t.Address == pool.Token0 {
				token0Info = t
			}
			if t.Address == pool.Token1 {
				token1Info = t
			}
		}
	}

	pp := &PricePoint{
		Pair: TokenPair{
			Token0: token0Info,
			Token1: token1Info,
		},
		ChainID:     swap.ChainID,
		DEX:         string(pool.DEX),
		PoolAddress: swap.Pool,
		Price:       price,
		Timestamp:   swap.Timestamp,
		BlockNumber: swap.BlockNumber,
		TxHash:      swap.TxHash,
		Source:      swap.Source,
	}

	pf.IngestPrice(pp)
}

func (pf *PriceFeed) checkSpreadAlert(pairID string) {
	if pf.OnSpreadAlert == nil {
		return
	}

	prices := pf.prices[pairID]
	if len(prices) < 2 {
		return
	}

	chainPrices := make(map[uint64]*PricePoint)
	for _, p := range prices {

		if existing, ok := chainPrices[p.ChainID]; !ok || p.Timestamp.After(existing.Timestamp) {
			chainPrices[p.ChainID] = p
		}
	}

	if len(chainPrices) < 2 {
		return
	}

	var chains []uint64
	for c := range chainPrices {
		chains = append(chains, c)
	}

	for i := 0; i < len(chains); i++ {
		for j := i + 1; j < len(chains); j++ {
			p1 := chainPrices[chains[i]]
			p2 := chainPrices[chains[j]]

			spread := pf.calculateSpreadBPS(p1.Price, p2.Price)
			if spread > pf.config.SpreadAlertBPS || spread < -pf.config.SpreadAlertBPS {
				buyChain, sellChain := chains[i], chains[j]
				if spread < 0 {
					buyChain, sellChain = sellChain, buyChain
					spread = -spread
				}
				go pf.OnSpreadAlert(pairID, buyChain, sellChain, spread)
			}
		}
	}
}

func (pf *PriceFeed) calculateSpreadBPS(price1, price2 *big.Float) int64 {
	if price1 == nil || price2 == nil || price1.Sign() == 0 {
		return 0
	}

	diff := new(big.Float).Sub(price2, price1)
	spread := new(big.Float).Quo(diff, price1)
	spread.Mul(spread, big.NewFloat(10000))

	spreadInt, _ := spread.Int64()
	return spreadInt
}

func (pf *PriceFeed) GetLatestPrice(pairID string, chainID uint64) *PricePoint {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	prices := pf.prices[pairID]
	if len(prices) == 0 {
		return nil
	}

	for i := len(prices) - 1; i >= 0; i-- {
		if prices[i].ChainID == chainID {
			return prices[i]
		}
	}
	return nil
}

func (pf *PriceFeed) GetLatestPrices(pairID string) map[uint64]*PricePoint {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	result := make(map[uint64]*PricePoint)
	prices := pf.prices[pairID]

	for i := len(prices) - 1; i >= 0; i-- {
		p := prices[i]
		if _, exists := result[p.ChainID]; !exists {
			result[p.ChainID] = p
		}
	}

	return result
}

func (pf *PriceFeed) GetBestBuySell(pairID string) (buy, sell *PricePoint, spreadBPS int64) {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	prices := pf.prices[pairID]
	if len(prices) < 2 {
		return nil, nil, 0
	}

	type chainDEXKey struct {
		chainID uint64
		dex     string
	}
	dexPrices := make(map[chainDEXKey]*PricePoint)
	for i := len(prices) - 1; i >= 0; i-- {
		p := prices[i]
		key := chainDEXKey{chainID: p.ChainID, dex: p.DEX}
		if _, exists := dexPrices[key]; !exists {
			dexPrices[key] = p
		}
	}

	if len(dexPrices) < 2 {
		return nil, nil, 0
	}

	var minPrice, maxPrice *PricePoint
	for _, p := range dexPrices {
		if minPrice == nil || p.Price.Cmp(minPrice.Price) < 0 {
			minPrice = p
		}
		if maxPrice == nil || p.Price.Cmp(maxPrice.Price) > 0 {
			maxPrice = p
		}
	}

	if minPrice == nil || maxPrice == nil {
		return nil, nil, 0
	}

	if minPrice.ChainID == maxPrice.ChainID && minPrice.DEX == maxPrice.DEX {
		return nil, nil, 0
	}

	spreadBPS = pf.calculateSpreadBPS(minPrice.Price, maxPrice.Price)

	return minPrice, maxPrice, spreadBPS
}

func (pf *PriceFeed) GetAllSpreads() map[string]*SpreadInfo {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	result := make(map[string]*SpreadInfo)

	for pairID := range pf.prices {
		buy, sell, spread := pf.GetBestBuySell(pairID)
		if buy != nil && sell != nil {
			result[pairID] = &SpreadInfo{
				PairID:     pairID,
				BuyChain:   buy.ChainID,
				SellChain:  sell.ChainID,
				BuyPrice:   buy.Price,
				SellPrice:  sell.Price,
				SpreadBPS:  spread,
				BuyDEX:     buy.DEX,
				SellDEX:    sell.DEX,
				Timestamp:  time.Now(),
			}
		}
	}

	return result
}

type SpreadInfo struct {
	PairID    string
	BuyChain  uint64
	SellChain uint64
	BuyPrice  *big.Float
	SellPrice *big.Float
	SpreadBPS int64
	BuyDEX    string
	SellDEX   string
	Timestamp time.Time
}

func (pf *PriceFeed) GetPriceHistory(pairID string, limit int) []*PricePoint {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	prices := pf.prices[pairID]
	if len(prices) == 0 {
		return nil
	}

	if limit <= 0 || limit > len(prices) {
		limit = len(prices)
	}

	result := make([]*PricePoint, limit)
	copy(result, prices[len(prices)-limit:])
	return result
}

func (pf *PriceFeed) GetTrackedPairs() []string {
	pf.mu.RLock()
	defer pf.mu.RUnlock()

	pairs := make([]string, 0, len(pf.prices))
	for pairID := range pf.prices {
		pairs = append(pairs, pairID)
	}
	return pairs
}

func (pf *PriceFeed) SetPrice(pairID string, chainID uint64, dexName string, price *big.Float) {
	tokens := CommonTokens()
	chainTokens := tokens[chainID]

	var token0, token1 TokenInfo

	parts := strings.Split(pairID, "-")
	if len(parts) == 2 {
		symbol0 := parts[0]
		symbol1 := parts[1]

		if symbol0 == "ETH" {
			symbol0 = "WETH"
		}
		if symbol1 == "ETH" {
			symbol1 = "WETH"
		}

		if t, ok := chainTokens[symbol0]; ok {
			token0 = t
		}
		if t, ok := chainTokens[symbol1]; ok {
			token1 = t
		}
	}

	pp := &PricePoint{
		Pair: TokenPair{
			Token0: token0,
			Token1: token1,
		},
		ChainID:   chainID,
		DEX:       dexName,
		Price:     price,
		Timestamp: time.Now(),
		Source:    PriceSourceRPC,
	}

	pf.IngestPrice(pp)
}
