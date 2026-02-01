package quantum

import (
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"time"

	"zeam/node"
)

type NetworkType string

const (
	NetworkL1       NetworkType = "l1"
	NetworkL2       NetworkType = "l2"
	NetworkRollup   NetworkType = "rollup"
	NetworkSidechain NetworkType = "sidechain"
	NetworkLocal    NetworkType = "local"
)

type QuantumService struct {
	mu sync.RWMutex

	Mesh        *Mesh
	Graph       *QuantumGraph
	Transformer *QuantumTransformer

	networks map[NetworkType]*NetworkSource

	onPressureChange []func(PressureMetrics)
	onResonance      []func(*ResonanceResult)

	config ServiceConfig

	feedSub *node.FeedSubscription

	feedEventCount uint64
}

type NetworkSource struct {
	Type       NetworkType
	Name       string
	Connected  bool
	LastHash   []byte
	LastUpdate time.Time
	HashCount  uint64
	FlowRate   float64
}

type ServiceConfig struct {
	MeshChains     int
	GroverWorkers  int
	WalkSteps      int
	PressureDecay  time.Duration
	EnableLogging  bool
}

func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		MeshChains:    8,
		GroverWorkers: 4,
		WalkSteps:     10,
		PressureDecay: 2 * time.Second,
		EnableLogging: false,
	}
}

var (
	globalService     *QuantumService
	globalServiceOnce sync.Once
)

func GetService() *QuantumService {
	globalServiceOnce.Do(func() {
		globalService = NewQuantumService(DefaultServiceConfig())
	})
	return globalService
}

func NewQuantumService(config ServiceConfig) *QuantumService {
	meshConfig := DefaultMeshConfig()
	meshConfig.NumChains = config.MeshChains
	meshConfig.DecayInterval = config.PressureDecay

	mesh := NewMesh(meshConfig)
	mesh.Start()

	return &QuantumService{
		Mesh:     mesh,
		Graph:    NewQuantumGraph(),
		networks: make(map[NetworkType]*NetworkSource),
		config:   config,
	}
}

func (qs *QuantumService) SetTransformer(t *QuantumTransformer) {
	qs.mu.Lock()
	defer qs.mu.Unlock()
	qs.Transformer = t
}

func (qs *QuantumService) RegisterNetwork(netType NetworkType, name string) {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	qs.networks[netType] = &NetworkSource{
		Type:      netType,
		Name:      name,
		Connected: true,
	}
}

func (qs *QuantumService) OnHashReceived(netType NetworkType, hash []byte) {
	qs.mu.Lock()
	source := qs.networks[netType]
	if source == nil {
		source = &NetworkSource{Type: netType, Connected: true}
		qs.networks[netType] = source
	}
	source.LastHash = hash
	source.LastUpdate = time.Now()
	source.HashCount++
	qs.mu.Unlock()

	if qs.Mesh != nil && len(hash) >= 8 {
		chainID := qs.Mesh.HashToChain(string(hash[:8]))
		pressure := hashToPressure(hash)
		qs.Mesh.PushData(chainID, hash, pressure)

		PerturbWaveFromHash(hash)

		metrics := qs.GetPressure()
		qs.mu.RLock()
		callbacks := qs.onPressureChange
		qs.mu.RUnlock()

		for _, cb := range callbacks {
			go cb(metrics)
		}
	}
}

func (qs *QuantumService) OnBlockReceived(netType NetworkType, blockHash []byte, blockNumber uint64) {

	pressure := 0.5 + float64(blockNumber%100)/200.0

	if qs.Mesh != nil && len(blockHash) >= 8 {
		chainID := qs.Mesh.HashToChain(string(blockHash[:8]))
		qs.Mesh.PushData(chainID, blockHash, pressure)
	}

	metrics := qs.GetPressure()
	qs.mu.RLock()
	callbacks := qs.onPressureChange
	qs.mu.RUnlock()

	for _, cb := range callbacks {
		go cb(metrics)
	}
}

func hashToPressure(hash []byte) float64 {
	if len(hash) == 0 {
		return 0.5
	}

	sum := 0
	for i := 0; i < 4 && i < len(hash); i++ {
		sum += int(hash[i])
	}
	return float64(sum) / (4 * 255.0)
}

