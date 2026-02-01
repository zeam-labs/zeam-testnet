package arb

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sort"
	"sync"
	"time"

	"zeam/arb/discovery"
	"zeam/arb/graph"
	"zeam/arb/routing"
	"zeam/synthesis"
	"zeam/node"
	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type SwapPriceUpdate struct {
	ChainID   uint64
	Pool      common.Address
	TokenIn   common.Address
	TokenOut  common.Address
	AmountIn  *big.Int
	AmountOut *big.Int
	Timestamp time.Time
}

type MultiHopDetector struct {
	mu sync.RWMutex

	registry *discovery.PoolRegistry

	tokenGraph *graph.TokenGraph

	routeFinder *routing.BellmanFordFinder

	neuralFinder *routing.NeuralRouteFinder

	flowCompute *node.FlowCompute
	pressure    *quantum.PressureTracker
	ngacBridge  *synthesis.NGACBridge

	liquidityFilter *routing.LiquidityFilter

	routeCache *routing.RouteCache

	calldataParser *discovery.CalldataParser

	config *MultiHopConfig

	stats MultiHopStats

	OnRouteDiscovered func(route *routing.ArbitrageRoute)
	OnSwapDetected    func(update *SwapPriceUpdate)
	OnPressureSpike   func(loc quantum.BlockLocation, pressure float64)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type MultiHopConfig struct {

	MaxHops           int
	MinProfitBPS      int64
	MinProfitWei      *big.Int
	MaxPriceImpactBPS int64

	GraphRebuildInterval time.Duration
	RouteScanInterval    time.Duration

	MinPoolLiquidityUSD float64
	RequireBluechipHub  bool

	RouteTTL time.Duration

	EnabledChains []uint64
}

func DefaultMultiHopConfig() *MultiHopConfig {
	return &MultiHopConfig{
		MaxHops:              4,
		MinProfitBPS:         10,
		MinProfitWei:         big.NewInt(1e15),
		MaxPriceImpactBPS:    100,
		GraphRebuildInterval: 5 * time.Minute,
		RouteScanInterval:    30 * time.Second,
		MinPoolLiquidityUSD:  10000,
		RequireBluechipHub:   false,
		RouteTTL:             10 * time.Second,
		EnabledChains:        []uint64{1, 8453, 10},
	}
}

type MultiHopStats struct {
	PoolsDiscovered  int
	GraphNodes       int
	GraphEdges       int
	RoutesFound      int
	RoutesFiltered   int
	BestProfitBPS    int64
	LastGraphRebuild time.Time
	LastRouteScan    time.Time
	ScanDuration     time.Duration
	TxsProcessed     uint64
}

func NewMultiHopDetector(
	config *MultiHopConfig,
	flowCompute *node.FlowCompute,
	ngacBridge *synthesis.NGACBridge,
) *MultiHopDetector {
	if config == nil {
		config = DefaultMultiHopConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	registry := discovery.NewPoolRegistry(nil)

	graphConfig := &graph.GraphConfig{
		EnabledChains:   config.EnabledChains,
		MinPoolScore:    0.01,
		MinLiquidityUSD: config.MinPoolLiquidityUSD,
		RebuildInterval: config.GraphRebuildInterval,
	}
	tokenGraph := graph.NewTokenGraph(registry, graphConfig)

	bfConfig := &routing.BellmanFordConfig{
		MaxIterations:          config.MaxHops + 1,
		EnableEarlyTermination: true,
		MaxRoutes:              100,
		RouteValidityDuration:  config.RouteTTL,
	}
	routeFinder := routing.NewBellmanFordFinder(bfConfig)

	lfConfig := &routing.LiquidityConfig{
		MinPoolLiquidityUSD: config.MinPoolLiquidityUSD,
		MaxHopImpactBPS:     config.MaxPriceImpactBPS,
		MaxTotalImpactBPS:   config.MaxPriceImpactBPS * int64(config.MaxHops),
		RequireBluechipHub:  config.RequireBluechipHub,
	}
	liquidityFilter := routing.NewLiquidityFilter(lfConfig)

	calldataParser := discovery.NewCalldataParser()

	pressure := quantum.NewPressureTracker()

	var neuralFinder *routing.NeuralRouteFinder
	if flowCompute != nil {
		neuralConfig := routing.DefaultNeuralConfig()
		neuralConfig.MinProfitBPS = config.MinProfitBPS
		neuralFinder = routing.NewNeuralRouteFinder(flowCompute, neuralConfig)
	}

	mhd := &MultiHopDetector{
		registry:        registry,
		tokenGraph:      tokenGraph,
		routeFinder:     routeFinder,
		neuralFinder:    neuralFinder,
		flowCompute:     flowCompute,
		pressure:        pressure,
		ngacBridge:      ngacBridge,
		liquidityFilter: liquidityFilter,
		routeCache:      routing.NewRouteCache(),
		calldataParser:  calldataParser,
		config:          config,
		ctx:             ctx,
		cancel:          cancel,
	}

	calldataParser.OnSwapCall = func(chainID uint64, call *discovery.ParsedSwapCall, tx *types.Transaction) {
		mhd.handleSwapDetected(chainID, call, tx)
	}

	if neuralFinder != nil {
		neuralFinder.OnRouteFound = func(route *routing.ArbitrageRoute) {
			mhd.handleNeuralRouteFound(route)
		}
		neuralFinder.OnPressureSpike = func(loc quantum.BlockLocation, p float64) {
			mhd.handlePressureSpike(loc, p)
		}
	}

	return mhd
}

func (mhd *MultiHopDetector) Start() error {

	if err := mhd.registry.Start(); err != nil {
		return fmt.Errorf("failed to start pool registry: %w", err)
	}

	mhd.rebuildGraph()

	mhd.wg.Add(1)
	go mhd.graphRebuildLoop()

	mhd.wg.Add(1)
	go mhd.routeScanLoop()

	if mhd.neuralFinder != nil {

		stopCh := make(chan struct{})
		go func() {
			<-mhd.ctx.Done()
			close(stopCh)
		}()

		mhd.neuralFinder.Start(mhd.tokenGraph, stopCh)
		log.Println("[MultiHop] Neural route finder started (FlowCompute + PressureTracker)")
	}

	if mhd.pressure != nil {
		stopCh := make(chan struct{})
		go func() {
			<-mhd.ctx.Done()
			close(stopCh)
		}()
		mhd.pressure.StartDecayLoop(time.Second, 10*time.Second, stopCh)
		log.Println("[MultiHop] Pressure decay loop started")
	}

	log.Println("[MultiHop] Detector started (PARASITIC - no RPC)")
	if mhd.flowCompute != nil {
		log.Println("[MultiHop] NEURAL MODE: Using FlowCompute entropy for route selection")
	}
	if mhd.ngacBridge != nil {
		log.Println("[MultiHop] NGAC MODE: Pressure collapse drives execution")
	}
	return nil
}

func (mhd *MultiHopDetector) Stop() {
	mhd.cancel()
	mhd.registry.Stop()
	mhd.wg.Wait()
	log.Println("[MultiHop] Detector stopped")
}

func (mhd *MultiHopDetector) ProcessTransaction(chainID uint64, tx *types.Transaction) {
	mhd.mu.Lock()
	mhd.stats.TxsProcessed++
	count := mhd.stats.TxsProcessed
	mhd.mu.Unlock()

	if count%100 == 0 {
		fmt.Printf("[Arb] Processed %d txs (chain %d)\n", count, chainID)
	}

	mhd.registry.ProcessTransaction(chainID, tx)

	mhd.calldataParser.ProcessTransaction(chainID, tx)
}

func (mhd *MultiHopDetector) handleSwapDetected(chainID uint64, call *discovery.ParsedSwapCall, tx *types.Transaction) {
	if call.AmountIn == nil || call.AmountOut == nil {
		return
	}

	pools := mhd.registry.GetPoolsForPair(chainID, call.TokenIn, call.TokenOut)
	if len(pools) == 0 {
		return
	}

	pool := pools[0]
	if call.Pool != (common.Address{}) {

		if p := mhd.registry.GetPool(chainID, call.Pool); p != nil {
			pool = p
		}
	}

	swapInfo := &discovery.ParsedSwapCall{
		Pool:      pool.Address,
		TokenIn:   call.TokenIn,
		TokenOut:  call.TokenOut,
		AmountIn:  call.AmountIn,
		AmountOut: call.AmountOut,
	}
	mhd.registry.UpdatePoolFromSwap(chainID, pool.Address, swapInfo)

	mhd.tokenGraph.UpdateEdge(pool)

	priceFloat := float64(0)
	if call.AmountIn.Sign() > 0 {
		priceFloat, _ = new(big.Float).Quo(
			new(big.Float).SetInt(call.AmountOut),
			new(big.Float).SetInt(call.AmountIn),
		).Float64()
	}

	log.Printf("[MultiHop-LIVE] Swap on %s: %s->%s price=%.6f (chain %d)",
		pool.Address.Hex()[:10], call.TokenIn.Hex()[:10], call.TokenOut.Hex()[:10],
		priceFloat, chainID)

	mhd.mu.Lock()
	mhd.stats.PoolsDiscovered = mhd.registry.Stats().TotalPoolsDiscovered
	mhd.mu.Unlock()

	if mhd.OnSwapDetected != nil {
		update := &SwapPriceUpdate{
			ChainID:   chainID,
			Pool:      pool.Address,
			TokenIn:   call.TokenIn,
			TokenOut:  call.TokenOut,
			AmountIn:  call.AmountIn,
			AmountOut: call.AmountOut,
			Timestamp: time.Now(),
		}
		mhd.OnSwapDetected(update)
	}
}

func (mhd *MultiHopDetector) handleNeuralRouteFound(route *routing.ArbitrageRoute) {

	loc := quantum.BlockLocation{
		ChainID: fmt.Sprintf("%d", route.StartChain),
	}

	profitFloat := float64(route.ProfitBPS) / 10000.0
	mhd.pressure.AddPressure(loc, profitFloat)

	if mhd.ngacBridge != nil {

		profitETH := float64(0)
		if route.NetProfit != nil {
			profitETH, _ = new(big.Float).Quo(
				new(big.Float).SetInt(route.NetProfit),
				big.NewFloat(1e18),
			).Float64()
		}
		routeIDHex := fmt.Sprintf("%x", route.ID)
		mhd.ngacBridge.OnChainEvent("ARB_OPPORTUNITY", profitETH, routeIDHex)
	}

	if mhd.OnRouteDiscovered != nil {
		mhd.OnRouteDiscovered(route)
	}

	mhd.routeCache.Add(route)

	log.Printf("[Neural] Route found: %d hops, %d BPS, pressure=%.4f at chain %d",
		len(route.Hops), route.ProfitBPS, mhd.pressure.GetPressure(loc), route.StartChain)
}

func (mhd *MultiHopDetector) handlePressureSpike(loc quantum.BlockLocation, pressure float64) {
	log.Printf("[Neural] PRESSURE SPIKE: chain=%s pressure=%.4f - NGAC collapse triggered",
		loc.ChainID, pressure)

	if mhd.OnPressureSpike != nil {
		mhd.OnPressureSpike(loc, pressure)
	}

	allRoutes := mhd.routeCache.GetValid()
	var chainRoutes []*routing.ArbitrageRoute
	for _, r := range allRoutes {
		if fmt.Sprintf("%d", r.StartChain) == loc.ChainID {
			chainRoutes = append(chainRoutes, r)
		}
	}

	if len(chainRoutes) > 0 {
		best := chainRoutes[0]
		log.Printf("[Neural] Best route at pressure spike: %d BPS profit, %d hops",
			best.ProfitBPS, len(best.Hops))

		if mhd.ngacBridge != nil {
			routeIDHex := fmt.Sprintf("%x", best.ID)
			mhd.ngacBridge.OnChainEvent("PRESSURE_COLLAPSE", pressure, routeIDHex)
		}
	}
}

func (mhd *MultiHopDetector) graphRebuildLoop() {
	defer mhd.wg.Done()

	ticker := time.NewTicker(mhd.config.GraphRebuildInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mhd.ctx.Done():
			return
		case <-ticker.C:
			mhd.rebuildGraph()
		}
	}
}

func (mhd *MultiHopDetector) rebuildGraph() {
	start := time.Now()

	if err := mhd.tokenGraph.Build(); err != nil {
		log.Printf("[MultiHop] Failed to rebuild graph: %v", err)
		return
	}

	stats := mhd.tokenGraph.Stats()

	mhd.mu.Lock()
	mhd.stats.GraphNodes = stats.NodeCount
	mhd.stats.GraphEdges = stats.EdgeCount
	mhd.stats.LastGraphRebuild = time.Now()
	mhd.mu.Unlock()

	log.Printf("[MultiHop] Graph rebuilt: %d nodes, %d edges in %v",
		stats.NodeCount, stats.EdgeCount, time.Since(start))
}

func (mhd *MultiHopDetector) routeScanLoop() {
	defer mhd.wg.Done()

	time.Sleep(5 * time.Second)

	ticker := time.NewTicker(mhd.config.RouteScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mhd.ctx.Done():
			return
		case <-ticker.C:
			mhd.scanForRoutes()
		}
	}
}

