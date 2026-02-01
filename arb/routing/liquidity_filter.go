package routing

import (
	"math/big"
	"sort"

	"zeam/arb/graph"

	"github.com/ethereum/go-ethereum/common"
)

type LiquidityFilter struct {
	config *LiquidityConfig
}

type LiquidityConfig struct {

	MinPoolLiquidity *big.Int

	MinPoolLiquidityUSD float64

	MaxHopImpactBPS int64

	MaxTotalImpactBPS int64

	RequireBluechipHub bool

	Tiers []LiquidityTier
}

type LiquidityTier struct {
	Name            string
	MinLiquidityUSD float64
	MaxHops         int
	Priority        int
}

func DefaultLiquidityConfig() *LiquidityConfig {
	return &LiquidityConfig{
		MinPoolLiquidity:    big.NewInt(1e18),
		MinPoolLiquidityUSD: 10000,
		MaxHopImpactBPS:     100,
		MaxTotalImpactBPS:   300,
		RequireBluechipHub:  false,
		Tiers: []LiquidityTier{
			{"blue_chip", 1_000_000, 4, 1},
			{"major", 100_000, 3, 2},
			{"medium", 10_000, 2, 3},
			{"long_tail", 1_000, 2, 4},
		},
	}
}

func NewLiquidityFilter(config *LiquidityConfig) *LiquidityFilter {
	if config == nil {
		config = DefaultLiquidityConfig()
	}
	return &LiquidityFilter{config: config}
}

func (lf *LiquidityFilter) FilterRoutes(routes []*ArbitrageRoute) []*ArbitrageRoute {
	var filtered []*ArbitrageRoute

	for _, route := range routes {
		if lf.ValidateRoute(route) {
			filtered = append(filtered, route)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].NetProfit == nil {
			return false
		}
		if filtered[j].NetProfit == nil {
			return true
		}
		return filtered[i].NetProfit.Cmp(filtered[j].NetProfit) > 0
	})

	return filtered
}

func (lf *LiquidityFilter) ValidateRoute(route *ArbitrageRoute) bool {
	if route == nil || len(route.Hops) == 0 {
		return false
	}

	if route.MinViableLiquidity != nil {
		if route.MinViableLiquidity.Cmp(lf.config.MinPoolLiquidity) < 0 {
			return false
		}
	}

	var totalImpact int64
	for _, hop := range route.Hops {
		if hop.PriceImpactBPS > lf.config.MaxHopImpactBPS {
			return false
		}
		totalImpact += hop.PriceImpactBPS
	}

	if totalImpact > lf.config.MaxTotalImpactBPS {
		return false
	}

	if lf.config.RequireBluechipHub && len(route.Hops) > 2 {
		hasBluechip := false
		for i := 1; i < len(route.Hops); i++ {
			if graph.IsBluechip(route.StartChain, route.Hops[i].TokenIn) {
				hasBluechip = true
				break
			}
		}
		if !hasBluechip {
			return false
		}
	}

	return true
}

func (lf *LiquidityFilter) FindOptimalTradeSize(
	route *ArbitrageRoute,
	g *graph.TokenGraph,
	minAmount, maxAmount *big.Int,
) (*big.Int, *big.Int) {
	if maxAmount == nil || maxAmount.Sign() <= 0 {

		if route.MinViableLiquidity != nil && route.MinViableLiquidity.Sign() > 0 {
			maxAmount = route.MinViableLiquidity
		} else {

			maxAmount = new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18))
		}
	}

	if minAmount == nil {
		minAmount = big.NewInt(1e16)
	}

	path := lf.routeToGraphPath(route, g)
	if path == nil {
		return minAmount, big.NewInt(0)
	}

	return graph.FindOptimalAmount(path, minAmount, maxAmount)
}

func (lf *LiquidityFilter) routeToGraphPath(
	route *ArbitrageRoute,
	g *graph.TokenGraph,
) *graph.GraphPath {
	if len(route.Hops) == 0 {
		return nil
	}

	path := &graph.GraphPath{
		Nodes: make([]graph.PathNode, 0, len(route.Hops)+1),
	}

	path.Nodes = append(path.Nodes, graph.PathNode{
		Token:   route.Hops[0].TokenIn,
		ChainID: route.StartChain,
		Edge:    nil,
	})

	for _, hop := range route.Hops {
		edge := g.GetEdge(hop.ChainID, hop.Pool)
		path.Nodes = append(path.Nodes, graph.PathNode{
			Token:   hop.TokenOut,
			ChainID: hop.ChainID,
			Edge:    edge,
		})
		if edge != nil {
			path.TotalWeight += edge.GetWeight(hop.TokenIn)
		}
	}

	path.IsCycle = route.Hops[0].TokenIn == route.Hops[len(route.Hops)-1].TokenOut

	return path
}

