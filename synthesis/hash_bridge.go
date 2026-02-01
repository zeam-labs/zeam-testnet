package synthesis

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"

	"zeam/ngac"
	"zeam/quantum"

	"github.com/ethereum/go-ethereum/crypto"
)

type HashBridge struct {
	mu sync.RWMutex

	ngac *ngac.NGAC

	dict *ngac.SubstrateDictionary

	network *quantum.HashNetwork

	synsetCounts map[string]int

	synsetIDs map[string][]string

	pressure quantum.PressureMetrics
}

func NewHashBridge(n *ngac.NGAC, network *quantum.HashNetwork) (*HashBridge, error) {
	if n == nil || n.Dictionary == nil {
		return nil, fmt.Errorf("NGAC instance with dictionary required")
	}

	bridge := &HashBridge{
		ngac:         n,
		dict:         n.Dictionary,
		network:      network,
		synsetCounts: make(map[string]int),
		synsetIDs:    make(map[string][]string),
		pressure: quantum.PressureMetrics{
			Hadamard: 0.5,
			PauliX: 0.5,
			PauliZ:   0.3,
			Phase:   0.5,
		},
	}

	if err := bridge.indexSynsets(); err != nil {
		return nil, fmt.Errorf("failed to index synsets: %w", err)
	}

	return bridge, nil
}

func (b *HashBridge) indexSynsets() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for synsetID, synset := range b.dict.SynsetIndex {
		cat := synset.Type
		if cat == "" {
			cat = "n"
		}
		b.synsetIDs[cat] = append(b.synsetIDs[cat], synsetID)
	}

	for cat, ids := range b.synsetIDs {
		b.synsetCounts[cat] = len(ids)
	}

	fmt.Printf("[HASH-BRIDGE] Indexed synsets: n=%d v=%d a=%d r=%d\n",
		b.synsetCounts["n"], b.synsetCounts["v"],
		b.synsetCounts["a"], b.synsetCounts["r"])

	return nil
}

func (b *HashBridge) BuildBoundsFromConcepts(concepts []string) *quantum.SemanticBounds {
	b.mu.RLock()
	defer b.mu.RUnlock()

	bounds := &quantum.SemanticBounds{
		ValidSynsets:    make(map[string]bool),
		ValidCategories: []string{"n", "v", "a", "r"},
		MinPauliX:    0.3,
		MaxPauliZ:      0.8,
		ConceptAnchors:  concepts,
	}

	for _, concept := range concepts {

		synsetIDs := b.dict.GetSynsetsForWord(strings.ToLower(concept))
		for _, synsetID := range synsetIDs {
			bounds.ValidSynsets[synsetID] = true

			synset := b.dict.GetSynsetByID(synsetID)
			if synset != nil {

				for _, member := range synset.Members {
					memberSynsets := b.dict.GetSynsetsForWord(member)
					for _, msID := range memberSynsets {
						bounds.ValidSynsets[msID] = true
					}
				}
			}
		}
	}

	if len(bounds.ValidSynsets) == 0 {
		bounds.ValidSynsets = nil
	}

	return bounds
}

func (b *HashBridge) BuildBoundsFromPressure(baseConcepts []string) *quantum.SemanticBounds {
	b.mu.RLock()
	pressure := b.pressure
	b.mu.RUnlock()

	bounds := b.BuildBoundsFromConcepts(baseConcepts)

	if pressure.PauliX > 0.7 {
		bounds.MinPauliX = 0.5
	}

	if pressure.PauliZ > 0.6 {
		bounds.MaxPauliZ = 0.9
	}

	if pressure.Hadamard > 0.7 {
		bounds.ValidCategories = []string{"v", "n", "a", "r"}
	}

	if pressure.Phase < 0.3 {
		bounds.ValidCategories = []string{"n"}
	}

	return bounds
}

func (b *HashBridge) ConceptToHash(concept string) []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()

	synsetIDs := b.dict.GetSynsetsForWord(strings.ToLower(concept))

	if len(synsetIDs) == 0 {

		return crypto.Keccak256([]byte(concept))
	}

	var hashInput []byte

	for _, synsetID := range synsetIDs {
		synset := b.dict.GetSynsetByID(synsetID)
		if synset == nil {
			continue
		}

		hashInput = append(hashInput, []byte(synsetID)...)

		hashInput = append(hashInput, categoryToByte(synset.Type))

		if len(synset.Definitions) > 0 {
			defHash := sha256.Sum256([]byte(synset.Definitions[0]))
			hashInput = append(hashInput, defHash[:8]...)
		}

		if synset.ILI != "" {
			hashInput = append(hashInput, []byte(synset.ILI)...)
		}
	}

	return crypto.Keccak256(hashInput)
}

