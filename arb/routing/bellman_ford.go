package routing

import (
	"fmt"
	"math"
	"math/big"
	"time"

	"zeam/arb/graph"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type BellmanFordFinder struct {
	config *BellmanFordConfig
}

type BellmanFordConfig struct {

	MaxIterations int

	EnableEarlyTermination bool

	MinImprovement float64

	MaxRoutes int

	RouteValidityDuration time.Duration
}

func DefaultBellmanFordConfig() *BellmanFordConfig {
	return &BellmanFordConfig{
		MaxIterations:          20,
		EnableEarlyTermination: true,
		MinImprovement:         1e-9,
		MaxRoutes:              100,
		RouteValidityDuration:  10 * time.Second,
	}
}

func NewBellmanFordFinder(config *BellmanFordConfig) *BellmanFordFinder {
	if config == nil {
		config = DefaultBellmanFordConfig()
	}
	return &BellmanFordFinder{config: config}
}

func (bf *BellmanFordFinder) FindRoutes(
	g *graph.TokenGraph,
	startToken common.Address,
	chainID uint64,
	constraints *RouteConstraints,
) *RouteFinderResult {
	start := time.Now()

	result := &RouteFinderResult{
		Routes: make([]*ArbitrageRoute, 0),
		Method: "bellman_ford",
	}

	nodes := g.GetAllNodes(chainID)
	if len(nodes) == 0 {
		result.SearchTime = time.Since(start)
		return result
	}

	dist := make(map[common.Address]float64)
	pred := make(map[common.Address]*predecessorInfo)

	for _, node := range nodes {
		dist[node.Address] = math.Inf(1)
	}
	dist[startToken] = 0

	iterations := bf.config.MaxIterations
	if iterations > len(nodes)-1 {
		iterations = len(nodes) - 1
	}

	improved := true
	for i := 0; i < iterations && improved; i++ {
		improved = false

		for _, node := range nodes {
			if math.IsInf(dist[node.Address], 1) {
				continue
			}

			edges := g.GetNeighbors(chainID, node.Address)
			for _, edge := range edges {
				neighbor := edge.GetTokenOut(node.Address)
				weight := edge.GetWeight(node.Address)
				newDist := dist[node.Address] + weight

				if newDist < dist[neighbor]-bf.config.MinImprovement {
					dist[neighbor] = newDist
					pred[neighbor] = &predecessorInfo{
						from: node.Address,
						edge: edge,
					}
					improved = true
				}
			}
		}

		result.TotalSearched++
	}

	cycles := bf.detectNegativeCycles(g, chainID, nodes, dist, pred, startToken)

	for _, cycle := range cycles {
		route := bf.cycleToRoute(cycle, chainID, constraints)
		if route != nil && constraints.Validate(route) {
			result.Routes = append(result.Routes, route)
			result.ValidCount++

			if result.BestProfit == nil || route.NetProfit.Cmp(result.BestProfit) > 0 {
				result.BestProfit = route.NetProfit
			}

			if len(result.Routes) >= bf.config.MaxRoutes {
				break
			}
		}
	}

	result.SearchTime = time.Since(start)
	return result
}

type predecessorInfo struct {
	from common.Address
	edge *graph.PoolEdge
}

type cycleInfo struct {
	nodes  []common.Address
	edges  []*graph.PoolEdge
	weight float64
}

func (bf *BellmanFordFinder) detectNegativeCycles(
	g *graph.TokenGraph,
	chainID uint64,
	nodes []*graph.TokenNode,
	dist map[common.Address]float64,
	pred map[common.Address]*predecessorInfo,
	startToken common.Address,
) []*cycleInfo {
	var cycles []*cycleInfo
	visited := make(map[common.Address]bool)

	for _, node := range nodes {
		if math.IsInf(dist[node.Address], 1) {
			continue
		}

		edges := g.GetNeighbors(chainID, node.Address)
		for _, edge := range edges {
			neighbor := edge.GetTokenOut(node.Address)
			weight := edge.GetWeight(node.Address)
			newDist := dist[node.Address] + weight

			if newDist < dist[neighbor]-bf.config.MinImprovement {
				if visited[neighbor] {
					continue
				}

				cycle := bf.traceCycle(g, chainID, neighbor, pred, edge)
				if cycle != nil && cycle.weight < 0 {
					cycles = append(cycles, cycle)
					visited[neighbor] = true
				}
			}
		}
	}

	return cycles
}

func (bf *BellmanFordFinder) traceCycle(
	g *graph.TokenGraph,
	chainID uint64,
	start common.Address,
	pred map[common.Address]*predecessorInfo,
	lastEdge *graph.PoolEdge,
) *cycleInfo {
	var nodes []common.Address
	var edges []*graph.PoolEdge
	var totalWeight float64

	nodes = append(nodes, lastEdge.GetTokenOut(start))
	edges = append(edges, lastEdge)
	totalWeight += lastEdge.GetWeight(start)

	current := start
	seen := make(map[common.Address]int)
	seen[current] = 0

	for i := 0; i < bf.config.MaxIterations; i++ {
		info := pred[current]
		if info == nil {
			break
		}

		nodes = append(nodes, current)
		edges = append(edges, info.edge)
		totalWeight += info.edge.GetWeight(info.from)

		if idx, found := seen[info.from]; found {

			nodes = nodes[idx:]
			edges = edges[idx:]

			totalWeight = 0
			for j := 0; j < len(edges); j++ {
				totalWeight += edges[j].GetWeight(nodes[j])
			}

			break
		}

		seen[info.from] = len(nodes)
		current = info.from
	}

	if len(nodes) < 2 {
		return nil
	}

	return &cycleInfo{
		nodes:  nodes,
		edges:  edges,
		weight: totalWeight,
	}
}

func (bf *BellmanFordFinder) cycleToRoute(
	cycle *cycleInfo,
	chainID uint64,
	constraints *RouteConstraints,
) *ArbitrageRoute {
	if len(cycle.nodes) < 2 || len(cycle.edges) < 2 {
		return nil
	}

	nodes := make([]common.Address, len(cycle.nodes))
	edges := make([]*graph.PoolEdge, len(cycle.edges))
	for i := 0; i < len(cycle.nodes); i++ {
		nodes[i] = cycle.nodes[len(cycle.nodes)-1-i]
	}
	for i := 0; i < len(cycle.edges); i++ {
		edges[i] = cycle.edges[len(cycle.edges)-1-i]
	}

	route := &ArbitrageRoute{
		StartToken:    nodes[0],
		StartChain:    chainID,
		ChainIDs:      []uint64{chainID},
		TotalWeight:   cycle.weight,
		DetectedAt:    time.Now(),
		ExpiresAt:     time.Now().Add(bf.config.RouteValidityDuration),
		ValidityMS:    bf.config.RouteValidityDuration.Milliseconds(),
		DetectionMethod: "bellman_ford",
	}

	for i := 0; i < len(edges); i++ {
		tokenIn := nodes[i]
		edge := edges[i]

		hop := RouteHop{
			ChainID:  chainID,
			Pool:     edge.Pool,
			DEX:      edge.DEX,
			TokenIn:  tokenIn,
			TokenOut: edge.GetTokenOut(tokenIn),
			Weight:   edge.GetWeight(tokenIn),
		}

		route.Hops = append(route.Hops, hop)
	}

	testAmount := big.NewInt(1e18)
	if constraints.MinTradeSize != nil {
		testAmount = constraints.MinTradeSize
	}

	route.StartAmount = testAmount
	route.EndAmount, route.TotalFees, _ = bf.simulateRoute(route, edges, nodes)

	if route.EndAmount != nil && route.EndAmount.Sign() > 0 {
		route.GrossProfit = new(big.Int).Sub(route.EndAmount, route.StartAmount)
		route.NetProfit = new(big.Int).Sub(route.GrossProfit, route.TotalFees)

		if route.StartAmount.Sign() > 0 {
			profitFloat := new(big.Float).SetInt(route.GrossProfit)
			startFloat := new(big.Float).SetInt(route.StartAmount)
			ratio := new(big.Float).Quo(profitFloat, startFloat)
			ratio.Mul(ratio, big.NewFloat(10000))
			bps, _ := ratio.Int64()
			route.ProfitBPS = bps
		}
	} else {
		route.GrossProfit = big.NewInt(0)
		route.NetProfit = big.NewInt(0)
	}

	route.GasEstimate = big.NewInt(int64(150000 * len(route.Hops)))

	route.ID = bf.generateRouteID(route)

	return route
}

func (bf *BellmanFordFinder) simulateRoute(
	route *ArbitrageRoute,
	edges []*graph.PoolEdge,
	nodes []common.Address,
) (*big.Int, *big.Int, bool) {
	currentAmount := new(big.Int).Set(route.StartAmount)
	totalFees := big.NewInt(0)

	for i := 0; i < len(edges); i++ {
		edge := edges[i]
		tokenIn := nodes[i]

		amountOut := graph.GetAmountOut(edge, tokenIn, currentAmount)
		if amountOut == nil || amountOut.Sign() == 0 {
			return nil, nil, false
		}

		feeAmount := new(big.Int).Mul(currentAmount, big.NewInt(int64(edge.Fee)))
		feeAmount.Div(feeAmount, big.NewInt(10000))
		totalFees.Add(totalFees, feeAmount)

		route.Hops[i].AmountIn = new(big.Int).Set(currentAmount)
		route.Hops[i].AmountOut = amountOut
		route.Hops[i].Fee = feeAmount
		route.Hops[i].PriceImpactBPS = graph.CalculatePriceImpact(edge, tokenIn, currentAmount)

		maxAmount := edge.GetMaxAmount(tokenIn)
		if maxAmount != nil {
			if route.MinViableLiquidity == nil || maxAmount.Cmp(route.MinViableLiquidity) < 0 {
				route.MinViableLiquidity = maxAmount
			}
		}

		currentAmount = amountOut
	}

	return currentAmount, totalFees, true
}

func (bf *BellmanFordFinder) generateRouteID(route *ArbitrageRoute) [32]byte {
	data := make([]byte, 0, 128)

	data = append(data, route.StartToken.Bytes()...)
	data = append(data, byte(route.StartChain))

	for _, hop := range route.Hops {
		data = append(data, hop.Pool.Bytes()...)
	}

	data = append(data, byte(route.DetectedAt.Unix()>>24))

	return crypto.Keccak256Hash(data)
}

func (bf *BellmanFordFinder) FindTriangular(
	g *graph.TokenGraph,
	chainID uint64,
	constraints *RouteConstraints,
) *RouteFinderResult {
	start := time.Now()

	result := &RouteFinderResult{
		Routes: make([]*ArbitrageRoute, 0),
		Method: "triangular",
	}

	bluechips := g.GetBluechipNodes(chainID)
	if len(bluechips) == 0 {
		bluechips = g.GetAllNodes(chainID)
	}

	for _, startNode := range bluechips {
		routes := bf.findTriangularFrom(g, chainID, startNode.Address, constraints)
		result.Routes = append(result.Routes, routes...)
		result.TotalSearched += len(routes)

		if len(result.Routes) >= bf.config.MaxRoutes {
			break
		}
	}

	for _, route := range result.Routes {
		if constraints.Validate(route) {
			result.ValidCount++
			if result.BestProfit == nil || route.NetProfit.Cmp(result.BestProfit) > 0 {
				result.BestProfit = route.NetProfit
			}
		}
	}

	result.SearchTime = time.Since(start)
	return result
}

func (bf *BellmanFordFinder) findTriangularFrom(
	g *graph.TokenGraph,
	chainID uint64,
	startToken common.Address,
	constraints *RouteConstraints,
) []*ArbitrageRoute {
	var routes []*ArbitrageRoute

	edgesA := g.GetNeighbors(chainID, startToken)

	for _, edgeA := range edgesA {
		tokenB := edgeA.GetTokenOut(startToken)
		if tokenB == startToken {
			continue
		}

		edgesB := g.GetNeighbors(chainID, tokenB)

		for _, edgeB := range edgesB {
			tokenC := edgeB.GetTokenOut(tokenB)
			if tokenC == startToken || tokenC == tokenB {
				continue
			}

			edgesC := g.GetEdgesBetween(chainID, tokenC, startToken)

			for _, edgeC := range edgesC {

				weight := edgeA.GetWeight(startToken) +
					edgeB.GetWeight(tokenB) +
					edgeC.GetWeight(tokenC)

				if weight >= 0 {
					continue
				}

				route := bf.buildTriangularRoute(
					chainID,
					startToken, tokenB, tokenC,
					edgeA, edgeB, edgeC,
					weight,
					constraints,
				)

				if route != nil && constraints.Validate(route) {
					routes = append(routes, route)
				}
			}
		}
	}

	return routes
}

func (bf *BellmanFordFinder) buildTriangularRoute(
	chainID uint64,
	tokenA, tokenB, tokenC common.Address,
	edgeAB, edgeBC, edgeCA *graph.PoolEdge,
	totalWeight float64,
	constraints *RouteConstraints,
) *ArbitrageRoute {
	route := &ArbitrageRoute{
		StartToken:    tokenA,
		StartChain:    chainID,
		ChainIDs:      []uint64{chainID},
		TotalWeight:   totalWeight,
		DetectedAt:    time.Now(),
		ExpiresAt:     time.Now().Add(bf.config.RouteValidityDuration),
		ValidityMS:    bf.config.RouteValidityDuration.Milliseconds(),
		DetectionMethod: "triangular",
		Hops: []RouteHop{
			{ChainID: chainID, Pool: edgeAB.Pool, DEX: edgeAB.DEX, TokenIn: tokenA, TokenOut: tokenB, Weight: edgeAB.GetWeight(tokenA)},
			{ChainID: chainID, Pool: edgeBC.Pool, DEX: edgeBC.DEX, TokenIn: tokenB, TokenOut: tokenC, Weight: edgeBC.GetWeight(tokenB)},
			{ChainID: chainID, Pool: edgeCA.Pool, DEX: edgeCA.DEX, TokenIn: tokenC, TokenOut: tokenA, Weight: edgeCA.GetWeight(tokenC)},
		},
	}

	testAmount := big.NewInt(1e18)
	if constraints.MinTradeSize != nil {
		testAmount = constraints.MinTradeSize
	}
	route.StartAmount = testAmount

	edges := []*graph.PoolEdge{edgeAB, edgeBC, edgeCA}
	nodes := []common.Address{tokenA, tokenB, tokenC}

	route.EndAmount, route.TotalFees, _ = bf.simulateRoute(route, edges, nodes)

	if route.EndAmount != nil && route.EndAmount.Sign() > 0 {
		route.GrossProfit = new(big.Int).Sub(route.EndAmount, route.StartAmount)
		route.NetProfit = new(big.Int).Sub(route.GrossProfit, route.TotalFees)

		if route.StartAmount.Sign() > 0 {
			profitFloat := new(big.Float).SetInt(route.GrossProfit)
			startFloat := new(big.Float).SetInt(route.StartAmount)
			ratio := new(big.Float).Quo(profitFloat, startFloat)
			ratio.Mul(ratio, big.NewFloat(10000))
			bps, _ := ratio.Int64()
			route.ProfitBPS = bps
		}
	} else {
		route.GrossProfit = big.NewInt(0)
		route.NetProfit = big.NewInt(0)
	}

	route.GasEstimate = big.NewInt(450000)
	route.ID = bf.generateRouteID(route)

	return route
}

func PrintRouteDetails(route *ArbitrageRoute) {
	fmt.Printf("Route %s:\n", common.Bytes2Hex(route.ID[:8]))
	fmt.Printf("  Hops: %d, Chain: %d\n", len(route.Hops), route.StartChain)
	fmt.Printf("  Weight: %.6f (negative = profitable)\n", route.TotalWeight)
	fmt.Printf("  Start: %s\n", route.StartToken.Hex()[:10])

	for i, hop := range route.Hops {
		fmt.Printf("  [%d] %s -> %s via %s (%s)\n",
			i+1,
			hop.TokenIn.Hex()[:10],
			hop.TokenOut.Hex()[:10],
			hop.Pool.Hex()[:10],
			hop.DEX)
	}

	if route.NetProfit != nil {
		profitEth := new(big.Float).Quo(
			new(big.Float).SetInt(route.NetProfit),
			big.NewFloat(1e18),
		)
		fmt.Printf("  Profit: %s wei (%.6f ETH, %d BPS)\n",
			route.NetProfit.String(), profitEth, route.ProfitBPS)
	}
}
