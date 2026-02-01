package quantum

import (
	"fmt"
	"math"
	"math/big"
	"sync"
)

type TransformerConfig struct {
	NumLayers     int
	NumHeads      int
	IterationsT   int
	LearningRate  float64
	InitThreshold int
}

func DefaultTransformerConfig() TransformerConfig {
	return TransformerConfig{
		NumLayers:     6,
		NumHeads:      4,
		IterationsT:   12,
		LearningRate:  0.1,
		InitThreshold: 50,
	}
}

type TransformerLayer struct {
	Index       int
	Gate        *Gate
	Contract    QuantumContract
	Threshold   float64
	FiringCount int
	AvgPressure float64
	mu          sync.Mutex
}

type QuantumTransformer struct {
	Config    TransformerConfig
	Layers    []*TransformerLayer
	Substrate *SubstrateChain
	Mesh      *Mesh

	currentPressure PressureMetrics
	outputCoords    []*big.Int

	mu sync.RWMutex
}

func NewQuantumTransformer(cfg TransformerConfig, mesh *Mesh) *QuantumTransformer {
	qt := &QuantumTransformer{
		Config: cfg,
		Mesh:   mesh,
		Layers: make([]*TransformerLayer, cfg.NumLayers),
	}

	for _, chain := range mesh.Chains {
		qt.Substrate = NewSubstrateChain(chain)
		break
	}

	gateTypes := []string{"hadamard", "paulix", "pauliz"}
	for i := 0; i < cfg.NumLayers; i++ {
		gateType := gateTypes[i%len(gateTypes)]

		qt.Layers[i] = &TransformerLayer{
			Index: i,
			Gate: &Gate{
				Position:    i,
				Restriction: cfg.InitThreshold,
				Type:        gateType,
			},
			Contract: QuantumContract{
				Transform: "feistel",
				T:         cfg.IterationsT,
				K:         64,
				Phase:     "alt",
				Contexts:  []string{fmt.Sprintf("mul:%d", i+1)},
			},
			Threshold:   float64(cfg.InitThreshold),
			FiringCount: 0,
			AvgPressure: 0.5,
		}
	}

	return qt
}

func (qt *QuantumTransformer) Forward(prompt string, maxTokens int) ([]string, PressureMetrics) {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	inputCoord := UTF8_ENCODE(qt.Substrate, prompt)

	qt.currentPressure = qt.computeInputPressure(prompt)

	outputs := make([]string, 0, maxTokens)
	qt.outputCoords = make([]*big.Int, 0, maxTokens)

	currentCoord := inputCoord
	for token := 0; token < maxTokens; token++ {

		for _, layer := range qt.Layers {
			currentCoord = qt.runLayerFast(layer, currentCoord)
		}

		qt.outputCoords = append(qt.outputCoords, new(big.Int).Set(currentCoord))

		word := UTF8_DECODE(currentCoord)
		if word != "" {
			outputs = append(outputs, word)
		}

		currentCoord = FeistelHash(currentCoord)

		qt.updatePressure(token, maxTokens)

		if qt.currentPressure.PauliX < 0.1 {
			break
		}
	}

	return outputs, qt.currentPressure
}

func (qt *QuantumTransformer) runLayerFast(layer *TransformerLayer, coord *big.Int) *big.Int {

	gatePressure := int(qt.currentPressure.Hadamard * 100)
	threshold := layer.Threshold

	if float64(gatePressure) <= threshold {

		layer.Contract.Contexts = []string{
			fmt.Sprintf("mul:%d", int(qt.currentPressure.Hadamard*10)+1),
			fmt.Sprintf("add:%d", int(qt.currentPressure.PauliZ*100)),
		}
		if qt.currentPressure.PauliX > 0.7 {
			layer.Contract.Contexts = append(layer.Contract.Contexts, "dom:coherent")
		}

		result, err := executeContract(qt.Substrate, layer.Contract)
		if err != nil {
			return FeistelHash(coord)
		}

		newCoord := new(big.Int).Xor(coord, result.Result)
		qt.currentPressure = blendPressure(qt.currentPressure, result.Pressure)
		return newCoord
	}

	return qt.runLayer(layer, coord)
}