func (b *HashBridge) DecodeHashToWord(hash []byte, bounds *quantum.SemanticBounds) (string, float64) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(hash) < 16 {
		return "", 0
	}

	categories := []string{"n", "v", "a", "r"}
	if bounds != nil && len(bounds.ValidCategories) > 0 {
		categories = bounds.ValidCategories
	}
	category := categories[int(hash[0])%len(categories)]

	synsetList := b.synsetIDs[category]
	if len(synsetList) == 0 {
		return "", 0
	}

	synsetIdx := binary.BigEndian.Uint32(hash[2:6]) % uint32(len(synsetList))

	synsetID := synsetList[synsetIdx]
	if bounds != nil && bounds.ValidSynsets != nil {

		synsetID = b.findValidSynset(synsetList, int(synsetIdx), bounds.ValidSynsets)
		if synsetID == "" {

			if len(bounds.ConceptAnchors) > 0 {
				return bounds.ConceptAnchors[int(hash[1])%len(bounds.ConceptAnchors)], 0.5
			}
			return "", 0
		}
	}

	synset := b.dict.GetSynsetByID(synsetID)
	if synset == nil || len(synset.Members) == 0 {
		return "", 0
	}

	wordIdx := binary.BigEndian.Uint16(hash[6:8]) % uint16(len(synset.Members))
	word := synset.Members[wordIdx]

	confidence := hashEntropy(hash[8:16]) / 8.0

	return word, confidence
}

func (b *HashBridge) findValidSynset(synsetList []string, startIdx int, validSynsets map[string]bool) string {

	for offset := 0; offset < len(synsetList); offset++ {

		fwdIdx := (startIdx + offset) % len(synsetList)
		if validSynsets[synsetList[fwdIdx]] {
			return synsetList[fwdIdx]
		}

		bwdIdx := (startIdx - offset + len(synsetList)) % len(synsetList)
		if validSynsets[synsetList[bwdIdx]] {
			return synsetList[bwdIdx]
		}
	}
	return ""
}

func (b *HashBridge) Forward(concepts []string) ([]string, error) {

	bounds := b.BuildBoundsFromPressure(concepts)

	b.network.SetBounds(bounds)
	b.network.SetPressure(b.pressure)

	indices, err := b.network.Forward(concepts)
	if err != nil {
		return nil, fmt.Errorf("network forward failed: %w", err)
	}

	words := make([]string, 0, len(indices))
	for _, idx := range indices {
		word := b.IndexToWord(idx, bounds)
		if word != "" {
			words = append(words, word)
		}
	}

	return words, nil
}

func (b *HashBridge) IndexToWord(idx quantum.SemanticIndex, bounds *quantum.SemanticBounds) string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	synsetList := b.synsetIDs[idx.Category]
	if len(synsetList) == 0 {
		return ""
	}

	synsetIdx := idx.SynsetIndex % uint32(len(synsetList))

	synsetID := synsetList[synsetIdx]
	if bounds != nil && bounds.ValidSynsets != nil {
		bounded := b.findValidSynset(synsetList, int(synsetIdx), bounds.ValidSynsets)
		if bounded != "" {
			synsetID = bounded
		}
	}

	synset := b.dict.GetSynsetByID(synsetID)
	if synset == nil || len(synset.Members) == 0 {
		return ""
	}

	wordIdx := idx.WordIndex % uint16(len(synset.Members))
	return synset.Members[wordIdx]
}

func (b *HashBridge) SetPressure(p quantum.PressureMetrics) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pressure = p
	if b.network != nil {
		b.network.SetPressure(p)
	}
}

func (b *HashBridge) GetPressure() quantum.PressureMetrics {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pressure
}

func (b *HashBridge) Generate(input string, maxWords int) (string, error) {

	concepts := b.extractConcepts(input)

	if len(concepts) == 0 {

		concepts = b.intentSeeds(input)
	}

	words, err := b.Forward(concepts)
	if err != nil {
		return "", err
	}

	if len(words) > maxWords {
		words = words[:maxWords]
	}

	response := assembleCoherent(words, b.pressure)

	return response, nil
}

