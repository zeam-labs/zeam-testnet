package ngac

import (
	"crypto/sha256"
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
			result.Hadamard += 0.2
			result.PauliZ += 0.1
		}
	}

	for _, word := range calmWords {
		if containsWord(query, word) {
			result.Hadamard -= 0.1
			result.PauliX += 0.1
		}
	}

	if result.Hadamard > 1.0 {
		result.Hadamard = 1.0
	}
	if result.Hadamard < 0.0 {
		result.Hadamard = 0.0
	}
	if result.PauliX > 1.0 {
		result.PauliX = 1.0
	}
	if result.PauliZ > 1.0 {
		result.PauliZ = 1.0
	}

	return result
}

func (n *NGAC) fallbackResponse(query string) string {
	return "System initializing. Please wait."
}

func pressureToSeed(pressure quantum.PressureMetrics) []byte {

	seed := make([]byte, 8)
	seed[0] = byte(pressure.Hadamard * 255)
	seed[1] = byte(pressure.PauliX * 255)
	seed[2] = byte(pressure.PauliZ * 255)
	seed[3] = byte(pressure.Phase * 255)

	seed[4] = byte((pressure.Hadamard + pressure.PauliZ) * 127)
	seed[5] = byte((pressure.PauliX + pressure.Phase) * 127)
	seed[6] = byte((pressure.Hadamard * pressure.PauliX) * 255)
	seed[7] = byte((pressure.PauliZ * pressure.Phase) * 255)
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

func (n *NGAC) SearchWord(query string) (string, float64) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.Dictionary == nil {
		return "", 0
	}

	qs := quantum.GetService()

	words := n.Dictionary.GetAllWords()
	if len(words) == 0 {
		return "", 0
	}

	queryHash := hashWord(query)

	searchSpace := make([][]byte, len(words))
	for i, word := range words {
		searchSpace[i] = hashWord(word)
	}

	result := qs.SearchSemantic(queryHash, searchSpace, 64)

	if result.Found && result.Index < len(words) {
		return words[result.Index], result.Confidence
	}

	return "", 0
}

func (n *NGAC) SearchSimilar(word string, maxResults int) []string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.Dictionary == nil {
		return nil
	}

	synonyms := n.GetSynonyms(word)
	if len(synonyms) == 0 {

		found, _ := n.SearchWord(word)
		if found != "" {
			synonyms = n.GetSynonyms(found)
		}
	}

	if len(synonyms) > maxResults {
		synonyms = synonyms[:maxResults]
	}

	return synonyms
}

func (n *NGAC) SearchByMeaning(meaning string, pos string) []string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.Dictionary == nil || n.Substrate == nil {
		return nil
	}

	qs := quantum.GetService()

	var candidates []string
	switch pos {
	case "noun", "n":
		candidates = n.Dictionary.GetWordsByPOS("n", 1000)
	case "verb", "v":
		candidates = n.Dictionary.GetWordsByPOS("v", 1000)
	case "adj", "adjective":
		candidates = n.Dictionary.GetWordsByPOS("adj", 1000)
	case "adv", "adverb":
		candidates = n.Dictionary.GetWordsByPOS("adv", 1000)
	default:
		candidates = n.Dictionary.GetAllWords()
		if len(candidates) > 1000 {
			candidates = candidates[:1000]
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	meaningHash := hashWord(meaning)
	searchSpace := make([][]byte, len(candidates))
	for i, word := range candidates {

		coord := quantum.UTF8_ENCODE(n.Substrate, word)
		def := n.Dictionary.GetDefinition(coord)
		searchSpace[i] = hashWord(word + " " + def)
	}

	result := qs.SearchSemantic(meaningHash, searchSpace, 128)

	if result.Found && result.Index < len(candidates) {

		results := []string{candidates[result.Index]}

		for i := result.Index - 2; i <= result.Index+2; i++ {
			if i >= 0 && i < len(candidates) && i != result.Index {
				results = append(results, candidates[i])
			}
		}
		return results
	}

	return nil
}

func hashWord(word string) []byte {
	h := sha256.Sum256([]byte(word))
	return h[:]
}
