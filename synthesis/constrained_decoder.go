package synthesis

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"

	"zeam/attention"
	"zeam/embed"
	"zeam/grammar"
	"zeam/ngac"
	"zeam/quantum"
)

type ConstrainedDecoder struct {
	Embeddings *embed.EmbeddingIndex
	Context    *attention.ContextWindow
	Dict       *ngac.SubstrateDictionary
	Config     *ConstrainedDecoderConfig
}

type ConstrainedDecoderConfig struct {
	TopK          int
	MinConfidence float64
}

func DefaultConstrainedDecoderConfig() *ConstrainedDecoderConfig {
	return &ConstrainedDecoderConfig{
		TopK:          20,
		MinConfidence: 0.05,
	}
}

func posToEmbedByte(pos grammar.PartOfSpeech) byte {
	switch pos {
	case grammar.POS_Noun:
		return 'n'
	case grammar.POS_Verb:
		return 'v'
	case grammar.POS_Adjective:
		return 'a'
	case grammar.POS_Adverb:
		return 'r'
	default:
		return 'n'
	}
}

func (cd *ConstrainedDecoder) DecodePosition(
	slot grammar.GrammarSlot,
	pressure quantum.PressureMetrics,
	hashSeed []byte,
) (string, float64) {
	if cd.Config == nil {
		cd.Config = DefaultConstrainedDecoderConfig()
	}

	contextVec := cd.Context.GetContextVector()

	posByte := posToEmbedByte(slot.POS)
	candidates := cd.Embeddings.Candidates(posByte)
	if len(candidates) == 0 {
		return "", 0
	}

	head := attention.NewAttentionHead(&attention.AttentionConfig{
		Temperature: 1.0,
		TopK:        cd.Config.TopK,
	})
	result := head.Attend(contextVec, candidates, pressure)

	if len(result.TopK) == 0 {
		return "", 0
	}

	idx := hashSelectIndex(hashSeed, len(result.TopK))
	selected := result.TopK[idx]

	word := cd.synsetToWord(selected.Embedding)
	if word == "" {
		return "", 0
	}

	return word, selected.Score
}

func (cd *ConstrainedDecoder) GenerateConstrained(
	parsed *grammar.ParsedSentence,
	pressure quantum.PressureMetrics,
	hashSeed []byte,
) string {
	if cd.Config == nil {
		cd.Config = DefaultConstrainedDecoderConfig()
	}

	pattern := grammar.GetPatternForIntent(parsed.Intent)
	if pattern == nil {
		return ""
	}

	sent := &grammar.Sentence{Pattern: pattern}

	seed := hashSeed
	for _, slot := range pattern.Slots {
		if slot.FixedWord != "" {
			sent.FillSlot(slot.Name, slot.FixedWord)
		} else {
			word, _ := cd.DecodePosition(slot, pressure, seed)
			if word != "" {
				sent.FillSlot(slot.Name, word)

				cd.Context.AddConcepts([]string{word})
			}
		}

		seed = advanceSeed(seed)
	}

	return sent.Build()
}

func (cd *ConstrainedDecoder) synsetToWord(emb *embed.SynsetEmbedding) string {
	if cd.Dict == nil {
		return ""
	}
	synset, ok := cd.Dict.SynsetIndex[emb.SynsetID]
	if !ok || len(synset.Members) == 0 {
		return ""
	}
	return synset.Members[0]
}

func hashSelectIndex(seed []byte, n int) int {
	if n <= 0 {
		return 0
	}
	if n == 1 {
		return 0
	}
	h := sha256.Sum256(seed)
	val := binary.BigEndian.Uint64(h[:8])

	frac := float64(val) / float64(^uint64(0))
	frac = frac * frac
	idx := int(frac * float64(n))
	if idx >= n {
		idx = n - 1
	}
	return idx
}

func advanceSeed(seed []byte) []byte {
	h := sha256.Sum256(seed)
	return h[:]
}

func SeedFromBigInt(bi *big.Int) []byte {
	if bi == nil {
		return make([]byte, 32)
	}
	b := bi.Bytes()
	if len(b) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		return padded
	}
	return b[:32]
}