func (mhd *MultiHopDetector) scanForRoutes() {
	start := time.Now()

	constraints := &routing.RouteConstraints{
		MaxHops:           mhd.config.MaxHops,
		MinHops:           2,
		MinProfitWei:      mhd.config.MinProfitWei,
		MinProfitBPS:      mhd.config.MinProfitBPS,
		MaxPriceImpactBPS: mhd.config.MaxPriceImpactBPS,
		AllowTriangular:   true,
		AllowCrossChain:   true,
	}

	var allRoutes []*routing.ArbitrageRoute
	var totalSearched, totalValid int

	for _, chainID := range mhd.config.EnabledChains {

		triangularResult := mhd.routeFinder.FindTriangular(mhd.tokenGraph, chainID, constraints)
		allRoutes = append(allRoutes, triangularResult.Routes...)
		totalSearched += triangularResult.TotalSearched
		totalValid += triangularResult.ValidCount

		bluechips := mhd.tokenGraph.GetBluechipNodes(chainID)
		for _, bluechip := range bluechips {
			result := mhd.routeFinder.FindRoutes(
				mhd.tokenGraph,
				bluechip.Address,
				chainID,
				constraints,
			)
			allRoutes = append(allRoutes, result.Routes...)
			totalSearched += result.TotalSearched
			totalValid += result.ValidCount
		}
	}

	filteredRoutes := mhd.liquidityFilter.FilterRoutes(allRoutes)

	if mhd.config.RequireBluechipHub {
		filteredRoutes = mhd.liquidityFilter.FilterByBluechipHub(filteredRoutes)
	}

	rankedRoutes := mhd.liquidityFilter.RankRoutes(filteredRoutes, mhd.tokenGraph)

	mhd.mu.Lock()
	mhd.stats.RoutesFound += len(allRoutes)
	mhd.stats.RoutesFiltered += len(allRoutes) - len(filteredRoutes)
	mhd.stats.LastRouteScan = time.Now()
	mhd.stats.ScanDuration = time.Since(start)

	for _, route := range rankedRoutes {
		if route.ProfitBPS > mhd.stats.BestProfitBPS {
			mhd.stats.BestProfitBPS = route.ProfitBPS
		}
	}
	mhd.mu.Unlock()

	for _, route := range rankedRoutes {
		mhd.routeCache.Add(route)

		if mhd.OnRouteDiscovered != nil {
			mhd.OnRouteDiscovered(route)
		}
	}

	if len(rankedRoutes) > 0 {
		log.Printf("[MultiHop] Scan complete: %d routes found, %d after filtering, best=%dbps (took %v)",
			len(allRoutes), len(rankedRoutes), mhd.stats.BestProfitBPS, time.Since(start))

		for i, route := range rankedRoutes {
			if i >= 3 {
				break
			}
			routing.PrintRouteDetails(route)
		}
	}
}