func (b *HashBridge) extractConcepts(input string) []string {
	words := strings.Fields(strings.ToLower(input))
	concepts := make([]string, 0)

	for _, word := range words {
		word = strings.Trim(word, ".,?!;:'\"()[]")
		if len(word) < 3 {
			continue
		}

		synsets := b.dict.GetSynsetsForWord(word)
		if len(synsets) > 0 && !isHashStopWord(word) {
			concepts = append(concepts, word)
		}
	}

	return concepts
}

func (b *HashBridge) intentSeeds(input string) []string {
	lower := strings.ToLower(input)

	if strings.HasSuffix(input, "?") {
		return []string{"question", "inquiry", "understand"}
	}
	if strings.HasPrefix(lower, "hello") || strings.HasPrefix(lower, "hi ") {
		return []string{"greeting", "welcome", "acknowledge"}
	}
	if strings.HasPrefix(lower, "explain") || strings.HasPrefix(lower, "describe") {
		return []string{"explain", "describe", "clarify"}
	}

	return []string{"consider", "observe", "note"}
}

func isHashStopWord(word string) bool {
	stops := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true,
		"i": true, "you": true, "he": true, "she": true, "it": true,
		"we": true, "they": true, "me": true, "him": true, "her": true,
		"us": true, "them": true, "my": true, "your": true, "his": true,
		"its": true, "our": true, "their": true,
		"this": true, "that": true, "these": true, "those": true,
		"what": true, "which": true, "who": true, "whom": true, "whose": true,
		"where": true, "when": true, "why": true, "how": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"else": true, "so": true, "because": true, "as": true, "until": true,
		"while": true, "of": true, "at": true, "by": true, "for": true,
		"with": true, "about": true, "against": true, "between": true,
		"into": true, "through": true, "during": true, "before": true,
		"after": true, "above": true, "below": true, "to": true, "from": true,
		"up": true, "down": true, "in": true, "out": true, "on": true,
		"off": true, "over": true, "under": true, "again": true, "further": true,
		"once": true, "here": true, "there": true,
		"all": true, "each": true, "few": true, "more": true, "most": true,
		"other": true, "some": true, "such": true, "no": true, "nor": true,
		"not": true, "only": true, "own": true, "same": true, "than": true,
		"too": true, "very": true, "just": true,
	}
	return stops[strings.ToLower(word)]
}

func assembleCoherent(words []string, pressure quantum.PressureMetrics) string {
	if len(words) == 0 {
		return ""
	}

	if pressure.PauliX > 0.7 && len(words) > 3 {
		words = words[:3]
	}

	result := strings.Join(words, " ")
	if pressure.Hadamard > 0.8 {
		result = strings.Title(result)
	}

	if len(result) > 0 {
		result = strings.ToUpper(result[:1]) + result[1:]
	}

	return result
}

func categoryToByte(cat string) byte {
	switch cat {
	case "n":
		return 0
	case "v":
		return 1
	case "a":
		return 2
	case "r":
		return 3
	default:
		return 0
	}
}

func hashEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}

	counts := make([]int, 256)
	for _, b := range data {
		counts[b]++
	}

	entropy := 0.0
	n := float64(len(data))
	for _, count := range counts {
		if count > 0 {
			p := float64(count) / n
			entropy -= p * logBase2(p)
		}
	}

	return entropy
}

func logBase2(x float64) float64 {
	if x <= 0 {
		return 0
	}
	result := 0.0
	for x >= 2 {
		x /= 2
		result++
	}
	for x < 1 && x > 0 {
		x *= 2
		result--
	}
	result += (x - 1)
	return result
}

type LiveHashDispatcher struct {
	dispatch func(payload []byte) ([][]byte, error)
}

func NewLiveHashDispatcher(fn func(payload []byte) ([][]byte, error)) *LiveHashDispatcher {
	return &LiveHashDispatcher{dispatch: fn}
}

func (d *LiveHashDispatcher) Dispatch(payload []byte) ([][]byte, error) {
	return d.dispatch(payload)
}

func (b *HashBridge) GetNetwork() *quantum.HashNetwork {
	return b.network
}

func (b *HashBridge) SetDispatcher(d quantum.HashDispatcher) {
	b.network.SetDispatcher(d)
}
