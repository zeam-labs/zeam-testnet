package routing

import (
	"fmt"
	"math"
	"math/big"
	"sync"
	"time"

	"zeam/arb/graph"
	"zeam/node"
	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
)

type NeuralRouteFinder struct {
	mu sync.RWMutex

	bellmanFord *BellmanFordFinder

	flowCompute *node.FlowCompute
	pressure    *quantum.PressureTracker

	config *NeuralConfig

	OnRouteFound     func(*ArbitrageRoute)
	OnPressureSpike  func(quantum.BlockLocation, float64)

	routesExplored   uint64
	routesFound      uint64
	pressureCollapses uint64
	lastExploration  time.Time
}

type NeuralConfig struct {

	ExecutionPressure float64

	PressureDecayInterval time.Duration
	PressureHalfLife      time.Duration

	ExplorationInterval time.Duration
	MaxRoutesPerRound   int

	MinProfitBPS int64
}

func DefaultNeuralConfig() *NeuralConfig {
	return &NeuralConfig{
		ExecutionPressure:     0.8,
		PressureDecayInterval: time.Second,
		PressureHalfLife:      10 * time.Second,
		ExplorationInterval:   5 * time.Second,
		MaxRoutesPerRound:     10,
		MinProfitBPS:          10,
	}
}

func NewNeuralRouteFinder(
	flowCompute *node.FlowCompute,
	config *NeuralConfig,
) *NeuralRouteFinder {
	if config == nil {
		config = DefaultNeuralConfig()
	}

	return &NeuralRouteFinder{
		bellmanFord: NewBellmanFordFinder(DefaultBellmanFordConfig()),
		flowCompute: flowCompute,
		pressure:    quantum.NewPressureTracker(),
		config:      config,
	}
}

func (nrf *NeuralRouteFinder) Start(g *graph.TokenGraph, stopCh <-chan struct{}) {

	go nrf.pressure.StartDecayLoop(
		nrf.config.PressureDecayInterval,
		nrf.config.PressureHalfLife,
		stopCh,
	)

	go nrf.explorationLoop(g, stopCh)
}

func (nrf *NeuralRouteFinder) explorationLoop(g *graph.TokenGraph, stopCh <-chan struct{}) {
	ticker := time.NewTicker(nrf.config.ExplorationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			nrf.exploreOnce(g)
		}
	}
}

func (nrf *NeuralRouteFinder) exploreOnce(g *graph.TokenGraph) {
	nrf.mu.Lock()
	nrf.routesExplored++
	nrf.lastExploration = time.Now()
	nrf.mu.Unlock()

	entropy := nrf.flowCompute.NextBytes(32)
	seed := new(big.Int).SetBytes(entropy)

	chains := g.GetChainIDs()
	if len(chains) == 0 {
		return
	}

	chainIdx := int(seed.Uint64() % uint64(len(chains)))
	chainID := chains[chainIdx]

	tokens := g.GetAllNodes(chainID)
	if len(tokens) == 0 {
		return
	}

	tokenIdx := int(nrf.flowCompute.NextUint64() % uint64(len(tokens)))
	startToken := tokens[tokenIdx].Address

	constraints := DefaultRouteConstraints()
	result := nrf.bellmanFord.FindRoutes(g, startToken, chainID, constraints)

	for _, route := range result.Routes {
		nrf.processRoute(route)
	}

	triangular := nrf.bellmanFord.FindTriangular(g, chainID, constraints)
	for _, route := range triangular.Routes {
		nrf.processRoute(route)
	}
}

func (nrf *NeuralRouteFinder) processRoute(route *ArbitrageRoute) {
	if route.ProfitBPS < nrf.config.MinProfitBPS {
		return
	}

	nrf.mu.Lock()
	nrf.routesFound++
	nrf.mu.Unlock()

	pressure := nrf.calculatePressure(route)

	loc := quantum.BlockLocation{
		ChainID: fmt.Sprintf("%d", route.StartChain),
	}
	nrf.pressure.AddPressure(loc, pressure)

	if nrf.OnRouteFound != nil {
		nrf.OnRouteFound(route)
	}

	totalPressure := nrf.pressure.GetPressure(loc)
	if totalPressure >= nrf.config.ExecutionPressure {
		nrf.mu.Lock()
		nrf.pressureCollapses++
		nrf.mu.Unlock()

		if nrf.OnPressureSpike != nil {
			nrf.OnPressureSpike(loc, totalPressure)
		}
	}
}

