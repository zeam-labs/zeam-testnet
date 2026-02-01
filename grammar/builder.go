package grammar

import (
	"math/big"
	"sort"
	"strings"
)

type WordCandidate struct {
	Word       string
	Lemma      string
	POS        PartOfSpeech
	Score      float64
	SynsetID   string
	Definition string
	Frequency  int
}

type SentenceBuilder struct {

	GetWordsByPOS    func(pos string) []string
	GetSynsets       func(word string) []string
	GetSynsetByID    func(id string) *SynsetInfo
	GetDefinition    func(word string) string
	GetPOS           func(word string) string
	GetSynonyms      func(word string) []string
	GetRelatedWords  func(word string, relation string) []string

	HashFunc func(input *big.Int) *big.Int
}

type SynsetInfo struct {
	ID          string
	Type        string
	Members     []string
	Definitions []string
	Domain      string
}

type PressureContext struct {
	Hadamard float64
	PauliX   float64
	PauliZ   float64
	Phase    float64
}

func NewSentenceBuilder() *SentenceBuilder {
	return &SentenceBuilder{}
}

func (sb *SentenceBuilder) BuildResponse(
	parsed *ParsedSentence,
	concepts []string,
	pressure PressureContext,
	hashSeed *big.Int,
) string {
	if parsed == nil || len(concepts) == 0 {
		return ""
	}

	pattern := sb.selectPattern(parsed)

	sentence := NewSentence(pattern)

	sb.fillSlots(sentence, parsed, concepts, pressure, hashSeed)

	return sentence.Build()
}

func (sb *SentenceBuilder) selectPattern(parsed *ParsedSentence) *SentencePattern {
	switch parsed.Intent {
	case "greeting":
		return PatternGreeting
	case "explanation":
		return PatternExplanation
	case "command":

		return PatternDeclarativeSVO
	case "question":

		return PatternDeclarativeSVO
	case "definition":
		return PatternDefinition
	default:
		return PatternDeclarativeSVO
	}
}

func (sb *SentenceBuilder) fillSlots(
	sentence *Sentence,
	parsed *ParsedSentence,
	concepts []string,
	pressure PressureContext,
	hashSeed *big.Int,
) {

	usedWords := make(map[string]bool)

	topics := parsed.ExtractTopicConcepts()

	allConcepts := make([]string, 0, len(topics)+len(concepts))
	seen := make(map[string]bool)
	for _, c := range topics {
		if !seen[c] {
			allConcepts = append(allConcepts, c)
			seen[c] = true
		}
	}
	for _, c := range concepts {
		if !seen[c] {
			allConcepts = append(allConcepts, c)
			seen[c] = true
		}
	}

	currentHash := hashSeed
	if currentHash == nil {
		currentHash = big.NewInt(42)
	}

	for _, slot := range sentence.Pattern.Slots {

		if slot.FixedWord != "" {
			sentence.FillSlot(slot.Name, slot.FixedWord)
			continue
		}

		candidates := sb.getCandidates(slot, allConcepts, usedWords, pressure)

		if len(candidates) == 0 {

			candidates = sb.getFallbackCandidates(slot, pressure)
		}

		if len(candidates) == 0 {
			continue
		}

		selectedIdx := sb.selectByHash(candidates, currentHash, pressure)
		selected := candidates[selectedIdx]

		var modifiers []string
		if slot.Modifiable && pressure.Phase > 0.5 {
			modifiers = sb.getModifiers(selected, slot.POS, allConcepts, usedWords, pressure)
		}

		sentence.FillSlot(slot.Name, selected.Word, modifiers...)
		usedWords[selected.Word] = true

		if sb.HashFunc != nil {
			currentHash = sb.HashFunc(currentHash)
		} else {
			currentHash = new(big.Int).Add(currentHash, big.NewInt(17))
		}
	}
}

