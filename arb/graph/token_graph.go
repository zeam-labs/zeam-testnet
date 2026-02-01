package graph

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"zeam/arb/discovery"

	"github.com/ethereum/go-ethereum/common"
)

type TokenGraph struct {
	mu sync.RWMutex

	Nodes map[uint64]map[common.Address]*TokenNode

	Edges map[uint64]map[common.Address][]*PoolEdge

	PoolEdges map[uint64]map[common.Address]*PoolEdge

	CrossChainEdges map[common.Address][]CrossChainEdge

	config *GraphConfig

	stats GraphStats

	registry *discovery.PoolRegistry
}

func NewTokenGraph(registry *discovery.PoolRegistry, config *GraphConfig) *TokenGraph {
	if config == nil {
		config = DefaultGraphConfig()
	}

	g := &TokenGraph{
		Nodes:           make(map[uint64]map[common.Address]*TokenNode),
		Edges:           make(map[uint64]map[common.Address][]*PoolEdge),
		PoolEdges:       make(map[uint64]map[common.Address]*PoolEdge),
		CrossChainEdges: make(map[common.Address][]CrossChainEdge),
		config:          config,
		registry:        registry,
		stats: GraphStats{
			NodesByChain: make(map[uint64]int),
			EdgesByChain: make(map[uint64]int),
			EdgesByDEX:   make(map[string]int),
		},
	}

	for _, edge := range config.CrossChainBridges {
		g.AddCrossChainEdge(edge)
	}

	return g
}

func (g *TokenGraph) Build() error {
	start := time.Now()

	g.mu.Lock()
	defer g.mu.Unlock()

	g.Nodes = make(map[uint64]map[common.Address]*TokenNode)
	g.Edges = make(map[uint64]map[common.Address][]*PoolEdge)
	g.PoolEdges = make(map[uint64]map[common.Address]*PoolEdge)
	g.stats = GraphStats{
		NodesByChain: make(map[uint64]int),
		EdgesByChain: make(map[uint64]int),
		EdgesByDEX:   make(map[string]int),
	}

	for _, chainID := range g.config.EnabledChains {
		pools := g.registry.GetAllPools(chainID)

		for _, pool := range pools {

			if pool.TotalScore < g.config.MinPoolScore {
				continue
			}

			g.addPoolEdgeLocked(pool)
		}
	}

	g.stats.LastRebuild = time.Now()
	g.stats.RebuildDuration = time.Since(start)

	fmt.Printf("[Graph] Built graph: %d nodes, %d edges in %v\n",
		g.stats.NodeCount, g.stats.EdgeCount, g.stats.RebuildDuration)

	return nil
}

func (g *TokenGraph) addPoolEdgeLocked(pool *discovery.DiscoveredPool) {
	chainID := pool.ChainID

	if g.Nodes[chainID] == nil {
		g.Nodes[chainID] = make(map[common.Address]*TokenNode)
	}
	if g.Edges[chainID] == nil {
		g.Edges[chainID] = make(map[common.Address][]*PoolEdge)
	}
	if g.PoolEdges[chainID] == nil {
		g.PoolEdges[chainID] = make(map[common.Address]*PoolEdge)
	}

	node0 := g.getOrCreateNodeLocked(chainID, pool.Token0)
	node1 := g.getOrCreateNodeLocked(chainID, pool.Token1)

	edge := &PoolEdge{
		Pool:        pool.Address,
		ChainID:     chainID,
		DEX:         pool.DEXType,
		Token0:      pool.Token0,
		Token1:      pool.Token1,
		Reserve0:    pool.Reserve0,
		Reserve1:    pool.Reserve1,
		Fee:         pool.Fee,
		TickSpacing: pool.TickSpacing,
		Liquidity:   pool.Liquidity,
		LastUpdate:  pool.LastUpdated,
		TotalScore:  pool.TotalScore,
	}

	CalculateEdgeWeights(edge)

	CalculateMaxAmounts(edge)

	g.Edges[chainID][pool.Token0] = append(g.Edges[chainID][pool.Token0], edge)
	g.Edges[chainID][pool.Token1] = append(g.Edges[chainID][pool.Token1], edge)

	g.PoolEdges[chainID][pool.Address] = edge

	node0.EdgeCount++
	node1.EdgeCount++

	g.stats.EdgeCount++
	g.stats.EdgesByChain[chainID]++
	g.stats.EdgesByDEX[pool.DEXType]++
}

