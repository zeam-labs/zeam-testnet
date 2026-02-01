package quantum

import (
	"math/big"
	"sort"
	"sync"
)

type WordDecoder struct {

	wordToCoord map[string]*big.Int

	coordToWord map[string]string

	rankedWords []string

	substrate *SubstrateChain

	mu sync.RWMutex
}

func NewWordDecoder(substrate *SubstrateChain, words []string) *WordDecoder {
	wd := &WordDecoder{
		wordToCoord: make(map[string]*big.Int),
		coordToWord: make(map[string]string),
		rankedWords: words,
		substrate:   substrate,
	}

	for _, word := range words {
		coord := UTF8_ENCODE(substrate, word)
		wd.wordToCoord[word] = coord

		key := coordKey(coord)
		wd.coordToWord[key] = word
	}

	return wd
}

func coordKey(coord *big.Int) string {
	if coord == nil {
		return ""
	}
	bytes := coord.Bytes()
	if len(bytes) > 8 {
		bytes = bytes[:8]
	}

	padded := make([]byte, 8)
	copy(padded[8-len(bytes):], bytes)
	return string(padded)
}

func (wd *WordDecoder) DecodeCoordinate(coord *big.Int) string {
	if coord == nil {
		return ""
	}

	wd.mu.RLock()
	defer wd.mu.RUnlock()

	key := coordKey(coord)
	if word, ok := wd.coordToWord[key]; ok {
		return word
	}

	return wd.findClosestWord(coord)
}

func (wd *WordDecoder) findClosestWord(target *big.Int) string {
	if len(wd.rankedWords) == 0 {
		return ""
	}

	idx := new(big.Int).Mod(target, big.NewInt(int64(len(wd.rankedWords)))).Int64()
	return wd.rankedWords[idx]
}

func (wd *WordDecoder) DecodeCoordinateWithPOS(coord *big.Int, targetPOS string, posLookup func(string) string) string {
	if coord == nil || len(wd.rankedWords) == 0 {
		return ""
	}

	wd.mu.RLock()
	defer wd.mu.RUnlock()

	startIdx := new(big.Int).Mod(coord, big.NewInt(int64(len(wd.rankedWords)))).Int64()

	for offset := 0; offset < len(wd.rankedWords); offset++ {
		idx := (int(startIdx) + offset) % len(wd.rankedWords)
		word := wd.rankedWords[idx]

		if posLookup != nil {
			pos := posLookup(word)
			if pos == targetPOS || targetPOS == "" {
				return word
			}
		} else {
			return word
		}
	}

	return wd.rankedWords[startIdx]
}

func (wd *WordDecoder) DecodeWithPressure(coord *big.Int, pressure PressureMetrics, posLookup func(string) string) string {
	if coord == nil || len(wd.rankedWords) == 0 {
		return ""
	}

	var targetPOS string
	if pressure.Hadamard > 0.6 {
		targetPOS = "v"
	} else if pressure.PauliZ > 0.5 {
		targetPOS = "a"
	} else if pressure.Phase > 0.5 {
		targetPOS = "n"
	}

	return wd.DecodeCoordinateWithPOS(coord, targetPOS, posLookup)
}

func (wd *WordDecoder) EncodeWord(word string) *big.Int {
	wd.mu.RLock()
	defer wd.mu.RUnlock()

	if coord, ok := wd.wordToCoord[word]; ok {
		return new(big.Int).Set(coord)
	}

	return UTF8_ENCODE(wd.substrate, word)
}

func (wd *WordDecoder) VocabSize() int {
	wd.mu.RLock()
	defer wd.mu.RUnlock()
	return len(wd.rankedWords)
}

func (wd *WordDecoder) GetSubstrate() *SubstrateChain {
	return wd.substrate
}

type SemanticDecoder struct {
	*WordDecoder

	entanglementGroups map[string][]string

	wordToSynset map[string]string
}