func (sb *SentenceBuilder) getCandidates(
	slot GrammarSlot,
	concepts []string,
	usedWords map[string]bool,
	pressure PressureContext,
) []WordCandidate {
	candidates := make([]WordCandidate, 0)

	for _, concept := range concepts {
		related := sb.findRelatedWords(concept, string(slot.POS))
		for _, word := range related {
			if usedWords[word] {
				continue
			}
			score := sb.scoreCandidate(word, concepts, pressure)
			candidates = append(candidates, WordCandidate{
				Word:  word,
				Lemma: strings.ToLower(word),
				POS:   slot.POS,
				Score: score,
			})
		}
	}

	if len(candidates) < 5 && sb.GetWordsByPOS != nil {
		posWords := sb.GetWordsByPOS(string(slot.POS))
		for _, word := range posWords {
			if usedWords[word] {
				continue
			}

			score := sb.scoreCandidate(word, concepts, pressure)
			if score > 0.2 || len(candidates) < 3 {
				candidates = append(candidates, WordCandidate{
					Word:  word,
					Lemma: strings.ToLower(word),
					POS:   slot.POS,
					Score: score,
				})
			}
			if len(candidates) >= 20 {
				break
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > 10 {
		candidates = candidates[:10]
	}

	return candidates
}

func (sb *SentenceBuilder) findRelatedWords(concept string, targetPOS string) []string {
	related := make([]string, 0)

	if sb.GetSynonyms != nil {
		synonyms := sb.GetSynonyms(concept)
		for _, syn := range synonyms {
			if sb.GetPOS != nil {
				pos := sb.GetPOS(syn)
				if pos == targetPOS {
					related = append(related, syn)
				}
			} else {
				related = append(related, syn)
			}
		}
	}

	if sb.GetSynsets != nil && sb.GetSynsetByID != nil {
		synsetIDs := sb.GetSynsets(concept)
		for _, id := range synsetIDs {
			synset := sb.GetSynsetByID(id)
			if synset != nil && synset.Type == targetPOS {
				related = append(related, synset.Members...)
			}
		}
	}

	if sb.GetRelatedWords != nil {

		if targetPOS == "n" {
			related = append(related, sb.GetRelatedWords(concept, "hypernym")...)
			related = append(related, sb.GetRelatedWords(concept, "hyponym")...)
		}

		if targetPOS == "v" {
			related = append(related, sb.GetRelatedWords(concept, "entails")...)
		}
	}

	return related
}

func (sb *SentenceBuilder) scoreCandidate(word string, concepts []string, pressure PressureContext) float64 {
	score := 0.1

	for _, c := range concepts {
		if strings.EqualFold(word, c) {
			score += 0.5
			break
		}
	}

	if sb.GetDefinition != nil {
		wordDef := sb.GetDefinition(word)
		for _, c := range concepts {
			if strings.Contains(strings.ToLower(wordDef), strings.ToLower(c)) {
				score += 0.3
				break
			}
		}
	}

	if pressure.PauliX > 0.6 {

		if len(word) < 8 {
			score += 0.1 * pressure.PauliX
		}
	}

	if pressure.PauliZ > 0.6 {
		score += 0.1 * pressure.PauliZ
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

func (sb *SentenceBuilder) getFallbackCandidates(slot GrammarSlot, pressure PressureContext) []WordCandidate {
	candidates := make([]WordCandidate, 0)

	fallbacks := map[PartOfSpeech][]string{
		POS_Noun:      {"concept", "thing", "idea", "element", "aspect", "matter", "subject", "topic", "point"},
		POS_Verb:      {"involves", "concerns", "relates", "indicates", "represents", "includes", "shows", "means"},
		POS_Adjective: {"relevant", "significant", "important", "notable", "related", "key", "main", "primary"},
		POS_Adverb:    {"essentially", "generally", "typically", "particularly", "specifically", "notably"},
	}

	if words, ok := fallbacks[slot.POS]; ok {
		for _, word := range words {
			candidates = append(candidates, WordCandidate{
				Word:  word,
				Lemma: word,
				POS:   slot.POS,
				Score: 0.3,
			})
		}
	}

	return candidates
}

func (sb *SentenceBuilder) selectByHash(candidates []WordCandidate, hash *big.Int, pressure PressureContext) int {
	if len(candidates) == 0 {
		return 0
	}

	totalWeight := 0.0
	for _, c := range candidates {
		totalWeight += c.Score
	}

	hashVal := hash.Int64()
	if hashVal < 0 {
		hashVal = -hashVal
	}

	selectionRange := len(candidates)
	if pressure.PauliX > 0.7 && len(candidates) > 3 {
		selectionRange = 3
	}

	idx := int(hashVal) % selectionRange
	return idx
}

func (sb *SentenceBuilder) getModifiers(
	word WordCandidate,
	targetPOS PartOfSpeech,
	concepts []string,
	usedWords map[string]bool,
	pressure PressureContext,
) []string {
	modifiers := make([]string, 0)

	var modifierPOS string
	if targetPOS == POS_Noun {
		modifierPOS = "a"
	} else if targetPOS == POS_Verb {
		modifierPOS = "r"
	} else {
		return modifiers
	}

	for _, concept := range concepts {
		if usedWords[concept] {
			continue
		}

		if sb.GetPOS != nil {
			pos := sb.GetPOS(concept)
			if pos == modifierPOS {
				modifiers = append(modifiers, concept)
				if len(modifiers) >= 2 {
					break
				}
			}
		}
	}

	if len(modifiers) > 1 {
		modifiers = modifiers[:1]
	}

	return modifiers
}

func (sb *SentenceBuilder) GenerateMultiSentence(
	parsed *ParsedSentence,
	concepts []string,
	pressure PressureContext,
	hashSeed *big.Int,
	maxSentences int,
) string {
	if maxSentences < 1 {
		maxSentences = 1
	}
	if maxSentences > 3 {
		maxSentences = 3
	}

	sentences := make([]string, 0, maxSentences)
	usedConcepts := make(map[string]bool)
	currentHash := hashSeed

	for i := 0; i < maxSentences && len(concepts) > 0; i++ {

		remainingConcepts := make([]string, 0)
		for _, c := range concepts {
			if !usedConcepts[c] {
				remainingConcepts = append(remainingConcepts, c)
			}
		}

		if len(remainingConcepts) == 0 {
			break
		}

		s := sb.BuildResponse(parsed, remainingConcepts, pressure, currentHash)
		if s != "" {
			sentences = append(sentences, s)

			for _, c := range remainingConcepts[:min(3, len(remainingConcepts))] {
				usedConcepts[c] = true
			}
		}

		if sb.HashFunc != nil {
			currentHash = sb.HashFunc(currentHash)
		} else {
			currentHash = new(big.Int).Add(currentHash, big.NewInt(97))
		}
	}

	return strings.Join(sentences, " ")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type ResponseType int

const (
	ResponseStatement ResponseType = iota
	ResponseExplanation
	ResponseGreeting
	ResponseAcknowledgment
)

func GetResponseType(parsed *ParsedSentence) ResponseType {
	switch parsed.Intent {
	case "greeting":
		return ResponseGreeting
	case "explanation", "question":
		return ResponseExplanation
	case "acknowledgment":
		return ResponseAcknowledgment
	default:
		return ResponseStatement
	}
}

var simpleTemplates = map[string][]string{
	"greeting": {
		"Hello.",
		"Greetings.",
		"Hello there.",
	},
	"acknowledgment": {
		"I understand.",
		"I see.",
		"That is noted.",
	},
}

func GetSimpleResponse(intent string, hashSeed *big.Int) string {
	templates, ok := simpleTemplates[intent]
	if !ok || len(templates) == 0 {
		return ""
	}

	idx := 0
	if hashSeed != nil {
		idx = int(hashSeed.Int64()) % len(templates)
		if idx < 0 {
			idx = -idx
		}
	}

	return templates[idx]
}