func (mhd *MultiHopDetector) GetTopRoutes(n int) []*routing.ArbitrageRoute {
	routes := mhd.routeCache.GetValid()

	if n > len(routes) {
		n = len(routes)
	}

	return routes[:n]
}

func (mhd *MultiHopDetector) GetRouteForToken(chainID uint64, token common.Address, amount *big.Int) []*routing.ArbitrageRoute {
	constraints := &routing.RouteConstraints{
		MaxHops:           mhd.config.MaxHops,
		MinHops:           2,
		MinProfitWei:      mhd.config.MinProfitWei,
		MinProfitBPS:      mhd.config.MinProfitBPS,
		MaxPriceImpactBPS: mhd.config.MaxPriceImpactBPS,
		MinTradeSize:      amount,
		AllowTriangular:   true,
	}

	result := mhd.routeFinder.FindRoutes(mhd.tokenGraph, token, chainID, constraints)

	filtered := mhd.liquidityFilter.FilterRoutes(result.Routes)
	return mhd.liquidityFilter.RankRoutes(filtered, mhd.tokenGraph)
}

func (mhd *MultiHopDetector) OptimizeRouteAmount(route *routing.ArbitrageRoute, minAmount, maxAmount *big.Int) (*big.Int, *big.Int) {
	return mhd.liquidityFilter.FindOptimalTradeSize(route, mhd.tokenGraph, minAmount, maxAmount)
}