func NewSemanticDecoder(substrate *SubstrateChain, words []string, synonymGroups map[string][]string) *SemanticDecoder {
	sd := &SemanticDecoder{
		WordDecoder:        NewWordDecoder(substrate, words),
		entanglementGroups: make(map[string][]string),
		wordToSynset:       make(map[string]string),
	}

	for synsetID, synonyms := range synonymGroups {
		if len(synonyms) == 0 {
			continue
		}

		groupCoord := UTF8_ENCODE(substrate, synonyms[0])
		groupKey := coordKey(groupCoord)

		for _, word := range synonyms {
			wordCoord := UTF8_ENCODE(substrate, word)
			wordKey := coordKey(wordCoord)

			sd.entanglementGroups[groupKey] = append(sd.entanglementGroups[groupKey], wordKey)
			sd.wordToSynset[word] = synsetID

			COMPOSE(substrate, groupCoord, wordCoord)
		}
	}

	return sd
}

func (sd *SemanticDecoder) DecodeWithEntanglement(coord *big.Int, pressure PressureMetrics) string {
	if coord == nil {
		return ""
	}

	sd.mu.RLock()
	defer sd.mu.RUnlock()

	key := coordKey(coord)
	if group, ok := sd.entanglementGroups[key]; ok && len(group) > 0 {

		idx := int(pressure.Hadamard * float64(len(group)))
		if idx >= len(group) {
			idx = len(group) - 1
		}

		targetKey := group[idx]
		if word, ok := sd.coordToWord[targetKey]; ok {
			return word
		}
	}

	return sd.DecodeCoordinate(coord)
}

type ContextDecoder struct {
	*SemanticDecoder

	contextWords []string
	contextSet   map[string]bool
}

func NewContextDecoder(semantic *SemanticDecoder) *ContextDecoder {
	return &ContextDecoder{
		SemanticDecoder: semantic,
		contextSet:      make(map[string]bool),
	}
}

func (cd *ContextDecoder) SetContext(promptWords []string) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	cd.contextWords = promptWords
	cd.contextSet = make(map[string]bool)
	for _, w := range promptWords {
		cd.contextSet[w] = true
	}
}

func (cd *ContextDecoder) DecodeInContext(coord *big.Int, pressure PressureMetrics) string {
	if coord == nil || len(cd.rankedWords) == 0 {
		return ""
	}

	cd.mu.RLock()
	defer cd.mu.RUnlock()

	if pressure.PauliX > 0.6 && len(cd.contextWords) > 0 {

		for _, contextWord := range cd.contextWords {
			if synsetID, ok := cd.wordToSynset[contextWord]; ok {

				for word, sid := range cd.wordToSynset {
					if sid == synsetID && word != contextWord {

						wordCoord := cd.EncodeWord(word)
						dist := new(big.Int).Sub(coord, wordCoord)
						dist.Abs(dist)

						threshold := big.NewInt(int64(1000 / (pressure.PauliX + 0.1)))
						if dist.Cmp(threshold) < 0 {
							return word
						}
					}
				}
			}
		}
	}

	return cd.DecodeWithEntanglement(coord, pressure)
}

func BuildDecoderFromNGAC(substrate *SubstrateChain, words []string, synonymGroups map[string][]string) *ContextDecoder {

	sortedWords := make([]string, len(words))
	copy(sortedWords, words)
	sort.Slice(sortedWords, func(i, j int) bool {
		return len(sortedWords[i]) < len(sortedWords[j])
	})

	semantic := NewSemanticDecoder(substrate, sortedWords, synonymGroups)
	return NewContextDecoder(semantic)
}

func DecodeCoordinates(decoder *ContextDecoder, coords []*big.Int, pressure PressureMetrics, promptWords []string) []string {
	if decoder == nil || len(coords) == 0 {
		return nil
	}

	decoder.SetContext(promptWords)

	words := make([]string, 0, len(coords))
	for _, coord := range coords {
		word := decoder.DecodeInContext(coord, pressure)
		if word != "" {
			words = append(words, word)
		}

		pressure.PauliX *= 0.95
		pressure.PauliZ *= 0.9
	}

	return words
}
