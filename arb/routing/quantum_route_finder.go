package routing

import (
	"crypto/sha256"
	"math/big"
	"time"

	"zeam/arb/graph"
	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
)

type QuantumRouteFinder struct {

	qs *quantum.QuantumService

	WalkSteps    int
	TargetProfit float64

	TotalSearches int
	TotalFound    int
	LastSearchMS  int64
}

func NewQuantumRouteFinder() *QuantumRouteFinder {
	return &QuantumRouteFinder{
		qs:           quantum.GetService(),
		WalkSteps:    10,
		TargetProfit: 0.005,
	}
}

func (qrf *QuantumRouteFinder) FindRoutes(
	g *graph.TokenGraph,
	startToken common.Address,
	chainID uint64,
	constraints *RouteConstraints,
) *RouteFinderResult {
	start := time.Now()
	qrf.TotalSearches++

	qGraph := qrf.buildQuantumGraph(g, chainID)

	startKey := tokenKey(startToken, chainID)
	walkResult := qrf.qs.FindArbitragePath(startKey, qrf.TargetProfit)

	result := &RouteFinderResult{
		Routes:     make([]*ArbitrageRoute, 0),
		Method:     "quantum_walk",
		SearchTime: time.Since(start),
	}

	if walkResult == nil || len(walkResult.Path) < 2 || walkResult.Probability <= 0 {
		return result
	}

	route := qrf.walkToRoute(walkResult, g, startToken, chainID, constraints)
	if route != nil && constraints.Validate(route) {
		result.Routes = append(result.Routes, route)
		result.ValidCount = 1
		result.BestProfit = route.NetProfit
		qrf.TotalFound++
	}

	result.TotalSearched = len(qGraph.Nodes)
	qrf.LastSearchMS = time.Since(start).Milliseconds()

	return result
}

func (qrf *QuantumRouteFinder) FindTriangular(
	g *graph.TokenGraph,
	chainID uint64,
	constraints *RouteConstraints,
) *RouteFinderResult {
	start := time.Now()
	qrf.TotalSearches++

	result := &RouteFinderResult{
		Routes:     make([]*ArbitrageRoute, 0),
		Method:     "quantum_triangular",
		SearchTime: time.Since(start),
	}

	tokens := g.GetTokensOnChain(chainID)
	if len(tokens) < 3 {
		return result
	}

	qGraph := qrf.buildQuantumGraph(g, chainID)

	for _, token := range tokens {
		if len(result.Routes) >= 10 {
			break
		}

		startKey := tokenKey(token, chainID)
		walkResult := qrf.qs.WalkGraph(startKey, 3, startKey)

		if walkResult != nil && len(walkResult.Path) >= 3 && walkResult.Probability > 0 {
			route := qrf.walkToRoute(walkResult, g, token, chainID, constraints)
			if route != nil && constraints.Validate(route) {
				result.Routes = append(result.Routes, route)
				if result.BestProfit == nil || route.NetProfit.Cmp(result.BestProfit) > 0 {
					result.BestProfit = route.NetProfit
				}
			}
		}
	}

	result.TotalSearched = len(qGraph.Nodes)
	result.ValidCount = len(result.Routes)
	result.SearchTime = time.Since(start)

	return result
}

