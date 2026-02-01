package attention

import (
	"math"
	"sort"

	"zeam/embed"
	"zeam/quantum"
)

type AttentionConfig struct {
	Temperature float64
	TopK        int
}

func DefaultAttentionConfig() *AttentionConfig {
	return &AttentionConfig{
		Temperature: 1.0,
		TopK:        20,
	}
}

type AttentionResult struct {
	Scores     map[string]float64
	TopK       []*ScoredCandidate
	Confidence float64
}

type ScoredCandidate struct {
	Embedding *embed.SynsetEmbedding
	Score     float64
}

type AttentionHead struct {
	Config *AttentionConfig
}

func NewAttentionHead(config *AttentionConfig) *AttentionHead {
	if config == nil {
		config = DefaultAttentionConfig()
	}
	return &AttentionHead{Config: config}
}

func (h *AttentionHead) Attend(
	query []float64,
	candidates []*embed.SynsetEmbedding,
	pressure quantum.PressureMetrics,
) *AttentionResult {
	if len(candidates) == 0 || len(query) == 0 {
		return &AttentionResult{
			Scores: make(map[string]float64),
		}
	}

	dim := float64(len(query))
	scale := math.Sqrt(dim)

	type scored struct {
		emb   *embed.SynsetEmbedding
		score float64
	}
	all := make([]scored, 0, len(candidates))

	for _, c := range candidates {
		dot := embed.DotProduct(query, c.Vector)
		s := dot / scale

		s *= (0.5 + pressure.Hadamard*0.5)

		all = append(all, scored{emb: c, score: s})
	}

	temp := h.Config.Temperature
	if pressure.PauliX > 0.7 {
		temp *= 0.5
	}
	if pressure.PauliZ > 0.5 {
		temp *= 2.0
	}

	maxScore := math.Inf(-1)
	for _, s := range all {
		if s.score > maxScore {
			maxScore = s.score
		}
	}

	sumExp := 0.0
	for i := range all {
		all[i].score = math.Exp((all[i].score - maxScore) / temp)
		sumExp += all[i].score
	}
	if sumExp > 0 {
		for i := range all {
			all[i].score /= sumExp
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].score > all[j].score
	})

	topK := h.Config.TopK
	if topK > len(all) {
		topK = len(all)
	}

	result := &AttentionResult{
		Scores: make(map[string]float64, len(all)),
		TopK:   make([]*ScoredCandidate, topK),
	}

	for i, s := range all {
		result.Scores[s.emb.SynsetID] = s.score
		if i < topK {
			result.TopK[i] = &ScoredCandidate{
				Embedding: s.emb,
				Score:     s.score,
			}
		}
	}

	if len(result.TopK) > 0 {
		result.Confidence = result.TopK[0].Score
	}

	return result
}