func (mhd *MultiHopDetector) Stats() MultiHopStats {
	mhd.mu.RLock()
	defer mhd.mu.RUnlock()

	mhd.stats.PoolsDiscovered = mhd.registry.Stats().TotalPoolsDiscovered
	return mhd.stats
}

func (mhd *MultiHopDetector) GetPoolRegistry() *discovery.PoolRegistry {
	return mhd.registry
}

func (mhd *MultiHopDetector) NeuralStats() map[string]interface{} {
	stats := make(map[string]interface{})

	if mhd.neuralFinder != nil {
		stats["neural_finder"] = mhd.neuralFinder.Stats()
	}

	if mhd.pressure != nil {
		stats["total_pressure"] = mhd.pressure.GetTotalPressure()
		stats["pressure_events"] = mhd.pressure.GetEventCount()
		stats["hotspots"] = len(mhd.pressure.GetHotspots(0.5))
	}

	stats["flow_compute_enabled"] = mhd.flowCompute != nil
	stats["ngac_bridge_enabled"] = mhd.ngacBridge != nil

	return stats
}

func (mhd *MultiHopDetector) GetRecentRoutes(limit int) []*routing.ArbitrageRoute {
	routes := mhd.routeCache.GetValid()

	sort.Slice(routes, func(i, j int) bool {
		return routes[i].ProfitBPS > routes[j].ProfitBPS
	})

	if limit > 0 && len(routes) > limit {
		routes = routes[:limit]
	}

	return routes
}

