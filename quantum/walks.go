package quantum

import (
	"math"
	"math/cmplx"
	"sync"
)

type GraphNode struct {
	ID        string
	Neighbors []string
	Data      interface{}
	Weight    float64
	Labels    map[string]string
}

type QuantumGraph struct {
	Nodes map[string]*GraphNode
	mu    sync.RWMutex
}

func NewQuantumGraph() *QuantumGraph {
	return &QuantumGraph{
		Nodes: make(map[string]*GraphNode),
	}
}

func (g *QuantumGraph) AddNode(id string, data interface{}) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Nodes[id] = &GraphNode{
		ID:        id,
		Neighbors: make([]string, 0),
		Data:      data,
		Weight:    1.0,
		Labels:    make(map[string]string),
	}
}

func (g *QuantumGraph) AddEdge(from, to string, weight float64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if node, exists := g.Nodes[from]; exists {
		node.Neighbors = append(node.Neighbors, to)
		if weight > 0 {
			node.Weight = weight
		}
	}
}

func (g *QuantumGraph) AddUndirectedEdge(a, b string, weight float64) {
	g.AddEdge(a, b, weight)
	g.AddEdge(b, a, weight)
}

type WalkState struct {

	Position map[string]complex128

	Coin map[string]complex128

	Path []string
}

func NewWalkState(startNode string) *WalkState {
	return &WalkState{
		Position: map[string]complex128{
			startNode: complex(1, 0),
		},
		Coin: make(map[string]complex128),
		Path: []string{startNode},
	}
}

type WalkResult struct {
	FinalNode   string
	Path        []string
	Probability float64
	Steps       int
}

func DiscreteTimeQuantumWalk(graph *QuantumGraph, start string, steps int, target string) *WalkResult {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	if _, exists := graph.Nodes[start]; !exists {
		return nil
	}

	state := NewWalkState(start)

	for step := 0; step < steps; step++ {

		state = applyCoinOperator(graph, state)

		state = applyShiftOperator(graph, state)

		if target != "" {
			state = markTarget(state, target)
		}
	}

	maxProb := 0.0
	maxNode := start
	for node, amp := range state.Position {
		prob := real(amp*cmplx.Conj(amp))
		if prob > maxProb {
			maxProb = prob
			maxNode = node
		}
	}

	return &WalkResult{
		FinalNode:   maxNode,
		Path:        state.Path,
		Probability: maxProb,
		Steps:       steps,
	}
}

func applyCoinOperator(graph *QuantumGraph, state *WalkState) *WalkState {
	newState := &WalkState{
		Position: make(map[string]complex128),
		Coin:     make(map[string]complex128),
		Path:     state.Path,
	}

	for nodeID, posAmp := range state.Position {
		node := graph.Nodes[nodeID]
		if node == nil || len(node.Neighbors) == 0 {
			newState.Position[nodeID] = posAmp
			continue
		}

		d := len(node.Neighbors)
		uniformAmp := complex(1.0/math.Sqrt(float64(d)), 0)

		mean := complex(0, 0)
		for _, neighbor := range node.Neighbors {
			coinKey := nodeID + "->" + neighbor
			if amp, exists := state.Coin[coinKey]; exists {
				mean += amp
			} else {

				mean += posAmp / complex(float64(d), 0)
			}
		}
		mean /= complex(float64(d), 0)

		for _, neighbor := range node.Neighbors {
			coinKey := nodeID + "->" + neighbor
			oldAmp := state.Coin[coinKey]
			if oldAmp == 0 {
				oldAmp = posAmp / complex(float64(d), 0)
			}

			newState.Coin[coinKey] = 2*mean - oldAmp

			newState.Position[nodeID] = posAmp * uniformAmp
		}
	}

	return newState
}

func applyShiftOperator(graph *QuantumGraph, state *WalkState) *WalkState {
	newState := &WalkState{
		Position: make(map[string]complex128),
		Coin:     make(map[string]complex128),
		Path:     state.Path,
	}

	for nodeID := range state.Position {
		node := graph.Nodes[nodeID]
		if node == nil {
			continue
		}

		for _, neighbor := range node.Neighbors {
			coinKey := nodeID + "->" + neighbor
			coinAmp := state.Coin[coinKey]

			newState.Position[neighbor] += coinAmp

			reverseCoinKey := neighbor + "->" + nodeID
			newState.Coin[reverseCoinKey] = coinAmp
		}
	}

	return newState
}

func markTarget(state *WalkState, target string) *WalkState {
	if amp, exists := state.Position[target]; exists {
		state.Position[target] = -amp
	}
	return state
}

