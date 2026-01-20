

package ngac

import (
	"math"
	"math/big"
	"math/cmplx"
	"strings"

	"zeam/quantum"
)


type SpeechGenerator struct {
	Dictionary *SubstrateDictionary
	Substrate  *quantum.SubstrateChain
}


func NewSpeechGenerator(dict *SubstrateDictionary, sc *quantum.SubstrateChain) *SpeechGenerator {
	return &SpeechGenerator{
		Dictionary: dict,
		Substrate:  sc,
	}
}


func (sg *SpeechGenerator) GenerateSpeech(pressure quantum.PressureMetrics, seed []byte) string {
	if sg.Dictionary == nil || sg.Substrate == nil {
		return ""
	}

	
	seedInt := new(big.Int).SetBytes(seed)
	if seedInt.Sign() == 0 {
		seedInt = big.NewInt(1)
	}

	
	wordCount := sg.calculateWordCount(pressure)

	
	words := make([]string, 0, wordCount)
	currentCoord := seedInt

	for len(words) < wordCount {
		
		word := sg.collapseToWord(currentCoord, pressure)
		if word != "" {
			words = append(words, word)
		}

		
		currentCoord = sg.traverseEdge(currentCoord, pressure, len(words))
	}

	
	if len(words) == 0 {
		return ""
	}

	sentence := strings.Join(words, " ")

	
	if len(sentence) > 0 {
		sentence = strings.ToUpper(string(sentence[0])) + sentence[1:]
	}

	
	if !strings.HasSuffix(sentence, ".") && !strings.HasSuffix(sentence, "!") {
		sentence += "."
	}

	return sentence
}


func (sg *SpeechGenerator) calculateWordCount(pressure quantum.PressureMetrics) int {
	
	if pressure.Tension > 0.6 {
		return 2
	}
	
	if pressure.Coherence > 0.6 {
		return 4
	}
	
	return 3
}


func (sg *SpeechGenerator) collapseToWord(coord *big.Int, pressure quantum.PressureMetrics) string {
	
	if sg.Dictionary.WordExists(coord) {
		word := quantum.UTF8_DECODE(coord)
		if word != "" && len(word) > 1 {
			return strings.ToLower(word)
		}
	}

	
	word := sg.amplitudeCollapse(coord, pressure)
	if word != "" {
		return word
	}

	
	return sg.hashToWord(coord, pressure)
}


func (sg *SpeechGenerator) amplitudeCollapse(coord *big.Int, pressure quantum.PressureMetrics) string {
	
	entangled := sg.Substrate.GetEntanglements(coord)

	var bestWord string
	var bestAmp float64

	for _, e := range entangled {
		amp := cmplx.Abs(sg.Substrate.GetAmplitude(e))
		if amp > bestAmp && sg.Dictionary.WordExists(e) {
			word := quantum.UTF8_DECODE(e)
			if word != "" && len(word) > 1 {
				bestWord = strings.ToLower(word)
				bestAmp = amp
			}
		}
	}

	return bestWord
}


func (sg *SpeechGenerator) hashToWord(coord *big.Int, pressure quantum.PressureMetrics) string {
	
	allWords := sg.Dictionary.GetAllWords()
	if len(allWords) == 0 {
		return ""
	}

	
	targetPOS := sg.selectPOS(pressure, coord)

	
	hash := quantum.FeistelHash(coord)
	candidates := sg.filterWordsByPOS(allWords, targetPOS)

	if len(candidates) == 0 {
		candidates = allWords
	}

	idx := new(big.Int).Mod(hash, big.NewInt(int64(len(candidates)))).Int64()
	return candidates[idx]
}


func (sg *SpeechGenerator) selectPOS(pressure quantum.PressureMetrics, coord *big.Int) string {
	
	posSelector := new(big.Int).Mod(coord, big.NewInt(100)).Int64()

	
	if pressure.Magnitude > 0.6 && posSelector < 50 {
		return "v"
	}

	
	if pressure.Tension > 0.5 && posSelector < 40 {
		return "a"
	}

	
	if pressure.Density > 0.5 {
		return "n"
	}

	
	switch posSelector % 4 {
	case 0:
		return "n"
	case 1:
		return "v"
	case 2:
		return "a"
	default:
		return "r" 
	}
}


func (sg *SpeechGenerator) filterWordsByPOS(words []string, targetPOS string) []string {
	filtered := make([]string, 0)

	for _, word := range words {
		coord := quantum.UTF8_ENCODE(sg.Substrate, word)
		pos := sg.Dictionary.GetPartOfSpeech(coord)
		if pos == targetPOS {
			filtered = append(filtered, word)
		}

		
		if len(filtered) >= 1000 {
			break
		}
	}

	return filtered
}


func (sg *SpeechGenerator) traverseEdge(current *big.Int, pressure quantum.PressureMetrics, step int) *big.Int {
	
	entangled := sg.Substrate.GetEntanglements(current)

	if len(entangled) > 0 {
		
		selectedEdge := sg.selectEdgeByPhase(entangled, pressure, step)
		if selectedEdge != nil {
			return selectedEdge
		}
	}

	
	salted := new(big.Int).Add(current, big.NewInt(int64(step*17)))
	return quantum.FeistelHash(salted)
}


func (sg *SpeechGenerator) selectEdgeByPhase(edges []*big.Int, pressure quantum.PressureMetrics, step int) *big.Int {
	if len(edges) == 0 {
		return nil
	}

	
	targetPhase := pressure.Coherence*math.Pi + pressure.Tension*math.Pi/2

	var bestEdge *big.Int
	var bestScore float64 = -1

	for i, edge := range edges {
		phase := sg.Substrate.GetPhase(edge)
		edgePhase := cmplx.Phase(phase)

		
		phaseDiff := math.Abs(edgePhase - targetPhase)
		if phaseDiff > math.Pi {
			phaseDiff = 2*math.Pi - phaseDiff
		}

		
		score := 1.0 - phaseDiff/math.Pi

		
		amp := cmplx.Abs(sg.Substrate.GetAmplitude(edge))
		score *= (1.0 + amp)

		
		jitter := float64((step*17+i)%100) / 1000.0
		score += jitter

		if score > bestScore {
			bestScore = score
			bestEdge = edge
		}
	}

	return bestEdge
}


func (sg *SpeechGenerator) GenerateFromQuantum(coord *big.Int, pressure quantum.PressureMetrics) string {
	if sg.Dictionary == nil {
		return ""
	}

	
	if sg.Dictionary.WordExists(coord) {
		def := sg.Dictionary.GetDefinition(coord)
		if def != "" && len(def) < 100 {
			
			if len(def) > 0 {
				def = strings.ToUpper(string(def[0])) + def[1:]
			}
			if !strings.HasSuffix(def, ".") {
				def += "."
			}
			return def
		}
	}

	
	return sg.GenerateSpeech(pressure, coord.Bytes())
}


func (sg *SpeechGenerator) EnhanceWithSynonyms(baseMessage string, seed int64) string {
	words := strings.Fields(baseMessage)
	enhanced := make([]string, 0, len(words))

	for i, word := range words {
		
		if len(word) > 4 {
			coord := quantum.UTF8_ENCODE(sg.Substrate, strings.ToLower(word))
			synonyms := sg.Dictionary.GetSynonyms(coord)

			if len(synonyms) > 0 {
				
				idx := (int(seed) + i) % len(synonyms)
				enhanced = append(enhanced, synonyms[idx])
				continue
			}
		}
		enhanced = append(enhanced, word)
	}

	return strings.Join(enhanced, " ")
}