func (qs *QuantumService) SearchSemantic(targetHash []byte, candidates [][]byte, maxDistance int) *GroverResult {
	return GroverSemanticSearch(qs.Mesh, targetHash, candidates, maxDistance)
}

func (qs *QuantumService) SearchExact(target []byte, searchSpace [][]byte) *GroverResult {
	oracle := func(item []byte) bool {
		if len(item) != len(target) {
			return false
		}
		for i := range item {
			if item[i] != target[i] {
				return false
			}
		}
		return true
	}
	return GroverSearchParallel(qs.Mesh, searchSpace, oracle)
}

func (qs *QuantumService) SearchPrefix(prefix []byte, searchSpace [][]byte) *GroverResult {
	oracle := func(item []byte) bool {
		if len(item) < len(prefix) {
			return false
		}
		for i, b := range prefix {
			if item[i] != b {
				return false
			}
		}
		return true
	}
	return GroverSearchParallel(qs.Mesh, searchSpace, oracle)
}

func (qs *QuantumService) SearchPredicate(searchSpace [][]byte, predicate func([]byte) bool) *GroverResult {
	return GroverSearchParallel(qs.Mesh, searchSpace, predicate)
}

func (qs *QuantumService) FindMinimum(values []uint64) (int, uint64) {
	return GroverMinimumFinding(qs.Mesh, values)
}

func (qs *QuantumService) FindArbitragePath(startToken string, targetProfit float64) *WalkResult {
	return ArbitragePath(qs.Graph, startToken, targetProfit)
}

func (qs *QuantumService) WalkGraph(start string, steps int, target string) *WalkResult {
	return DiscreteTimeQuantumWalk(qs.Graph, start, steps, target)
}

func (qs *QuantumService) WalkContinuous(start string, time float64, target string) *WalkResult {
	return ContinuousTimeQuantumWalk(qs.Graph, start, time, target)
}

func (qs *QuantumService) AddToken(symbol string, data interface{}) {
	qs.Graph.AddNode(symbol, data)
}

func (qs *QuantumService) AddPool(tokenA, tokenB string, exchangeRate float64) {
	qs.Graph.AddUndirectedEdge(tokenA, tokenB, exchangeRate)
}

func (qs *QuantumService) FindPath(start, end string) *WalkResult {
	return WalkSpatialSearch(qs.Graph, start, func(node *GraphNode) bool {
		return node.ID == end
	})
}

func (qs *QuantumService) GetPressure() PressureMetrics {
	if qs.Mesh == nil {
		return PressureMetrics{Hadamard: 0.5, PauliX: 0.5, PauliZ: 0.3, Phase: 0.5}
	}
	return BuildPressureFromMesh(qs.Mesh)
}

func (qs *QuantumService) GetResonance() *ResonanceResult {
	if qs.Mesh == nil {
		return nil
	}
	return qs.Mesh.FindStrongestResonance()
}

func (qs *QuantumService) GetTopResonances(n int) []*ResonanceResult {
	if qs.Mesh == nil {
		return nil
	}
	return qs.Mesh.FindTopResonances(n)
}

func (qs *QuantumService) GetCoherence() float64 {
	if qs.Mesh == nil {
		return 0.5
	}
	return qs.Mesh.GetPhaseCoherence()
}

func (qs *QuantumService) ApplyGate(gateType string) error {
	if qs.Mesh == nil {
		return nil
	}
	return qs.Mesh.ApplyWaveFunction(gateType)
}

func (qs *QuantumService) AddPressure(chainID string, blockIdx int, pressure float64) {
	if qs.Mesh == nil || qs.Mesh.Pressure == nil {
		return
	}
	loc := BlockLocation{ChainID: chainID, BlockIdx: blockIdx}
	qs.Mesh.Pressure.AddPressure(loc, pressure)
}

func (qs *QuantumService) Transform(prompt string, maxTokens int) ([]string, PressureMetrics) {
	qs.mu.RLock()
	transformer := qs.Transformer
	qs.mu.RUnlock()

	if transformer == nil {
		return nil, qs.GetPressure()
	}
	return transformer.Forward(prompt, maxTokens)
}

