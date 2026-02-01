package embed

const Dimension = 64

type SynsetEmbedding struct {
	SynsetID string
	Vector   []float64
	POS      byte
	Domain   string
}

type EmbeddingIndex struct {
	BySynsetID map[string]*SynsetEmbedding
	POSIndex   map[byte][]*SynsetEmbedding
	ByWord     map[string]*SynsetEmbedding
}

type EmbeddingConfig struct {
	Dimension int
}

func DefaultEmbeddingConfig() *EmbeddingConfig {
	return &EmbeddingConfig{Dimension: Dimension}
}

func (idx *EmbeddingIndex) Get(synsetID string) *SynsetEmbedding {
	return idx.BySynsetID[synsetID]
}

func (idx *EmbeddingIndex) GetByWord(word string) *SynsetEmbedding {
	return idx.ByWord[word]
}

func (idx *EmbeddingIndex) Candidates(pos byte) []*SynsetEmbedding {
	return idx.POSIndex[pos]
}

func (idx *EmbeddingIndex) Size() int {
	return len(idx.BySynsetID)
}