func (qrf *QuantumRouteFinder) FindCrossChain(
	g *graph.TokenGraph,
	chainIDs []uint64,
	constraints *RouteConstraints,
) *RouteFinderResult {
	start := time.Now()

	result := &RouteFinderResult{
		Routes:     make([]*ArbitrageRoute, 0),
		Method:     "quantum_crosschain",
		SearchTime: time.Since(start),
	}

	qGraph := quantum.NewQuantumGraph()
	for _, chainID := range chainIDs {
		qrf.addChainToGraph(qGraph, g, chainID)
	}

	qrf.addBridgeEdges(qGraph, chainIDs)

	walkTime := float64(len(chainIDs)) * 2.0

	tokens := g.GetTokensOnChain(chainIDs[0])
	limit := 5
	if len(tokens) < limit {
		limit = len(tokens)
	}
	for _, token := range tokens[:limit] {
		startKey := tokenKey(token, chainIDs[0])
		walkResult := qrf.qs.WalkContinuous(startKey, walkTime, "")

		if walkResult != nil && walkResult.Probability > qrf.TargetProfit {

			route := &ArbitrageRoute{
				ID:              hashPath(walkResult.Path),
				StartToken:      token,
				StartChain:      chainIDs[0],
				IsCrossChain:    true,
				ChainIDs:        chainIDs,
				DetectedAt:      time.Now(),
				ExpiresAt:       time.Now().Add(30 * time.Second),
				DetectionMethod: "quantum_crosschain",
			}
			result.Routes = append(result.Routes, route)
		}
	}

	result.TotalSearched = len(qGraph.Nodes)
	result.ValidCount = len(result.Routes)
	result.SearchTime = time.Since(start)

	return result
}

func (qrf *QuantumRouteFinder) buildQuantumGraph(g *graph.TokenGraph, chainID uint64) *quantum.QuantumGraph {
	qGraph := quantum.NewQuantumGraph()

	tokens := g.GetTokensOnChain(chainID)
	for _, token := range tokens {
		key := tokenKey(token, chainID)
		qGraph.AddNode(key, token)
	}

	pools := g.GetPoolsOnChain(chainID)
	for _, pool := range pools {
		token0Key := tokenKey(pool.Token0, chainID)
		token1Key := tokenKey(pool.Token1, chainID)

		weight0to1 := qrf.calculateEdgeWeight(pool, true)
		weight1to0 := qrf.calculateEdgeWeight(pool, false)

		qrf.qs.AddPool(token0Key, token1Key, weight0to1)
		qrf.qs.AddPool(token1Key, token0Key, weight1to0)
	}

	return qGraph
}

func (qrf *QuantumRouteFinder) addChainToGraph(qGraph *quantum.QuantumGraph, g *graph.TokenGraph, chainID uint64) {
	tokens := g.GetTokensOnChain(chainID)
	for _, token := range tokens {
		key := tokenKey(token, chainID)
		qGraph.AddNode(key, map[string]interface{}{
			"token":   token,
			"chainID": chainID,
		})
	}

	pools := g.GetPoolsOnChain(chainID)
	for _, pool := range pools {
		token0Key := tokenKey(pool.Token0, chainID)
		token1Key := tokenKey(pool.Token1, chainID)
		weight := qrf.calculateEdgeWeight(pool, true)
		qGraph.AddUndirectedEdge(token0Key, token1Key, weight)
	}
}

func (qrf *QuantumRouteFinder) addBridgeEdges(qGraph *quantum.QuantumGraph, chainIDs []uint64) {

	bridgeableTokens := []common.Address{
		common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
		common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
		common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
	}

	for _, token := range bridgeableTokens {
		for i := 0; i < len(chainIDs); i++ {
			for j := i + 1; j < len(chainIDs); j++ {
				key1 := tokenKey(token, chainIDs[i])
				key2 := tokenKey(token, chainIDs[j])

				qGraph.AddUndirectedEdge(key1, key2, 0.998)
			}
		}
	}
}

func (qrf *QuantumRouteFinder) calculateEdgeWeight(pool *graph.PoolInfo, zeroToOne bool) float64 {
	if pool.Reserve0 == nil || pool.Reserve1 == nil {
		return 1.0
	}

	r0 := new(big.Float).SetInt(pool.Reserve0)
	r1 := new(big.Float).SetInt(pool.Reserve1)

	var rate float64
	if zeroToOne {

		rateFloat, _ := new(big.Float).Quo(r1, r0).Float64()
		rate = rateFloat
	} else {

		rateFloat, _ := new(big.Float).Quo(r0, r1).Float64()
		rate = rateFloat
	}

	fee := 0.003
	if pool.Fee > 0 {
		fee = float64(pool.Fee) / 1000000.0
	}

	return rate * (1 - fee)
}