func (qs *QuantumService) GetOutputCoordinates() []string {
	qs.mu.RLock()
	transformer := qs.Transformer
	qs.mu.RUnlock()

	if transformer == nil {
		return nil
	}

	coords := make([]string, len(transformer.outputCoords))
	for i, c := range transformer.outputCoords {
		coords[i] = c.String()
	}
	return coords
}

func (qs *QuantumService) ComputeHash(input []byte) []byte {

	if qs.Mesh == nil {
		h := sha256.Sum256(input)
		return h[:]
	}

	result := make([]byte, 32)
	copy(result, input)
	if len(result) < 32 {
		result = append(result, make([]byte, 32-len(result))...)
	}

	qs.Mesh.mu.RLock()
	var chainID string
	for id := range qs.Mesh.Chains {
		chainID = id
		break
	}
	qs.Mesh.mu.RUnlock()

	if chainID != "" {
		state := ReadState(chainID)

		phaseByte := byte(state.Phase * 255)
		for i := range result {
			result[i] ^= phaseByte
		}
	}

	h := sha256.Sum256(result)
	return h[:]
}

func (qs *QuantumService) OnPressureChange(cb func(PressureMetrics)) {
	qs.mu.Lock()
	defer qs.mu.Unlock()
	qs.onPressureChange = append(qs.onPressureChange, cb)
}

func (qs *QuantumService) OnResonance(cb func(*ResonanceResult)) {
	qs.mu.Lock()
	defer qs.mu.Unlock()
	qs.onResonance = append(qs.onResonance, cb)
}

func (qs *QuantumService) Stats() map[string]interface{} {
	qs.mu.RLock()
	defer qs.mu.RUnlock()

	stats := make(map[string]interface{})

	networkStats := make(map[string]interface{})
	totalHashes := uint64(0)
	for netType, source := range qs.networks {
		networkStats[string(netType)] = map[string]interface{}{
			"name":       source.Name,
			"connected":  source.Connected,
			"hash_count": source.HashCount,
			"flow_rate":  source.FlowRate,
		}
		totalHashes += source.HashCount
	}
	stats["networks"] = networkStats
	stats["total_hashes_received"] = totalHashes

	if qs.Mesh != nil {
		stats["mesh"] = qs.Mesh.Stats()
	}

	stats["graph_nodes"] = len(qs.Graph.Nodes)

	origin := GetWaveOrigin()
	stats["live_amplitude"] = origin.Amplitude
	stats["live_phase"] = origin.Phase

	perturbCount, lastPerturbTime := GetPerturbStats()
	stats["perturb_count"] = perturbCount
	stats["last_perturb_time"] = lastPerturbTime

	stats["feed_event_count"] = atomic.LoadUint64(&qs.feedEventCount)

	return stats
}

func (qs *QuantumService) Stop() {
	qs.mu.Lock()
	sub := qs.feedSub
	qs.feedSub = nil
	qs.mu.Unlock()

	if sub != nil {
		sub.Unsubscribe()
	}
	if qs.Mesh != nil {
		qs.Mesh.Stop()
	}
}

func (qs *QuantumService) ConnectFeed(feed node.NetworkFeed) {
	sub := feed.Subscribe(512)

	qs.mu.Lock()
	qs.feedSub = sub
	qs.mu.Unlock()

	go qs.feedLoop(sub)
}

func (qs *QuantumService) feedLoop(sub *node.FeedSubscription) {
	for event := range sub.C {

		atomic.AddUint64(&qs.feedEventCount, 1)

		switch event.Type {
		case node.EventTxHashes:
			netType := ChainIDToNetworkType(event.ChainID)
			for _, h := range event.Hashes {
				qs.OnHashReceived(netType, h[:])
			}
		case node.EventL1Block:
			qs.OnBlockReceived(NetworkL1, event.L1BlockHash[:], event.L1BlockNumber)
		case node.EventL2Block:
			if event.L2Block != nil {
				qs.OnBlockReceived(NetworkL2, event.L2Block.BlockHash[:], event.L2Block.BlockNumber)
			}
		}
	}
}

func ChainIDToNetworkType(chainID uint64) NetworkType {
	switch chainID {
	case 1:
		return NetworkL1
	case 8453, 84532:
		return NetworkL2
	case 10, 11155420:
		return NetworkL2
	case 42161, 421614:
		return NetworkRollup
	default:
		if chainID == 0 {
			return NetworkL1
		}
		return NetworkL2
	}
}