func (qt *QuantumTransformer) runLayer(layer *TransformerLayer, coord *big.Int) *big.Int {
	layer.mu.Lock()
	defer layer.mu.Unlock()

	gatePressure := int(qt.currentPressure.Hadamard * 100)

	shouldFire := float64(gatePressure) > layer.Threshold

	if shouldFire {

		state := QuantumState{
			Amplitude: qt.currentPressure.Hadamard,
			Phase:     qt.currentPressure.PauliX - 0.5,
		}

		var newState QuantumState
		switch layer.Gate.Type {
		case "hadamard":
			newState = Hadamard(state)
		case "paulix":
			newState = PauliX(state)
		case "pauliz":
			newState = PauliZ(state)
		default:
			newState = state
		}

		qt.currentPressure.Hadamard = math.Abs(newState.Amplitude)
		qt.currentPressure.PauliZ += 0.1

		layer.FiringCount++
		layer.AvgPressure = layer.AvgPressure*0.9 + qt.currentPressure.Hadamard*0.1

		targetFiringRate := 0.3
		actualFiringRate := float64(layer.FiringCount) / float64(layer.FiringCount+1)
		if actualFiringRate > targetFiringRate {
			layer.Threshold += qt.Config.LearningRate * 10
		} else {
			layer.Threshold -= qt.Config.LearningRate * 5
		}

		if layer.Threshold < 10 {
			layer.Threshold = 10
		}
		if layer.Threshold > 90 {
			layer.Threshold = 90
		}
	}

	layer.Contract.Contexts = []string{
		fmt.Sprintf("mul:%d", int(qt.currentPressure.Hadamard*10)+1),
		fmt.Sprintf("add:%d", int(qt.currentPressure.PauliZ*100)),
	}
	if qt.currentPressure.PauliX > 0.7 {
		layer.Contract.Contexts = append(layer.Contract.Contexts, "dom:coherent")
	}

	result, err := executeContract(qt.Substrate, layer.Contract)
	if err != nil {

		return FeistelHash(coord)
	}

	newCoord := new(big.Int).Xor(coord, result.Result)

	qt.currentPressure = blendPressure(qt.currentPressure, result.Pressure)

	return newCoord
}

func (qt *QuantumTransformer) computeInputPressure(prompt string) PressureMetrics {

	live := GetWaveOrigin()
	liveH := Hadamard(live)
	liveX := PauliX(live)
	liveZ := PauliZ(live)

	lengthFactor := math.Min(float64(len(prompt))/100.0, 1.0)

	questionCount := 0
	for _, c := range prompt {
		if c == '?' {
			questionCount++
		}
	}
	tensionFactor := math.Min(float64(questionCount)*0.2, 0.5)

	wordCount := 1
	for _, c := range prompt {
		if c == ' ' {
			wordCount++
		}
	}
	densityFactor := math.Min(float64(wordCount)/20.0, 0.5)

	return PressureMetrics{
		Hadamard: liveH.Amplitude*0.6 + lengthFactor*0.4,
		PauliX:   liveX.Amplitude*0.6 + 0.7*0.4,
		PauliZ:   liveZ.Phase*0.6 + tensionFactor*0.4,
		Phase:    live.Phase*0.6 + densityFactor*0.4,
	}
}

func (qt *QuantumTransformer) updatePressure(tokenIdx, maxTokens int) {
	progress := float64(tokenIdx) / float64(maxTokens)

	qt.currentPressure.PauliX *= 0.95

	qt.currentPressure.PauliZ *= (1.0 - progress*0.1)

	qt.currentPressure.Phase = math.Min(qt.currentPressure.Phase+0.05, 1.0)
}

func blendPressure(a, b PressureMetrics) PressureMetrics {
	alpha := 0.3
	return PressureMetrics{
		Hadamard: a.Hadamard*(1-alpha) + b.Hadamard*alpha,
		PauliX:   a.PauliX*(1-alpha) + b.PauliX*alpha,
		PauliZ:   a.PauliZ*(1-alpha) + b.PauliZ*alpha,
		Phase:    a.Phase*(1-alpha) + b.Phase*alpha,
	}
}

func (qt *QuantumTransformer) GetOutputCoordinates() []*big.Int {
	qt.mu.RLock()
	defer qt.mu.RUnlock()
	return qt.outputCoords
}