func (qrf *QuantumRouteFinder) walkToRoute(
	walk *quantum.WalkResult,
	g *graph.TokenGraph,
	startToken common.Address,
	chainID uint64,
	constraints *RouteConstraints,
) *ArbitrageRoute {
	if walk == nil || len(walk.Path) < 2 {
		return nil
	}

	route := &ArbitrageRoute{
		ID:              hashPath(walk.Path),
		StartToken:      startToken,
		StartChain:      chainID,
		StartAmount:     constraints.MinTradeSize,
		DetectedAt:      time.Now(),
		ExpiresAt:       time.Now().Add(time.Duration(constraints.MaxHops*10) * time.Second),
		DetectionMethod: "quantum_walk",
		ChainIDs:        []uint64{chainID},
	}

	if route.StartAmount == nil {
		route.StartAmount = big.NewInt(1e18)
	}

	hops := make([]RouteHop, 0, len(walk.Path)-1)
	currentAmount := new(big.Int).Set(route.StartAmount)

	for i := 0; i < len(walk.Path)-1; i++ {
		fromKey := walk.Path[i]
		toKey := walk.Path[i+1]

		tokenIn := parseTokenFromKey(fromKey)
		tokenOut := parseTokenFromKey(toKey)

		pool := g.FindPool(tokenIn, tokenOut, chainID)
		if pool == nil {
			continue
		}

		amountOut := qrf.calculateAmountOut(pool, tokenIn, currentAmount)

		hop := RouteHop{
			ChainID:   chainID,
			Pool:      pool.Address,
			DEX:       pool.DEX,
			TokenIn:   tokenIn,
			TokenOut:  tokenOut,
			AmountIn:  new(big.Int).Set(currentAmount),
			AmountOut: amountOut,
		}

		hops = append(hops, hop)
		currentAmount = amountOut
	}

	if len(hops) == 0 {
		return nil
	}

	route.Hops = hops
	route.EndAmount = currentAmount

	route.GrossProfit = new(big.Int).Sub(route.EndAmount, route.StartAmount)

	gasPerHop := big.NewInt(150000)
	route.GasEstimate = new(big.Int).Mul(gasPerHop, big.NewInt(int64(len(hops))))

	gasCost := new(big.Int).Mul(route.GasEstimate, big.NewInt(50e9))
	route.NetProfit = new(big.Int).Sub(route.GrossProfit, gasCost)

	if route.StartAmount.Sign() > 0 {
		profitFloat := new(big.Float).SetInt(route.NetProfit)
		startFloat := new(big.Float).SetInt(route.StartAmount)
		ratio, _ := new(big.Float).Quo(profitFloat, startFloat).Float64()
		route.ProfitBPS = int64(ratio * 10000)
	}

	return route
}

func (qrf *QuantumRouteFinder) calculateAmountOut(pool *graph.PoolInfo, tokenIn common.Address, amountIn *big.Int) *big.Int {
	if pool.Reserve0 == nil || pool.Reserve1 == nil || amountIn == nil {
		return big.NewInt(0)
	}

	var reserveIn, reserveOut *big.Int
	if tokenIn == pool.Token0 {
		reserveIn = pool.Reserve0
		reserveOut = pool.Reserve1
	} else {
		reserveIn = pool.Reserve1
		reserveOut = pool.Reserve0
	}

	amountInWithFee := new(big.Int).Mul(amountIn, big.NewInt(997))
	numerator := new(big.Int).Mul(amountInWithFee, reserveOut)
	denominator := new(big.Int).Add(
		new(big.Int).Mul(reserveIn, big.NewInt(1000)),
		amountInWithFee,
	)

	if denominator.Sign() == 0 {
		return big.NewInt(0)
	}

	return new(big.Int).Div(numerator, denominator)
}

func tokenKey(token common.Address, chainID uint64) string {
	return token.Hex() + "_" + string(rune(chainID))
}

func parseTokenFromKey(key string) common.Address {
	if len(key) < 42 {
		return common.Address{}
	}
	return common.HexToAddress(key[:42])
}

func hashPath(path []string) [32]byte {
	data := ""
	for _, p := range path {
		data += p
	}
	return sha256.Sum256([]byte(data))
}