func ContinuousTimeQuantumWalk(graph *QuantumGraph, start string, time float64, target string) *WalkResult {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	n := len(graph.Nodes)
	if n == 0 {
		return nil
	}

	nodeIndex := make(map[string]int)
	indexNode := make([]string, n)
	i := 0
	for id := range graph.Nodes {
		nodeIndex[id] = i
		indexNode[i] = id
		i++
	}

	startIdx := nodeIndex[start]

	adj := make([][]float64, n)
	degree := make([]float64, n)
	for i := range adj {
		adj[i] = make([]float64, n)
	}

	for id, node := range graph.Nodes {
		i := nodeIndex[id]
		for _, neighbor := range node.Neighbors {
			j := nodeIndex[neighbor]
			adj[i][j] = 1
			degree[i]++
		}
	}

	laplacian := make([][]float64, n)
	for i := range laplacian {
		laplacian[i] = make([]float64, n)
		for j := range laplacian[i] {
			if i == j {
				laplacian[i][j] = degree[i]
			} else {
				laplacian[i][j] = -adj[i][j]
			}
		}
	}

	state := make([]complex128, n)
	state[startIdx] = complex(1, 0)

	dt := 0.01
	steps := int(time / dt)

	for step := 0; step < steps; step++ {
		newState := make([]complex128, n)

		for i := 0; i < n; i++ {
			newState[i] = state[i]
			for j := 0; j < n; j++ {

				newState[i] -= complex(0, laplacian[i][j]*dt) * state[j]
			}
		}

		norm := 0.0
		for _, amp := range newState {
			norm += real(amp * cmplx.Conj(amp))
		}
		norm = math.Sqrt(norm)
		if norm > 0 {
			for i := range newState {
				newState[i] /= complex(norm, 0)
			}
		}

		state = newState
	}

	maxProb := 0.0
	maxIdx := startIdx
	for i, amp := range state {
		prob := real(amp * cmplx.Conj(amp))
		if prob > maxProb {
			maxProb = prob
			maxIdx = i
		}
	}

	return &WalkResult{
		FinalNode:   indexNode[maxIdx],
		Probability: maxProb,
		Steps:       steps,
	}
}

func ArbitragePath(graph *QuantumGraph, startToken string, targetProfit float64) *WalkResult {

	n := len(graph.Nodes)
	steps := int(math.Pi / 4.0 * math.Sqrt(float64(n)))
	if steps < 5 {
		steps = 5
	}

	return profitAwareWalk(graph, startToken, steps, targetProfit)
}

func profitAwareWalk(graph *QuantumGraph, start string, steps int, targetProfit float64) *WalkResult {
	graph.mu.RLock()
	defer graph.mu.RUnlock()

	type profitState struct {
		node   string
		profit float64
		path   []string
	}

	current := []profitState{{
		node:   start,
		profit: 1.0,
		path:   []string{start},
	}}

	for step := 0; step < steps; step++ {
		next := make([]profitState, 0)

		for _, ps := range current {
			node := graph.Nodes[ps.node]
			if node == nil {
				continue
			}

			for _, neighbor := range node.Neighbors {
				neighborNode := graph.Nodes[neighbor]
				if neighborNode == nil {
					continue
				}

				newProfit := ps.profit * neighborNode.Weight

				if neighbor == start && newProfit > 1.0+targetProfit {
					return &WalkResult{
						FinalNode:   neighbor,
						Path:        append(ps.path, neighbor),
						Probability: newProfit - 1.0,
						Steps:       step + 1,
					}
				}

				if newProfit < 0.5 {
					continue
				}

				inPath := false
				for _, p := range ps.path {
					if p == neighbor && neighbor != start {
						inPath = true
						break
					}
				}
				if inPath {
					continue
				}

				next = append(next, profitState{
					node:   neighbor,
					profit: newProfit,
					path:   append(append([]string{}, ps.path...), neighbor),
				})
			}
		}

		current = next
		if len(current) == 0 {
			break
		}

		if len(current) > 100 {

			for i := 0; i < len(current)-1; i++ {
				for j := i + 1; j < len(current); j++ {
					if current[j].profit > current[i].profit {
						current[i], current[j] = current[j], current[i]
					}
				}
			}
			current = current[:100]
		}
	}

	if len(current) == 0 {
		return &WalkResult{FinalNode: start, Probability: 0}
	}

	best := current[0]
	for _, ps := range current[1:] {
		if ps.profit > best.profit {
			best = ps
		}
	}

	return &WalkResult{
		FinalNode:   best.node,
		Path:        best.path,
		Probability: best.profit - 1.0,
		Steps:       steps,
	}
}

func SemanticWalk(graph *QuantumGraph, startConcept string, targetSimilarity float64, steps int) *WalkResult {

	return DiscreteTimeQuantumWalk(graph, startConcept, steps, "")
}

func BuildGraphFromMesh(mesh *Mesh) *QuantumGraph {
	if mesh == nil {
		return NewQuantumGraph()
	}

	graph := NewQuantumGraph()

	mesh.mu.RLock()
	defer mesh.mu.RUnlock()

	for chainID, chain := range mesh.Chains {
		graph.AddNode(chainID, chain)

		for _, anchor := range chain.Anchors {
			graph.AddEdge(chainID, anchor, 1.0)
		}
	}

	return graph
}

func WalkSpatialSearch(graph *QuantumGraph, start string, condition func(*GraphNode) bool) *WalkResult {
	graph.mu.RLock()
	n := len(graph.Nodes)
	graph.mu.RUnlock()

	steps := int(math.Sqrt(float64(n)))
	if steps < 3 {
		steps = 3
	}

	targets := make(map[string]bool)
	graph.mu.RLock()
	for id, node := range graph.Nodes {
		if condition(node) {
			targets[id] = true
		}
	}
	graph.mu.RUnlock()

	if len(targets) == 0 {
		return &WalkResult{FinalNode: start, Probability: 0}
	}

	state := NewWalkState(start)

	for step := 0; step < steps; step++ {
		state = applyCoinOperator(graph, state)
		state = applyShiftOperator(graph, state)

		for target := range targets {
			state = markTarget(state, target)
		}
	}

	maxProb := 0.0
	maxNode := start
	for node, amp := range state.Position {
		prob := real(amp * cmplx.Conj(amp))
		if prob > maxProb && targets[node] {
			maxProb = prob
			maxNode = node
		}
	}

	return &WalkResult{
		FinalNode:   maxNode,
		Probability: maxProb,
		Steps:       steps,
	}
}