func (nrf *NeuralRouteFinder) calculatePressure(route *ArbitrageRoute) float64 {

	magnitude := math.Min(float64(route.ProfitBPS)/1000.0, 1.0)

	coherence := 1.0 - float64(len(route.Hops))/10.0
	if coherence < 0.1 {
		coherence = 0.1
	}

	tension := 0.8

	density := 0.5
	if route.MinViableLiquidity != nil {

		liqFloat := new(big.Float).SetInt(route.MinViableLiquidity)
		liqFloat.Quo(liqFloat, big.NewFloat(1e18))
		liqVal, _ := liqFloat.Float64()
		density = math.Min(liqVal/1000.0, 1.0)
	}

	pressure := 0.4*magnitude + 0.2*coherence + 0.2*tension + 0.2*density

	return pressure
}

func (nrf *NeuralRouteFinder) SelectRoute(candidates []*ArbitrageRoute) *ArbitrageRoute {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	idx := nrf.flowCompute.SelectIndex(len(candidates))
	return candidates[idx]
}

func (nrf *NeuralRouteFinder) SelectWeightedByProfit(candidates []*ArbitrageRoute) *ArbitrageRoute {
	if len(candidates) == 0 {
		return nil
	}

	weights := make([]float64, len(candidates))
	for i, route := range candidates {
		if route.ProfitBPS > 0 {
			weights[i] = float64(route.ProfitBPS)
		} else {
			weights[i] = 0.1
		}
	}

	target := nrf.flowCompute.NextFloat() * sumFloat(weights)
	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if target <= cumulative {
			return candidates[i]
		}
	}

	return candidates[len(candidates)-1]
}

func sumFloat(vals []float64) float64 {
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum
}

func (nrf *NeuralRouteFinder) GetPressure(loc quantum.BlockLocation) float64 {
	return nrf.pressure.GetPressure(loc)
}

func (nrf *NeuralRouteFinder) GetTotalPressure() float64 {
	return nrf.pressure.GetTotalPressure()
}

func (nrf *NeuralRouteFinder) GetHotspots(threshold float64) []quantum.PressurePoint {
	return nrf.pressure.GetHotspots(threshold)
}

func (nrf *NeuralRouteFinder) Stats() map[string]interface{} {
	nrf.mu.RLock()
	defer nrf.mu.RUnlock()

	return map[string]interface{}{
		"routes_explored":    nrf.routesExplored,
		"routes_found":       nrf.routesFound,
		"pressure_collapses": nrf.pressureCollapses,
		"total_pressure":     nrf.pressure.GetTotalPressure(),
		"pressure_events":    nrf.pressure.GetEventCount(),
		"last_exploration":   nrf.lastExploration,
	}
}

func (nrf *NeuralRouteFinder) ExploreFromToken(
	g *graph.TokenGraph,
	startToken common.Address,
	chainID uint64,
) []*ArbitrageRoute {
	constraints := DefaultRouteConstraints()

	result := nrf.bellmanFord.FindRoutes(g, startToken, chainID, constraints)

	var validRoutes []*ArbitrageRoute
	for _, route := range result.Routes {
		nrf.processRoute(route)
		if route.ProfitBPS >= nrf.config.MinProfitBPS {
			validRoutes = append(validRoutes, route)
		}
	}

	return validRoutes
}

func (nrf *NeuralRouteFinder) ExploreAllChains(g *graph.TokenGraph) []*ArbitrageRoute {
	var allRoutes []*ArbitrageRoute

	for _, chainID := range g.GetChainIDs() {

		tokens := g.GetAllNodes(chainID)
		if len(tokens) == 0 {
			continue
		}

		numStarts := 3
		if numStarts > len(tokens) {
			numStarts = len(tokens)
		}

		for i := 0; i < numStarts; i++ {
			idx := nrf.flowCompute.SelectIndex(len(tokens))
			routes := nrf.ExploreFromToken(g, tokens[idx].Address, chainID)
			allRoutes = append(allRoutes, routes...)
		}
	}

	return allRoutes
}
