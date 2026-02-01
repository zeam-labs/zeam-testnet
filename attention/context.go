package attention

import (
	"strings"

	"zeam/embed"
	"zeam/ngac"
)

type ConceptEntry struct {
	Word      string
	SynsetID  string
	Embedding []float64
	TurnIndex int
	Weight    float64
}

type ContextWindow struct {
	Entries    []*ConceptEntry
	MaxLen     int
	Embeddings *embed.EmbeddingIndex
	DecayRate  float64
	turnCount  int
}

func NewContextWindow(maxLen int, embeddings *embed.EmbeddingIndex) *ContextWindow {
	return &ContextWindow{
		MaxLen:     maxLen,
		Embeddings: embeddings,
		DecayRate:  0.9,
	}
}

func (cw *ContextWindow) AddTurn(text string, dict *ngac.SubstrateDictionary) {
	cw.turnCount++
	words := extractContentWords(text)
	cw.AddConcepts(words)
}

func (cw *ContextWindow) AddConcepts(words []string) {
	cw.turnCount++
	for _, w := range words {
		w = strings.ToLower(w)
		emb := cw.Embeddings.GetByWord(w)
		if emb == nil {
			continue
		}
		entry := &ConceptEntry{
			Word:      w,
			SynsetID:  emb.SynsetID,
			Embedding: emb.Vector,
			TurnIndex: cw.turnCount,
			Weight:    1.0,
		}
		cw.Entries = append(cw.Entries, entry)
	}

	if len(cw.Entries) > cw.MaxLen {
		cw.Entries = cw.Entries[len(cw.Entries)-cw.MaxLen:]
	}
}

func (cw *ContextWindow) GetContextVector() []float64 {
	if len(cw.Entries) == 0 {
		return make([]float64, embed.Dimension)
	}

	result := make([]float64, embed.Dimension)
	totalWeight := 0.0

	for _, entry := range cw.Entries {

		turnAge := cw.turnCount - entry.TurnIndex
		decay := 1.0
		for i := 0; i < turnAge; i++ {
			decay *= cw.DecayRate
		}
		w := entry.Weight * decay
		totalWeight += w
		for j, v := range entry.Embedding {
			if j < len(result) {
				result[j] += v * w
			}
		}
	}

	if totalWeight > 0 {
		for j := range result {
			result[j] /= totalWeight
		}
	}
	return result
}

func (cw *ContextWindow) Clear() {
	cw.Entries = nil
	cw.turnCount = 0
}

func (cw *ContextWindow) Len() int {
	return len(cw.Entries)
}

func extractContentWords(text string) []string {
	words := strings.Fields(strings.ToLower(text))
	var out []string
	for _, w := range words {

		w = strings.Trim(w, ".,!?;:'\"()-")
		if len(w) < 3 {
			continue
		}
		if isStopWord(w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "had": true,
	"her": true, "was": true, "one": true, "our": true, "out": true,
	"has": true, "have": true, "from": true, "this": true, "that": true,
	"with": true, "they": true, "been": true, "said": true, "each": true,
	"which": true, "their": true, "will": true, "other": true, "about": true,
	"many": true, "then": true, "them": true, "these": true, "some": true,
	"would": true, "into": true, "more": true, "what": true, "when": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}