func (mhd *MultiHopDetector) PrintStats() {
	stats := mhd.Stats()
	fmt.Printf("[MultiHop] Statistics (PARASITIC - no RPC):\n")
	fmt.Printf("  Pools discovered: %d\n", stats.PoolsDiscovered)
	fmt.Printf("  Txs processed: %d\n", stats.TxsProcessed)
	fmt.Printf("  Graph: %d nodes, %d edges\n", stats.GraphNodes, stats.GraphEdges)
	fmt.Printf("  Routes found: %d (filtered: %d)\n", stats.RoutesFound, stats.RoutesFiltered)
	fmt.Printf("  Best profit: %d BPS\n", stats.BestProfitBPS)
	fmt.Printf("  Cache size: %d\n", mhd.routeCache.Size())
	fmt.Printf("  Last scan: %v ago (took %v)\n",
		time.Since(stats.LastRouteScan).Round(time.Second),
		stats.ScanDuration.Round(time.Millisecond))

	if mhd.neuralFinder != nil || mhd.pressure != nil {
		fmt.Printf("[MultiHop] Neural Compute Statistics:\n")
		if mhd.neuralFinder != nil {
			nstats := mhd.neuralFinder.Stats()
			fmt.Printf("  Routes explored: %v\n", nstats["routes_explored"])
			fmt.Printf("  Routes found (neural): %v\n", nstats["routes_found"])
			fmt.Printf("  Pressure collapses: %v\n", nstats["pressure_collapses"])
		}
		if mhd.pressure != nil {
			fmt.Printf("  Total pressure: %.4f\n", mhd.pressure.GetTotalPressure())
			fmt.Printf("  Pressure events: %d\n", mhd.pressure.GetEventCount())
			hotspots := mhd.pressure.GetHotspots(0.5)
			fmt.Printf("  Active hotspots (>0.5): %d\n", len(hotspots))
		}
		fmt.Printf("  FlowCompute: %v\n", mhd.flowCompute != nil)
		fmt.Printf("  NGAC Bridge: %v\n", mhd.ngacBridge != nil)
	}
}

