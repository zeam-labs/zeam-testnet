package embed

import (
	"crypto/sha256"
	"math"
	"strings"

	"zeam/ngac"
)

func BuildEmbeddingIndex(dict *ngac.SubstrateDictionary) *EmbeddingIndex {
	idx := &EmbeddingIndex{
		BySynsetID: make(map[string]*SynsetEmbedding),
		POSIndex:   make(map[byte][]*SynsetEmbedding),
		ByWord:     make(map[string]*SynsetEmbedding),
	}

	if dict == nil {
		return idx
	}

	for id, synset := range dict.SynsetIndex {
		emb := buildSynsetEmbedding(synset)
		idx.BySynsetID[id] = emb
		idx.POSIndex[emb.POS] = append(idx.POSIndex[emb.POS], emb)
	}

	for key, synsetIDs := range dict.LemmaRanked {
		if len(synsetIDs) == 0 {
			continue
		}

		parts := strings.SplitN(key, ".", 2)
		if len(parts) != 2 {
			continue
		}
		word := parts[1]
		if _, exists := idx.ByWord[word]; exists {
			continue
		}
		if emb, ok := idx.BySynsetID[synsetIDs[0]]; ok {
			idx.ByWord[word] = emb
		}
	}

	return idx
}

func buildSynsetEmbedding(synset *ngac.Synset) *SynsetEmbedding {
	vec := make([]float64, Dimension)

	domainHash := sha256.Sum256([]byte(synset.Domain))
	for i := 0; i < 8; i++ {
		vec[i] = hashByteToFloat(domainHash[i])
	}

	pos := synsetTypeToPOS(synset.Type)

	switch pos {
	case 'n':
		vec[8] = 1.0
	case 'v':
		vec[9] = 1.0
	case 'a':
		vec[10] = 1.0
	case 'r':
		vec[11] = 1.0
	}

	typeHash := sha256.Sum256([]byte(synset.Type))
	for i := 0; i < 4; i++ {
		vec[12+i] = hashByteToFloat(typeHash[i])
	}

	def := ""
	if len(synset.Definitions) > 0 {
		def = synset.Definitions[0]
	}
	defHash := sha256.Sum256([]byte(def))
	for i := 0; i < 16; i++ {
		vec[16+i] = hashByteToFloat(defHash[i])
	}

	memberCount := float64(len(synset.Members))
	vec[32] = math.Min(memberCount/10.0, 1.0)
	totalLen := 0
	for _, m := range synset.Members {
		totalLen += len(m)
	}
	avgLen := 0.0
	if memberCount > 0 {
		avgLen = float64(totalLen) / memberCount
	}
	vec[33] = math.Min(avgLen/12.0, 1.0)

	membersConcat := strings.Join(synset.Members, "|")
	memberHash := sha256.Sum256([]byte(membersConcat))
	for i := 0; i < 14; i++ {
		vec[34+i] = hashByteToFloat(memberHash[i])
	}

	idData := synset.SynsetId + ":" + synset.ILI
	idHash := sha256.Sum256([]byte(idData))
	for i := 0; i < 16; i++ {
		vec[48+i] = hashByteToFloat(idHash[i])
	}

	return &SynsetEmbedding{
		SynsetID: synset.SynsetId,
		Vector:   vec,
		POS:      pos,
		Domain:   synset.Domain,
	}
}

func synsetTypeToPOS(typ string) byte {
	switch strings.ToLower(typ) {
	case "n", "noun":
		return 'n'
	case "v", "verb":
		return 'v'
	case "a", "adj", "adjective", "s":
		return 'a'
	case "r", "adv", "adverb":
		return 'r'
	default:
		return 'n'
	}
}

func hashByteToFloat(b byte) float64 {
	return (float64(b)/127.5 - 1.0)
}

func WordToEmbedding(word string, idx *EmbeddingIndex) *SynsetEmbedding {
	return idx.GetByWord(strings.ToLower(word))
}

func WordsToVector(words []string, idx *EmbeddingIndex) []float64 {
	var embs []*SynsetEmbedding
	for _, w := range words {
		if e := idx.GetByWord(strings.ToLower(w)); e != nil {
			embs = append(embs, e)
		}
	}
	return CombineEmbeddings(embs, nil)
}
