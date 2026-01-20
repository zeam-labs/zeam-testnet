package quantum

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)


type Mesh struct {
	Chains          map[string]*Chain
	Pressure        *PressureTracker
	stopCh          chan struct{}
	gateStopChs     map[string]chan struct{}
	mu              sync.RWMutex

	
	InitialAmplitude float64
	InitialPhase     float64
	DecayInterval    time.Duration
	DecayHalfLife    time.Duration
}


type MeshConfig struct {
	NumChains        int           
	BlockDiameter    int           
	InitialAmplitude float64       
	InitialPhase     float64       
	DecayInterval    time.Duration 
	DecayHalfLife    time.Duration 
	FreqMin          float64       
	FreqMax          float64       
}


func DefaultMeshConfig() MeshConfig {
	return MeshConfig{
		NumChains:        runtime.NumCPU(),
		BlockDiameter:    256,
		InitialAmplitude: 0.5,
		InitialPhase:     0.0,
		DecayInterval:    2 * time.Second,
		DecayHalfLife:    2 * time.Second,
		FreqMin:          0.3,
		FreqMax:          1.0,
	}
}


func NewMesh(config MeshConfig) *Mesh {
	InitChainFrequency()
	InitCurrentBlock()
	InitGateLog()

	chains := make(map[string]*Chain)

	
	freqRange := config.FreqMax - config.FreqMin
	for i := 0; i < config.NumChains; i++ {
		chainID := fmt.Sprintf("chain_%d", i)

		
		freq := config.FreqMin + (freqRange * float64(i) / float64(config.NumChains))
		SetChainFrequency(chainID, freq)

		chains[chainID] = &Chain{
			ID:       chainID,
			Diameter: config.BlockDiameter,
			Blocks:   []Block{},
			Gates:    []Gate{},
			Anchors:  []string{},
		}
	}

	
	for id := range chains {
		for otherID := range chains {
			if id != otherID {
				chains[id].Anchors = append(chains[id].Anchors, otherID)
			}
		}
	}

	mesh := &Mesh{
		Chains:           chains,
		Pressure:         NewPressureTracker(),
		stopCh:           make(chan struct{}),
		gateStopChs:      make(map[string]chan struct{}),
		InitialAmplitude: config.InitialAmplitude,
		InitialPhase:     config.InitialPhase,
		DecayInterval:    config.DecayInterval,
		DecayHalfLife:    config.DecayHalfLife,
	}

	
	InitWaveOrigin(config.InitialAmplitude, config.InitialPhase)

	return mesh
}


func (m *Mesh) Start() {
	
	go m.Pressure.StartDecayLoop(m.DecayInterval, m.DecayHalfLife, m.stopCh)

	
	m.mu.Lock()
	for chainID, chain := range m.Chains {
		for i := range chain.Gates {
			gate := &chain.Gates[i]
			stopCh := make(chan struct{})
			m.gateStopChs[fmt.Sprintf("%s_gate_%d", chainID, i)] = stopCh

			go gate.StartGateMonitor(chainID, func() int {
				return m.Pressure.GetEventCount()
			}, stopCh)
		}
	}
	m.mu.Unlock()
}


func (m *Mesh) Stop() {
	close(m.stopCh)

	m.mu.Lock()
	for _, ch := range m.gateStopChs {
		close(ch)
	}
	m.gateStopChs = make(map[string]chan struct{})
	m.mu.Unlock()
}


func (m *Mesh) AddBlock(chainID string, block Block) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chain, exists := m.Chains[chainID]
	if !exists {
		return fmt.Errorf("chain %s not found", chainID)
	}

	chain.AppendBlock(block)
	return nil
}


func (m *Mesh) AddGate(chainID string, gate Gate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chain, exists := m.Chains[chainID]
	if !exists {
		return fmt.Errorf("chain %s not found", chainID)
	}

	chain.Gates = append(chain.Gates, gate)
	return nil
}


