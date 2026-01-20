

package ngac

import (
	"fmt"
	"math/big"
	"sync"

	"zeam/quantum"
)


type NGAC struct {
	Language      *Language
	Dictionary    *SubstrateDictionary
	SemanticField *SemanticField
	Substrate     *quantum.SubstrateChain
	Speech        *SpeechGenerator

	oewnPath    string
	initialized bool
	mu          sync.RWMutex
}


func New(oewnPath string) (*NGAC, error) {
	n := &NGAC{
		oewnPath: oewnPath,
	}

	
	dict, err := BuildDictionaryIndex(oewnPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build dictionary index: %w", err)
	}
	n.Dictionary = dict

	return n, nil
}


func (n *NGAC) Initialize(chain *quantum.Chain) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.initialized {
		return nil
	}

	
	n.Substrate = quantum.NewSubstrateChain(chain)

	
	n.Substrate.MintEvent("NGAC_INIT", fmt.Sprintf("words=%d", len(n.Dictionary.LemmaIndex)))

	
	n.Language = NewEnglishLanguage(n.Substrate)

	
	coreWords := n.Dictionary.GetRandomWords(1000) 
	if err := InitializeSemanticField(n.Substrate, n.Dictionary, coreWords); err != nil {
		fmt.Printf("[NGAC] Warning: failed to initialize core vocabulary: %v\n", err)
	}

	
	n.Substrate.MintEvent("NGAC_VOCAB", fmt.Sprintf("encoded=%d", len(coreWords)))

	
	examples := DefaultSpeechExamples()
	n.SemanticField = BuildSemanticField(n.Substrate, "speech", examples)

	
	n.Speech = NewSpeechGenerator(n.Dictionary, n.Substrate)

	n.initialized = true
	fmt.Printf("[NGAC] Initialized: %d words minted, %d semantic states, speech generator ready\n",
		len(n.Dictionary.LemmaIndex), len(n.SemanticField.Decoder))

	return nil
}


func (n *NGAC) Speak(pressure quantum.PressureMetrics) string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.initialized {
		return "[NGAC not initialized]"
	}

	
	if n.Speech != nil {
		
		seed := pressureToSeed(pressure)
		speech := n.Speech.GenerateSpeech(pressure, seed)
		
		return speech
	}

	
	if n.SemanticField == nil {
		return "[NGAC not initialized]"
	}
	state := n.SemanticField.MeasureSemantics(n.Substrate, pressure, nil)
	return state.Message
}


func (n *NGAC) SpeakWithSeed(pressure quantum.PressureMetrics, seed *big.Int) string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.initialized {
		return "[NGAC not initialized]"
	}

	
	var seedBytes []byte
	if seed != nil {
		seedBytes = seed.Bytes()
	}

	
	if n.Speech != nil {
		speech := n.Speech.GenerateSpeech(pressure, seedBytes)
		
		return speech
	}

	
	if n.SemanticField == nil {
		return "[NGAC not initialized]"
	}
	state := n.SemanticField.MeasureSemantics(n.Substrate, pressure, seedBytes)
	return state.Message
}


func (n *NGAC) GenerateUtterance(seed *big.Int) string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.initialized || n.Language == nil {
		return ""
	}

	
	result := UTTERANCE_PHONOTACTIC(n.Substrate, seed)
	return quantum.UTF8_DECODE(result)
}


func (n *NGAC) QueryToSeed(query string) *big.Int {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.Substrate == nil {
		return big.NewInt(0)
	}

	return quantum.UTF8_ENCODE(n.Substrate, query)
}


func (n *NGAC) WordCount() int {
	if n.Dictionary == nil {
		return 0
	}
	return len(n.Dictionary.LemmaIndex)
}


func (n *NGAC) IsInitialized() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.initialized
}


func (n *NGAC) GetDefinition(word string) string {
	if n.Dictionary == nil || n.Substrate == nil {
		return ""
	}

	coord := quantum.UTF8_ENCODE(n.Substrate, word)
	return n.Dictionary.GetDefinition(coord)
}


func (n *NGAC) GetSynonyms(word string) []string {
	if n.Dictionary == nil || n.Substrate == nil {
		return nil
	}

	coord := quantum.UTF8_ENCODE(n.Substrate, word)
	return n.Dictionary.GetSynonyms(coord)
}


func (n *NGAC) ContextualSpeak(query string, pressure quantum.PressureMetrics) string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.initialized {
		return n.fallbackResponse(query)
	}

	
	seed := n.QueryToSeed(query)

	
	modPressure := n.modulatePressureFromQuery(query, pressure)

	
	if n.Speech != nil {
		speech := n.Speech.GenerateSpeech(modPressure, seed.Bytes())
		
		return speech
	}

	
	seedBytes := seed.Bytes()
	state := n.SemanticField.MeasureSemantics(n.Substrate, modPressure, seedBytes)
	return state.Message
}


func (n *NGAC) modulatePressureFromQuery(query string, base quantum.PressureMetrics) quantum.PressureMetrics {
	
	urgentWords := []string{"urgent", "critical", "emergency", "now", "immediately", "alert"}
	
	calmWords := []string{"status", "info", "check", "how", "what", "tell"}

	result := base

	for _, word := range urgentWords {
		if containsWord(query, word) {
			result.Magnitude += 0.2
			result.Tension += 0.1
		}
	}

	for _, word := range calmWords {
		if containsWord(query, word) {
			result.Magnitude -= 0.1
			result.Coherence += 0.1
		}
	}

	
	if result.Magnitude > 1.0 {
		result.Magnitude = 1.0
	}
	if result.Magnitude < 0.0 {
		result.Magnitude = 0.0
	}
	if result.Coherence > 1.0 {
		result.Coherence = 1.0
	}
	if result.Tension > 1.0 {
		result.Tension = 1.0
	}

	return result
}


func (n *NGAC) fallbackResponse(query string) string {
	return "System initializing. Please wait."
}


func pressureToSeed(pressure quantum.PressureMetrics) []byte {
	
	seed := make([]byte, 8)
	seed[0] = byte(pressure.Magnitude * 255)
	seed[1] = byte(pressure.Coherence * 255)
	seed[2] = byte(pressure.Tension * 255)
	seed[3] = byte(pressure.Density * 255)
	
	seed[4] = byte((pressure.Magnitude + pressure.Tension) * 127)
	seed[5] = byte((pressure.Coherence + pressure.Density) * 127)
	seed[6] = byte((pressure.Magnitude * pressure.Coherence) * 255)
	seed[7] = byte((pressure.Tension * pressure.Density) * 255)
	return seed
}


func containsWord(query, word string) bool {
	
	for i := 0; i <= len(query)-len(word); i++ {
		match := true
		for j := 0; j < len(word); j++ {
			qc := query[i+j]
			wc := word[j]
			
			if qc >= 'A' && qc <= 'Z' {
				qc += 32
			}
			if wc >= 'A' && wc <= 'Z' {
				wc += 32
			}
			if qc != wc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
