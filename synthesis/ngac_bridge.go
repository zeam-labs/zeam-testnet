package synthesis

import (
	"crypto/sha256"
	"math"
	"math/big"
	"strings"
	"sync"

	"zeam/ngac"
	"zeam/quantum"
)

type NGACBridge struct {
	ngac      *ngac.NGAC
	substrate *quantum.SubstrateChain
	mesh      *quantum.Mesh

	outputBuffer strings.Builder
	tokenCount   int
	lastPressure quantum.PressureMetrics

	mu sync.Mutex
}

func NewNGACBridge(oewnPath string) (*NGACBridge, error) {

	n, err := ngac.New(oewnPath)
	if err != nil {
		return nil, err
	}

	meshCfg := quantum.DefaultMeshConfig()
	mesh := quantum.NewMesh(meshCfg)

	var chain *quantum.Chain
	for _, c := range mesh.Chains {
		chain = c
		break
	}

	if err := n.Initialize(chain); err != nil {
		return nil, err
	}

	return &NGACBridge{
		ngac:      n,
		mesh:      mesh,
		substrate: n.Substrate,
	}, nil
}

func (b *NGACBridge) OnToken(token string) quantum.PressureMetrics {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.outputBuffer.WriteString(token)
	b.tokenCount++

	pressure := b.calculateTokenPressure(token)

	b.lastPressure = blendPressure(b.lastPressure, pressure, 0.3)

	return b.lastPressure
}

func (b *NGACBridge) OnGeneration(prompt, output string) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	pressure := b.calculateGenerationPressure(prompt, output)

	seed := textToSeed(output)

	speech := b.ngac.SpeakWithSeed(pressure, seed)

	b.outputBuffer.Reset()
	b.tokenCount = 0

	return speech
}

func (b *NGACBridge) GetPressure() quantum.PressureMetrics {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastPressure
}

func (b *NGACBridge) GetNGAC() *ngac.NGAC {
	return b.ngac
}

func (b *NGACBridge) GetMesh() *quantum.Mesh {
	return b.mesh
}

func (b *NGACBridge) calculateTokenPressure(token string) quantum.PressureMetrics {

	magnitude := math.Min(float64(len(token))/10.0, 1.0)

	lowerCount := 0
	for _, c := range token {
		if c >= 'a' && c <= 'z' {
			lowerCount++
		}
	}
	coherence := 0.5
	if len(token) > 0 {
		coherence = float64(lowerCount) / float64(len(token))
	}

	specialCount := 0
	for _, c := range token {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != ' ' {
			specialCount++
		}
	}
	tension := math.Min(float64(specialCount)/3.0, 1.0)

	density := math.Min(float64(b.tokenCount)/100.0, 1.0)

	return quantum.PressureMetrics{
		Hadamard: magnitude,
		PauliX: coherence,
		PauliZ:   tension,
		Phase:   density,
	}
}

func (b *NGACBridge) calculateGenerationPressure(prompt, output string) quantum.PressureMetrics {

	ratio := float64(len(output)) / math.Max(float64(len(prompt)), 1.0)
	magnitude := math.Min(ratio/5.0, 1.0)

	sentences := strings.Count(output, ". ") + strings.Count(output, ".\n") + 1
	avgSentLen := float64(len(output)) / float64(sentences)
	coherence := math.Min(avgSentLen/50.0, 1.0)

	questionMarks := strings.Count(output, "?")
	exclamations := strings.Count(output, "!")
	tension := math.Min(float64(questionMarks+exclamations)/5.0, 1.0)

	words := strings.Fields(strings.ToLower(output))
	unique := make(map[string]bool)
	for _, w := range words {
		unique[w] = true
	}
	density := 0.5
	if len(words) > 0 {
		density = float64(len(unique)) / float64(len(words))
	}

	return quantum.PressureMetrics{
		Hadamard: magnitude,
		PauliX: coherence,
		PauliZ:   tension,
		Phase:   density,
	}
}

func blendPressure(old, new quantum.PressureMetrics, alpha float64) quantum.PressureMetrics {
	return quantum.PressureMetrics{
		Hadamard: old.Hadamard*(1-alpha) + new.Hadamard*alpha,
		PauliX: old.PauliX*(1-alpha) + new.PauliX*alpha,
		PauliZ:   old.PauliZ*(1-alpha) + new.PauliZ*alpha,
		Phase:   old.Phase*(1-alpha) + new.Phase*alpha,
	}
}

func textToSeed(text string) *big.Int {
	hash := sha256.Sum256([]byte(text))
	return new(big.Int).SetBytes(hash[:])
}

func (b *NGACBridge) ContextualResponse(query, llmOutput string) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	pressure := b.calculateGenerationPressure(query, llmOutput)

	return b.ngac.ContextualSpeak(query, pressure)
}

func (b *NGACBridge) OnChainEvent(eventType string, amount float64, metadata string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var eventPressure quantum.PressureMetrics

	switch eventType {
	case "SWAP":

		magnitude := math.Min(amount/1e18, 1.0)
		eventPressure = quantum.PressureMetrics{
			Hadamard: magnitude,
			PauliX: 0.7,
			PauliZ:   magnitude * 0.5,
			Phase:   0.5,
		}
	case "BLOCK":

		eventPressure = quantum.PressureMetrics{
			Hadamard: math.Max(0.3, amount),
			PauliX: 0.9,
			PauliZ:   0.1,
			Phase:   0.7,
		}
	case "PENDING":

		eventPressure = quantum.PressureMetrics{
			Hadamard: 0.4,
			PauliX: 0.6,
			PauliZ:   math.Min(amount+0.3, 1.0),
			Phase:   0.5,
		}
	case "FLASHBLOCK":

		eventPressure = quantum.PressureMetrics{
			Hadamard: math.Max(0.5, amount),
			PauliX: 0.95,
			PauliZ:   0.2,
			Phase:   0.8,
		}
	case "LIQUIDATION":

		eventPressure = quantum.PressureMetrics{
			Hadamard: 0.8,
			PauliX: 0.3,
			PauliZ:   0.9,
			Phase:   0.6,
		}
	case "OPPORTUNITY":

		eventPressure = quantum.PressureMetrics{
			Hadamard: 0.9,
			PauliX: 0.8,
			PauliZ:   0.7,
			Phase:   0.8,
		}
	default:

		eventPressure = quantum.PressureMetrics{
			Hadamard: 0.2,
			PauliX: 0.5,
			PauliZ:   0.2,
			Phase:   0.3,
		}
	}

	b.lastPressure = blendPressure(b.lastPressure, eventPressure, 0.4)

}

func (b *NGACBridge) GetDefinition(word string) string {
	return b.ngac.GetDefinition(word)
}

func (b *NGACBridge) GetSynonyms(word string) []string {
	return b.ngac.GetSynonyms(word)
}

func (b *NGACBridge) WordCount() int {
	return b.ngac.WordCount()
}