func (qt *QuantumTransformer) GetLayerStats() []map[string]interface{} {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	stats := make([]map[string]interface{}, len(qt.Layers))
	for i, layer := range qt.Layers {
		layer.mu.Lock()
		stats[i] = map[string]interface{}{
			"index":        layer.Index,
			"gate_type":    layer.Gate.Type,
			"threshold":    layer.Threshold,
			"firing_count": layer.FiringCount,
			"avg_pressure": layer.AvgPressure,
		}
		layer.mu.Unlock()
	}
	return stats
}

func (qt *QuantumTransformer) ResetStats() {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	for _, layer := range qt.Layers {
		layer.mu.Lock()
		layer.FiringCount = 0
		layer.AvgPressure = 0.5
		layer.Threshold = float64(qt.Config.InitThreshold)
		layer.mu.Unlock()
	}
}

func (qt *QuantumTransformer) OnChainEvent(eventType string, magnitude float64) {
	qt.mu.Lock()
	defer qt.mu.Unlock()

	switch eventType {
	case "SWAP":
		qt.currentPressure.Hadamard += magnitude * 0.1
		qt.currentPressure.PauliZ += 0.05
	case "BLOCK":
		qt.currentPressure.PauliX += 0.1
		qt.currentPressure.Phase += 0.05
	case "LIQUIDATION":
		qt.currentPressure.PauliZ += 0.3
		qt.currentPressure.PauliX -= 0.1
	}

	qt.currentPressure.Hadamard = math.Min(math.Max(qt.currentPressure.Hadamard, 0), 1)
	qt.currentPressure.PauliX = math.Min(math.Max(qt.currentPressure.PauliX, 0), 1)
	qt.currentPressure.PauliZ = math.Min(math.Max(qt.currentPressure.PauliZ, 0), 1)
	qt.currentPressure.Phase = math.Min(math.Max(qt.currentPressure.Phase, 0), 1)
}

func (qt *QuantumTransformer) ExportWeights() []byte {
	qt.mu.RLock()
	defer qt.mu.RUnlock()

	data := make([]byte, 1+len(qt.Layers)*5)
	data[0] = byte(len(qt.Layers))

	for i, layer := range qt.Layers {
		layer.mu.Lock()
		offset := 1 + i*5

		switch layer.Gate.Type {
		case "hadamard":
			data[offset] = 0x01
		case "paulix":
			data[offset] = 0x02
		case "pauliz":
			data[offset] = 0x03
		}

		thresh := uint16(layer.Threshold * 100)
		data[offset+1] = byte(thresh >> 8)
		data[offset+2] = byte(thresh)

		avgP := uint16(layer.AvgPressure * 65535)
		data[offset+3] = byte(avgP >> 8)
		data[offset+4] = byte(avgP)

		layer.mu.Unlock()
	}

	return data
}

func (qt *QuantumTransformer) ImportWeights(data []byte) error {
	if len(data) < 1 {
		return fmt.Errorf("weights data too short")
	}

	numLayers := int(data[0])
	if len(data) < 1+numLayers*5 {
		return fmt.Errorf("weights data truncated")
	}

	qt.mu.Lock()
	defer qt.mu.Unlock()

	for len(qt.Layers) < numLayers {
		qt.Layers = append(qt.Layers, &TransformerLayer{
			Index: len(qt.Layers),
			Gate:  &Gate{Position: len(qt.Layers), Restriction: qt.Config.InitThreshold},
			Contract: QuantumContract{
				Transform: "feistel",
				T:         qt.Config.IterationsT,
				K:         64,
				Phase:     "alt",
			},
		})
	}

	for i := 0; i < numLayers && i < len(qt.Layers); i++ {
		layer := qt.Layers[i]
		offset := 1 + i*5

		layer.mu.Lock()

		switch data[offset] {
		case 0x01:
			layer.Gate.Type = "hadamard"
		case 0x02:
			layer.Gate.Type = "paulix"
		case 0x03:
			layer.Gate.Type = "pauliz"
		}

		thresh := uint16(data[offset+1])<<8 | uint16(data[offset+2])
		layer.Threshold = float64(thresh) / 100.0

		avgP := uint16(data[offset+3])<<8 | uint16(data[offset+4])
		layer.AvgPressure = float64(avgP) / 65535.0

		layer.mu.Unlock()
	}

	return nil
}
