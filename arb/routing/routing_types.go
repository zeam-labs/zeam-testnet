package routing

import (
	"math/big"
	"time"

	"zeam/arb/graph"

	"github.com/ethereum/go-ethereum/common"
)

type RouteHop struct {

	ChainID uint64
	Pool    common.Address
	DEX     string

	TokenIn  common.Address
	TokenOut common.Address

	AmountIn     *big.Int
	AmountOut    *big.Int
	MinAmountOut *big.Int

	PriceImpactBPS int64
	Fee            *big.Int
	Weight         float64
}

type ArbitrageRoute struct {

	ID [32]byte

	Hops []RouteHop

	StartToken  common.Address
	StartChain  uint64
	StartAmount *big.Int
	EndAmount   *big.Int

	GrossProfit *big.Int
	TotalFees   *big.Int
	GasEstimate *big.Int
	NetProfit   *big.Int

	ProfitBPS    int64
	TotalWeight  float64

	MinViableLiquidity *big.Int
	MaxSlippageBPS     int64

	IsCrossChain bool
	ChainIDs     []uint64

	DetectedAt    time.Time
	ExpiresAt     time.Time
	ValidityMS    int64

	PreSigned    bool
	PreSignedTxs map[uint64][]byte

	DetectionMethod string
}

func (r *ArbitrageRoute) IsValid() bool {
	return time.Now().Before(r.ExpiresAt) && r.NetProfit != nil && r.NetProfit.Sign() > 0
}

func (r *ArbitrageRoute) HopCount() int {
	return len(r.Hops)
}

func (r *ArbitrageRoute) GetTokenSequence() []common.Address {
	if len(r.Hops) == 0 {
		return nil
	}

	tokens := make([]common.Address, 0, len(r.Hops)+1)
	tokens = append(tokens, r.Hops[0].TokenIn)
	for _, hop := range r.Hops {
		tokens = append(tokens, hop.TokenOut)
	}
	return tokens
}

func (r *ArbitrageRoute) GetPoolSequence() []common.Address {
	pools := make([]common.Address, len(r.Hops))
	for i, hop := range r.Hops {
		pools[i] = hop.Pool
	}
	return pools
}

type RouteConstraints struct {

	MaxHops    int
	MinHops    int

	MinProfitWei *big.Int
	MinProfitBPS int64

	MaxGasGwei     int64
	MaxGasCostWei  *big.Int

	MinLiquidityUSD float64
	MinTradeSize    *big.Int

	MaxPriceImpactBPS int64
	MaxTotalSlippage  int64

	AllowedTokens  []common.Address
	ExcludedTokens []common.Address

	AllowedDEXs   []string
	ExcludedPools []common.Address

	AllowTriangular  bool
	AllowCrossChain  bool
	RequireBluechip  bool
}

func DefaultRouteConstraints() *RouteConstraints {
	return &RouteConstraints{
		MaxHops:           4,
		MinHops:           2,
		MinProfitWei:      big.NewInt(1e15),
		MinProfitBPS:      5,
		MaxGasGwei:        50,
		MaxPriceImpactBPS: 100,
		MaxTotalSlippage:  200,
		MinLiquidityUSD:   10000,
		AllowTriangular:   true,
		AllowCrossChain:   true,
		RequireBluechip:   false,
	}
}

func (c *RouteConstraints) Validate(route *ArbitrageRoute) bool {

	if len(route.Hops) < c.MinHops || len(route.Hops) > c.MaxHops {
		return false
	}

	if route.NetProfit == nil || route.NetProfit.Cmp(c.MinProfitWei) < 0 {
		return false
	}
	if route.ProfitBPS < c.MinProfitBPS {
		return false
	}

	if c.MaxGasCostWei != nil && route.GasEstimate != nil {
		if route.GasEstimate.Cmp(c.MaxGasCostWei) > 0 {
			return false
		}
	}

	if route.IsCrossChain && !c.AllowCrossChain {
		return false
	}

	for _, hop := range route.Hops {

		if hop.PriceImpactBPS > c.MaxPriceImpactBPS {
			return false
		}

		for _, excluded := range c.ExcludedPools {
			if hop.Pool == excluded {
				return false
			}
		}

		for _, excluded := range c.ExcludedTokens {
			if hop.TokenIn == excluded || hop.TokenOut == excluded {
				return false
			}
		}

		if len(c.AllowedDEXs) > 0 {
			found := false
			for _, dex := range c.AllowedDEXs {
				if hop.DEX == dex {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

type RouteFinderResult struct {
	Routes        []*ArbitrageRoute
	TotalSearched int
	ValidCount    int
	BestProfit    *big.Int
	SearchTime    time.Duration
	Method        string
}

type RouteFinder interface {

	FindRoutes(
		g *graph.TokenGraph,
		startToken common.Address,
		chainID uint64,
		constraints *RouteConstraints,
	) *RouteFinderResult

	FindTriangular(
		g *graph.TokenGraph,
		chainID uint64,
		constraints *RouteConstraints,
	) *RouteFinderResult
}

type RouteCache struct {
	routes   map[[32]byte]*ArbitrageRoute
	expiries map[[32]byte]time.Time
}

func NewRouteCache() *RouteCache {
	return &RouteCache{
		routes:   make(map[[32]byte]*ArbitrageRoute),
		expiries: make(map[[32]byte]time.Time),
	}
}

func (c *RouteCache) Add(route *ArbitrageRoute) {
	c.routes[route.ID] = route
	c.expiries[route.ID] = route.ExpiresAt
}

func (c *RouteCache) Get(id [32]byte) *ArbitrageRoute {
	route, ok := c.routes[id]
	if !ok {
		return nil
	}

	if time.Now().After(c.expiries[id]) {
		delete(c.routes, id)
		delete(c.expiries, id)
		return nil
	}

	return route
}

func (c *RouteCache) GetValid() []*ArbitrageRoute {
	now := time.Now()
	var valid []*ArbitrageRoute

	for id, route := range c.routes {
		if now.Before(c.expiries[id]) {
			valid = append(valid, route)
		} else {
			delete(c.routes, id)
			delete(c.expiries, id)
		}
	}

	return valid
}

func (c *RouteCache) Prune() int {
	now := time.Now()
	pruned := 0

	for id, expiry := range c.expiries {
		if now.After(expiry) {
			delete(c.routes, id)
			delete(c.expiries, id)
			pruned++
		}
	}

	return pruned
}

func (c *RouteCache) Size() int {
	return len(c.routes)
}