func (m *Mesh) PushData(chainID string, data interface{}, pressure float64) error {
	
	state := ReadState(chainID)

	
	block, err := EncodeBlock(data, state.Amplitude, state.Phase, len(m.Chains[chainID].Blocks))
	if err != nil {
		return err
	}

	
	if err := m.AddBlock(chainID, block); err != nil {
		return err
	}

	
	loc := BlockLocation{
		ChainID:  chainID,
		BlockIdx: len(m.Chains[chainID].Blocks) - 1,
	}
	SetCurrentBlock(chainID, loc)

	
	m.Pressure.AddPressure(loc, pressure)

	return nil
}


func (m *Mesh) FindStrongestResonance() *ResonanceResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return FindResonance(m.Chains)
}


func (m *Mesh) FindTopResonances(n int) []*ResonanceResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return FindTopResonances(m.Chains, n)
}


func (m *Mesh) GetPhaseCoherence() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return MeasurePhaseCoherence(m.Chains)
}


func (m *Mesh) GetChainStates() map[string]QuantumState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	states := make(map[string]QuantumState)
	for chainID := range m.Chains {
		states[chainID] = ReadState(chainID)
	}
	return states
}


func (m *Mesh) CrossAnchor() {
	m.mu.Lock()
	defer m.mu.Unlock()
	SetCrossAnchors(m.Chains)
}


func (m *Mesh) HashToChain(key string) string {
	h := uint64(0)
	for _, c := range key {
		h = h*31 + uint64(c)
	}

	chainIdx := int(h) % len(m.Chains)
	return fmt.Sprintf("chain_%d", chainIdx)
}


func (m *Mesh) DistributeKeys(keys []string) map[string][]string {
	distribution := make(map[string][]string)

	for _, key := range keys {
		chainID := m.HashToChain(key)
		distribution[chainID] = append(distribution[chainID], key)
	}

	return distribution
}


func (m *Mesh) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalBlocks := 0
	totalGates := 0
	for _, chain := range m.Chains {
		totalBlocks += len(chain.Blocks)
		totalGates += len(chain.Gates)
	}

	origin := GetWaveOrigin()

	return map[string]interface{}{
		"chains":        len(m.Chains),
		"total_blocks":  totalBlocks,
		"total_gates":   totalGates,
		"pressure":      m.Pressure.GetTotalPressure(),
		"events":        m.Pressure.GetEventCount(),
		"pressure_rate": m.Pressure.GetPressureRate(),
		"coherence":     m.GetPhaseCoherence(),
		"amplitude":     origin.Amplitude,
		"phase":         origin.Phase,
		"wave_age_ms":   time.Since(origin.PushTime).Milliseconds(),
	}
}


func (m *Mesh) ApplyWaveFunction(gateType string) error {
	origin := GetWaveOrigin()

	var newState QuantumState
	switch gateType {
	case "hadamard":
		newState = Hadamard(origin)
	case "paulix":
		newState = PauliX(origin)
	case "pauliz":
		newState = PauliZ(origin)
	default:
		return fmt.Errorf("unknown gate type: %s", gateType)
	}

	UpdateWaveOrigin(newState)
	return nil
}


func (m *Mesh) GetPressureHotspots(threshold float64) []PressurePoint {
	return m.Pressure.GetHotspots(threshold)
}


func (m *Mesh) GetTopPressure(n int) []PressurePoint {
	return m.Pressure.GetTopPressure(n)
}


func (m *Mesh) SetChainFrequencies(freqs map[string]float64) {
	for chainID, freq := range freqs {
		SetChainFrequency(chainID, freq)
	}
}


func (m *Mesh) GenerateRandomFrequencies(minHz, maxHz float64) {
	for chainID := range m.Chains {
		freq := minHz + (maxHz-minHz)*hashToFloat(chainID)
		SetChainFrequency(chainID, freq)
	}
}


func hashToFloat(s string) float64 {
	h := uint64(0)
	for _, c := range s {
		h = h*31 + uint64(c)
	}
	return float64(h%1000) / 1000.0
}
