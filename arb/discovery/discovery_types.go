package discovery

import (
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type FactoryInfo struct {
	Address       common.Address
	ChainID       uint64
	DEXType       string
	Version       int
	EventTopic    common.Hash
	EventName     string
}

type DiscoveredPool struct {
	Address common.Address
	ChainID uint64
	DEXType string
	Token0  common.Address
	Token1  common.Address
	Fee     uint32

	TickSpacing int

	Stable bool

	Reserve0     *big.Int
	Reserve1     *big.Int
	Liquidity    *big.Int
	SqrtPriceX96 *big.Int
	Tick         int

	DiscoveredAt  time.Time
	LastUpdated   time.Time
	CreatedBlock  uint64
	LastBlock     uint64

	VolumeScore   float64
	DepthScore    float64
	ActivityScore float64
	TotalScore    float64

	UpdateCount uint64
	SwapCount   uint64
}

func (p *DiscoveredPool) GetPrice() *big.Float {
	if p.Reserve0 == nil || p.Reserve1 == nil || p.Reserve0.Sign() == 0 {
		return nil
	}
	price := new(big.Float).Quo(
		new(big.Float).SetInt(p.Reserve1),
		new(big.Float).SetInt(p.Reserve0),
	)
	return price
}

func (p *DiscoveredPool) HasToken(token common.Address) bool {
	return p.Token0 == token || p.Token1 == token
}

func (p *DiscoveredPool) OtherToken(token common.Address) common.Address {
	if p.Token0 == token {
		return p.Token1
	}
	return p.Token0
}

func (p *DiscoveredPool) IsV3() bool {
	return p.DEXType == "uniswap_v3"
}

type PoolFilter struct {
	MinLiquidityUSD float64
	MinVolumeUSD    float64
	MaxAgeHours     int
	MinTotalScore   float64
	IncludeStable   bool
	IncludeVolatile bool
	TokenWhitelist  []common.Address
	TokenBlacklist  []common.Address
	DEXWhitelist    []string
}

func DefaultPoolFilter() *PoolFilter {
	return &PoolFilter{
		MinLiquidityUSD: 10000,
		MinVolumeUSD:    1000,
		MaxAgeHours:     24,
		MinTotalScore:   0.1,
		IncludeStable:   true,
		IncludeVolatile: true,
	}
}

func (f *PoolFilter) Matches(pool *DiscoveredPool) bool {

	if pool.TotalScore < f.MinTotalScore {
		return false
	}

	if f.MaxAgeHours > 0 {
		maxAge := time.Duration(f.MaxAgeHours) * time.Hour
		if time.Since(pool.LastUpdated) > maxAge {
			return false
		}
	}

	if pool.Stable && !f.IncludeStable {
		return false
	}
	if !pool.Stable && !f.IncludeVolatile {
		return false
	}

	if len(f.TokenWhitelist) > 0 {
		found := false
		for _, t := range f.TokenWhitelist {
			if pool.HasToken(t) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for _, t := range f.TokenBlacklist {
		if pool.HasToken(t) {
			return false
		}
	}

	if len(f.DEXWhitelist) > 0 {
		found := false
		for _, d := range f.DEXWhitelist {
			if pool.DEXType == d {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

type PoolTier struct {
	Name            string
	MinLiquidityUSD float64
	MaxHops         int
	Priority        int
}

func DefaultTiers() []PoolTier {
	return []PoolTier{
		{"blue_chip", 1_000_000, 4, 1},
		{"major", 100_000, 3, 2},
		{"medium", 10_000, 2, 3},
		{"long_tail", 1_000, 2, 4},
	}
}

type PoolUpdate struct {
	Pool        common.Address
	ChainID     uint64
	Reserve0    *big.Int
	Reserve1    *big.Int
	Liquidity   *big.Int
	BlockNumber uint64
	Timestamp   time.Time
}

type TokenInfo struct {
	Address  common.Address
	ChainID  uint64
	Symbol   string
	Decimals uint8
	IsStable bool
	IsNative bool
	PriceUSD float64
}

type TokenRegistry struct {
	mu     sync.RWMutex
	tokens map[uint64]map[common.Address]*TokenInfo

	crossChain map[common.Address]map[uint64]common.Address
}

func NewTokenRegistry() *TokenRegistry {
	return &TokenRegistry{
		tokens:     make(map[uint64]map[common.Address]*TokenInfo),
		crossChain: make(map[common.Address]map[uint64]common.Address),
	}
}

func (r *TokenRegistry) Add(token *TokenInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.tokens[token.ChainID] == nil {
		r.tokens[token.ChainID] = make(map[common.Address]*TokenInfo)
	}
	r.tokens[token.ChainID][token.Address] = token
}

func (r *TokenRegistry) Get(chainID uint64, address common.Address) *TokenInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if chainTokens, ok := r.tokens[chainID]; ok {
		return chainTokens[address]
	}
	return nil
}

func (r *TokenRegistry) AddCrossChainMapping(canonical common.Address, chainID uint64, address common.Address) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.crossChain[canonical] == nil {
		r.crossChain[canonical] = make(map[uint64]common.Address)
	}
	r.crossChain[canonical][chainID] = address
}

func (r *TokenRegistry) GetCrossChainAddress(canonical common.Address, targetChain uint64) (common.Address, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if chains, ok := r.crossChain[canonical]; ok {
		if addr, exists := chains[targetChain]; exists {
			return addr, true
		}
	}
	return common.Address{}, false
}

type DiscoveryStats struct {
	TotalPoolsDiscovered int
	PoolsByChain         map[uint64]int
	PoolsByDEX           map[string]int
	UniqueTokens         int
	LastDiscoveryBlock   map[uint64]uint64
	LastUpdateTime       time.Time
}