func (g *TokenGraph) getOrCreateNodeLocked(chainID uint64, token common.Address) *TokenNode {
	if node, exists := g.Nodes[chainID][token]; exists {
		return node
	}

	node := &TokenNode{
		Address:    token,
		ChainID:    chainID,
		IsBluechip: IsBluechip(chainID, token),
		IsStable:   IsStable(chainID, token),
		BridgedTo:  make(map[uint64]common.Address),
	}

	g.Nodes[chainID][token] = node
	g.stats.NodeCount++
	g.stats.NodesByChain[chainID]++

	if node.IsBluechip {
		g.stats.BluechipTokens++
	}

	return node
}

func (g *TokenGraph) AddCrossChainEdge(edge CrossChainEdge) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.CrossChainEdges[edge.Token] = append(g.CrossChainEdges[edge.Token], edge)

	if srcNode := g.Nodes[edge.SourceChain][edge.SourceAddress]; srcNode != nil {
		srcNode.BridgedTo[edge.DestChain] = edge.DestAddress
	}
	if dstNode := g.Nodes[edge.DestChain][edge.DestAddress]; dstNode != nil {
		dstNode.BridgedTo[edge.SourceChain] = edge.SourceAddress
	}

	g.stats.CrossChainEdges++
}

func (g *TokenGraph) GetNode(chainID uint64, token common.Address) *TokenNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if chainNodes, ok := g.Nodes[chainID]; ok {
		return chainNodes[token]
	}
	return nil
}

func (g *TokenGraph) GetEdge(chainID uint64, pool common.Address) *PoolEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if chainEdges, ok := g.PoolEdges[chainID]; ok {
		return chainEdges[pool]
	}
	return nil
}

func (g *TokenGraph) GetNeighbors(chainID uint64, token common.Address) []*PoolEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if chainEdges, ok := g.Edges[chainID]; ok {
		return chainEdges[token]
	}
	return nil
}

func (g *TokenGraph) GetNeighborTokens(chainID uint64, token common.Address) []common.Address {
	edges := g.GetNeighbors(chainID, token)
	seen := make(map[common.Address]bool)
	var tokens []common.Address

	for _, edge := range edges {
		other := edge.GetTokenOut(token)
		if !seen[other] {
			seen[other] = true
			tokens = append(tokens, other)
		}
	}

	return tokens
}

func (g *TokenGraph) GetEdgesBetween(chainID uint64, token0, token1 common.Address) []*PoolEdge {
	edges := g.GetNeighbors(chainID, token0)
	var matching []*PoolEdge

	for _, edge := range edges {
		if edge.GetTokenOut(token0) == token1 {
			matching = append(matching, edge)
		}
	}

	return matching
}

func (g *TokenGraph) GetBestEdgeBetween(chainID uint64, tokenIn, tokenOut common.Address) *PoolEdge {
	edges := g.GetEdgesBetween(chainID, tokenIn, tokenOut)
	if len(edges) == 0 {
		return nil
	}

	best := edges[0]
	bestWeight := best.GetWeight(tokenIn)

	for _, edge := range edges[1:] {
		weight := edge.GetWeight(tokenIn)
		if weight < bestWeight {
			best = edge
			bestWeight = weight
		}
	}

	return best
}

func (g *TokenGraph) GetAllNodes(chainID uint64) []*TokenNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	chainNodes := g.Nodes[chainID]
	nodes := make([]*TokenNode, 0, len(chainNodes))
	for _, node := range chainNodes {
		nodes = append(nodes, node)
	}
	return nodes
}