func ConvertToArbitrageOpportunity(route *routing.ArbitrageRoute) *ArbitrageOpportunity {
	opp := &ArbitrageOpportunity{
		Type:           OpTypeMultiHop,
		Status:         StatusDetected,
		SpreadBPS:      route.ProfitBPS,
		InputAmount:    route.StartAmount,
		ExpectedOutput: route.EndAmount,
		DetectedAt:     route.DetectedAt,
		ExpiresAt:      route.ExpiresAt,
	}

	if len(route.ChainIDs) > 0 {
		opp.BuyChain = route.ChainIDs[0]
		opp.SellChain = route.ChainIDs[len(route.ChainIDs)-1]
		opp.IsCrossChain = len(route.ChainIDs) > 1
	}

	if route.NetProfit != nil && route.StartAmount != nil && route.StartAmount.Sign() > 0 {
		profitFloat := new(big.Float).SetInt(route.NetProfit)
		startFloat := new(big.Float).SetInt(route.StartAmount)
		opp.ExpectedProfit = route.NetProfit

		pct := new(big.Float).Quo(profitFloat, startFloat)
		pctFloat, _ := pct.Float64()
		_ = pctFloat
	}

	if route.GasEstimate != nil {
		opp.GasEstimate = route.GasEstimate
	}

	opp.ComputeID()
	return opp
}

func (mhd *MultiHopDetector) IntegrateWithZEAMDetector(detector *ZEAMDetector) {
	mhd.OnRouteDiscovered = func(route *routing.ArbitrageRoute) {

		opp := ConvertToArbitrageOpportunity(route)

		scoredOpp := &ScoredOpportunity{
			ArbitrageOpportunity: opp,
			Priority:             int(route.ProfitBPS),
		}

		if detector.config.ShotgunEnabled {
			if !detector.applyShotgunFilter(scoredOpp) {
				return
			}
		}

		detector.mu.Lock()
		detector.opportunities[opp.ID] = scoredOpp
		detector.opportunitiesFound++
		detector.mu.Unlock()

		if detector.OnOpportunity != nil {
			detector.OnOpportunity(scoredOpp)
		}

		log.Printf("[MultiHop->ZEAM] Injected route: %d hops, %d BPS profit",
			len(route.Hops), route.ProfitBPS)
	}
}