func (lf *LiquidityFilter) GetTierForPool(liquidityUSD float64) *LiquidityTier {
	for i := range lf.config.Tiers {
		tier := &lf.config.Tiers[i]
		if liquidityUSD >= tier.MinLiquidityUSD {
			return tier
		}
	}
	return nil
}

func (lf *LiquidityFilter) GetMaxHopsForPath(
	route *ArbitrageRoute,
	poolLiquidities map[common.Address]float64,
) int {
	minTierMaxHops := 100

	for _, hop := range route.Hops {
		liquidity, ok := poolLiquidities[hop.Pool]
		if !ok {
			liquidity = 0
		}

		tier := lf.GetTierForPool(liquidity)
		if tier == nil {
			return 0
		}

		if tier.MaxHops < minTierMaxHops {
			minTierMaxHops = tier.MaxHops
		}
	}

	return minTierMaxHops
}

func (lf *LiquidityFilter) CalculateRouteCapacity(
	route *ArbitrageRoute,
	g *graph.TokenGraph,
) *big.Int {
	if len(route.Hops) == 0 {
		return big.NewInt(0)
	}

	var minCapacity *big.Int

	for _, hop := range route.Hops {
		edge := g.GetEdge(hop.ChainID, hop.Pool)
		if edge == nil {
			return big.NewInt(0)
		}

		maxAmount := edge.GetMaxAmount(hop.TokenIn)
		if maxAmount == nil || maxAmount.Sign() == 0 {
			return big.NewInt(0)
		}

		if minCapacity == nil || maxAmount.Cmp(minCapacity) < 0 {
			minCapacity = maxAmount
		}
	}

	return minCapacity
}

func (lf *LiquidityFilter) ScoreRouteByLiquidity(
	route *ArbitrageRoute,
	g *graph.TokenGraph,
) float64 {
	if len(route.Hops) == 0 {
		return 0
	}

	var totalScore float64
	var hopCount int

	for _, hop := range route.Hops {
		edge := g.GetEdge(hop.ChainID, hop.Pool)
		if edge == nil {
			continue
		}

		var reserveScore float64
		if edge.Reserve0 != nil && edge.Reserve1 != nil {

			r0f, _ := new(big.Float).SetInt(edge.Reserve0).Float64()
			r1f, _ := new(big.Float).SetInt(edge.Reserve1).Float64()
			if r0f > 0 && r1f > 0 {
				reserveScore = min(1.0, (r0f*r1f)/(1e36))
			}
		}

		var bluechipBonus float64
		if graph.IsBluechip(hop.ChainID, hop.TokenIn) || graph.IsBluechip(hop.ChainID, hop.TokenOut) {
			bluechipBonus = 0.2
		}

		impactScore := 1.0 - float64(hop.PriceImpactBPS)/10000.0

		hopScore := (reserveScore*0.5 + impactScore*0.3 + bluechipBonus)
		totalScore += hopScore
		hopCount++
	}

	if hopCount == 0 {
		return 0
	}

	return totalScore / float64(hopCount)
}

func (lf *LiquidityFilter) FilterByBluechipHub(routes []*ArbitrageRoute) []*ArbitrageRoute {
	var filtered []*ArbitrageRoute

	for _, route := range routes {
		if lf.routeHasBluechipHub(route) {
			filtered = append(filtered, route)
		}
	}

	return filtered
}

func (lf *LiquidityFilter) routeHasBluechipHub(route *ArbitrageRoute) bool {

	if len(route.Hops) <= 2 {
		return true
	}

	for i := 1; i < len(route.Hops); i++ {
		tokenIn := route.Hops[i].TokenIn
		if graph.IsBluechip(route.StartChain, tokenIn) {
			return true
		}
	}

	return false
}

func (lf *LiquidityFilter) RankRoutes(
	routes []*ArbitrageRoute,
	g *graph.TokenGraph,
) []*ArbitrageRoute {
	type rankedRoute struct {
		route *ArbitrageRoute
		score float64
	}

	ranked := make([]rankedRoute, len(routes))

	for i, route := range routes {

		profitScore := 0.0
		if route.NetProfit != nil && route.NetProfit.Sign() > 0 {
			profitEth, _ := new(big.Float).Quo(
				new(big.Float).SetInt(route.NetProfit),
				big.NewFloat(1e18),
			).Float64()
			profitScore = min(1.0, profitEth/1.0)
		}

		liquidityScore := lf.ScoreRouteByLiquidity(route, g)

		hopPenalty := float64(len(route.Hops)) * 0.05

		score := profitScore*0.5 + liquidityScore*0.4 - hopPenalty

		ranked[i] = rankedRoute{route, score}
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	result := make([]*ArbitrageRoute, len(ranked))
	for i, r := range ranked {
		result[i] = r.route
	}

	return result
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