func (g *TokenGraph) GetBluechipNodes(chainID uint64) []*TokenNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var nodes []*TokenNode
	for _, node := range g.Nodes[chainID] {
		if node.IsBluechip {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (g *TokenGraph) GetCrossChainEdges(token common.Address) []CrossChainEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.CrossChainEdges[token]
}

func (g *TokenGraph) HasPath(chainID uint64, from, to common.Address, maxHops int) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if from == to {
		return true
	}

	visited := make(map[common.Address]bool)
	queue := []struct {
		token common.Address
		depth int
	}{{from, 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxHops {
			continue
		}

		if visited[current.token] {
			continue
		}
		visited[current.token] = true

		for _, edge := range g.Edges[chainID][current.token] {
			next := edge.GetTokenOut(current.token)
			if next == to {
				return true
			}
			if !visited[next] {
				queue = append(queue, struct {
					token common.Address
					depth int
				}{next, current.depth + 1})
			}
		}
	}

	return false
}

func (g *TokenGraph) UpdateEdge(pool *discovery.DiscoveredPool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	edge, ok := g.PoolEdges[pool.ChainID][pool.Address]
	if !ok {
		return
	}

	edge.Reserve0 = pool.Reserve0
	edge.Reserve1 = pool.Reserve1
	edge.Liquidity = pool.Liquidity
	edge.LastUpdate = pool.LastUpdated
	edge.TotalScore = pool.TotalScore

	CalculateEdgeWeights(edge)
	CalculateMaxAmounts(edge)
}

func (g *TokenGraph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.stats
}

func (g *TokenGraph) PrintSummary() {
	stats := g.Stats()
	fmt.Printf("[Graph] Summary:\n")
	fmt.Printf("  Total nodes: %d\n", stats.NodeCount)
	fmt.Printf("  Total edges: %d\n", stats.EdgeCount)
	fmt.Printf("  Bluechip tokens: %d\n", stats.BluechipTokens)
	fmt.Printf("  Cross-chain edges: %d\n", stats.CrossChainEdges)
	fmt.Printf("  Nodes by chain:\n")
	for chainID, count := range stats.NodesByChain {
		fmt.Printf("    Chain %d: %d\n", chainID, count)
	}
	fmt.Printf("  Edges by DEX:\n")
	for dex, count := range stats.EdgesByDEX {
		fmt.Printf("    %s: %d\n", dex, count)
	}
}

func (g *TokenGraph) GetChainIDs() []uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	chains := make([]uint64, 0, len(g.Nodes))
	for chainID := range g.Nodes {
		chains = append(chains, chainID)
	}
	return chains
}

func (g *TokenGraph) GetTokensOnChain(chainID uint64) []common.Address {
	g.mu.RLock()
	defer g.mu.RUnlock()

	chainNodes := g.Nodes[chainID]
	if chainNodes == nil {
		return nil
	}

	tokens := make([]common.Address, 0, len(chainNodes))
	for token := range chainNodes {
		tokens = append(tokens, token)
	}
	return tokens
}

func (g *TokenGraph) GetPoolsOnChain(chainID uint64) []*PoolInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()

	poolEdges := g.PoolEdges[chainID]
	if poolEdges == nil {
		return nil
	}

	pools := make([]*PoolInfo, 0, len(poolEdges))
	for _, edge := range poolEdges {
		pools = append(pools, &PoolInfo{
			Address:  edge.Pool,
			Token0:   edge.Token0,
			Token1:   edge.Token1,
			Reserve0: edge.Reserve0,
			Reserve1: edge.Reserve1,
			Fee:      edge.Fee,
			DEX:      edge.DEX,
		})
	}
	return pools
}

func (g *TokenGraph) FindPool(tokenIn, tokenOut common.Address, chainID uint64) *PoolInfo {

	edges := g.GetEdgesBetween(chainID, tokenIn, tokenOut)
	if len(edges) == 0 {
		return nil
	}

	edge := edges[0]
	return &PoolInfo{
		Address:  edge.Pool,
		Token0:   edge.Token0,
		Token1:   edge.Token1,
		Reserve0: edge.Reserve0,
		Reserve1: edge.Reserve1,
		Fee:      edge.Fee,
		DEX:      edge.DEX,
	}
}

type PoolInfo struct {
	Address  common.Address
	Token0   common.Address
	Token1   common.Address
	Reserve0 *big.Int
	Reserve1 *big.Int
	Fee      uint32
	DEX      string
}
