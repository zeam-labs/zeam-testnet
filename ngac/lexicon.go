
package ngac

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"strings"
	"sync"

	"zeam/quantum"
)


type OEWNDatabase struct {
	Lexes         []Lex         `json:"lexes"`
	Synsets       []Synset      `json:"synsets"`
	Senses        []Sense       `json:"senses"`
	VerbFrames    []interface{} `json:"verbFrames"`
	VerbTemplates []interface{} `json:"verbTemplates"`
}


type Lex struct {
	Lemma        string   `json:"lemma"`
	Type         string   `json:"type"` 
	SenseKeys    []string `json:"senseKeys"`
	Discriminant *string  `json:"discriminant"`
	Source       string   `json:"source"`
}


type Synset struct {
	SynsetId    string   `json:"synsetId"`
	Type        string   `json:"type"`
	Members     []string `json:"members"` 
	Definitions []string `json:"definitions"`
	ILI         string   `json:"ili"`
	Domain      string   `json:"domain"`
}


type Sense struct {
	SenseId  string   `json:"senseId"`
	SynsetId string   `json:"synsetId"`
	LexIndex int      `json:"lexIndex"`
	Type     string   `json:"type"`
	Lex      Lex      `json:"lex"`
	TagCount TagCount `json:"tagCount"`
}


type TagCount struct {
	Count    int `json:"count"`
	SenseNum int `json:"senseNum"`
}


type SubstrateDictionary struct {
	FilePath string

	
	LemmaIndex  map[string]*Lex     
	SynsetIndex map[string]*Synset  
	SenseIndex  map[string]*Sense   
	LemmaRanked map[string][]string 

	mutex sync.RWMutex
}


func BuildDictionaryIndex(jsonPath string) (*SubstrateDictionary, error) {
	fmt.Println("[NGAC] Building OEWN dictionary index...")

	
	db, err := loadOEWN(jsonPath)
	if err != nil {
		return nil, err
	}

	dict := &SubstrateDictionary{
		FilePath:    jsonPath,
		LemmaIndex:  make(map[string]*Lex),
		SynsetIndex: make(map[string]*Synset),
		SenseIndex:  make(map[string]*Sense),
		LemmaRanked: make(map[string][]string),
	}

	
	fmt.Printf("[NGAC] Indexing %d lexes...\n", len(db.Lexes))
	for i := range db.Lexes {
		lex := &db.Lexes[i]
		key := strings.ToLower(lex.Lemma)
		dict.LemmaIndex[key] = lex
	}

	
	fmt.Printf("[NGAC] Indexing %d synsets...\n", len(db.Synsets))
	for i := range db.Synsets {
		synset := &db.Synsets[i]
		dict.SynsetIndex[synset.SynsetId] = synset
	}

	
	fmt.Printf("[NGAC] Indexing %d senses...\n", len(db.Senses))

	
	type rankedSense struct {
		synsetId string
		tagCount int
	}
	lemmaGroups := make(map[string][]rankedSense)

	for i := range db.Senses {
		sense := &db.Senses[i]
		dict.SenseIndex[sense.SenseId] = sense

		
		if sense.Lex.Lemma != "" {
			key := sense.Type + "." + strings.ToLower(sense.Lex.Lemma)

			
			tagCount := 0
			if sense.TagCount.Count > 0 {
				tagCount = sense.TagCount.Count
			}

			lemmaGroups[key] = append(lemmaGroups[key], rankedSense{
				synsetId: sense.SynsetId,
				tagCount: tagCount,
			})
		}
	}

	
	for key, senses := range lemmaGroups {
		
		for i := 0; i < len(senses); i++ {
			for j := i + 1; j < len(senses); j++ {
				if senses[j].tagCount > senses[i].tagCount {
					senses[i], senses[j] = senses[j], senses[i]
				}
			}
		}

		
		synsetIds := make([]string, len(senses))
		for i, s := range senses {
			synsetIds[i] = s.synsetId
		}
		dict.LemmaRanked[key] = synsetIds
	}

	fmt.Printf("[NGAC] Index built: %d words, %d meanings, %d ranked entries\n",
		len(dict.LemmaIndex), len(dict.SynsetIndex), len(dict.LemmaRanked))

	return dict, nil
}


func loadOEWN(path string) (*OEWNDatabase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var db OEWNDatabase
	decoder := json.NewDecoder(file)

	fmt.Println("[NGAC] Parsing OEWN JSON (this may take a moment)...")
	if err := decoder.Decode(&db); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &db, nil
}


func (sd *SubstrateDictionary) QueryWord(coord *big.Int) []*Synset {
	sd.mutex.RLock()
	defer sd.mutex.RUnlock()

	
	word := strings.ToLower(quantum.UTF8_DECODE(coord))

	
	lex, exists := sd.LemmaIndex[word]
	if !exists {
		return nil
	}

	
	synsets := make([]*Synset, 0)
	for _, senseKey := range lex.SenseKeys {
		sense, ok := sd.SenseIndex[senseKey]
		if !ok {
			continue
		}

		synset, ok := sd.SynsetIndex[sense.SynsetId]
		if !ok {
			continue
		}

		synsets = append(synsets, synset)
	}

	return synsets
}


func (sd *SubstrateDictionary) GetDefinition(coord *big.Int) string {
	word := strings.ToLower(quantum.UTF8_DECODE(coord))

	
	posTypes := []string{"n", "v", "a", "r"}
	for _, pos := range posTypes {
		key := pos + "." + word
		if synsetIDs, exists := sd.LemmaRanked[key]; exists && len(synsetIDs) > 0 {
			
			if synset, ok := sd.SynsetIndex[synsetIDs[0]]; ok {
				if len(synset.Definitions) > 0 {
					return synset.Definitions[0]
				}
			}
		}
	}

	
	synsets := sd.QueryWord(coord)
	if len(synsets) > 0 && len(synsets[0].Definitions) > 0 {
		return synsets[0].Definitions[0]
	}

	return ""
}


func (sd *SubstrateDictionary) GetPartOfSpeech(coord *big.Int) string {
	word := strings.ToLower(quantum.UTF8_DECODE(coord))
	lex, exists := sd.LemmaIndex[word]
	if !exists {
		return ""
	}
	return lex.Type
}


func (sd *SubstrateDictionary) GetSynonyms(coord *big.Int) []string {
	synsets := sd.QueryWord(coord)
	if len(synsets) == 0 {
		return nil
	}

	word := strings.ToLower(quantum.UTF8_DECODE(coord))
	synonyms := make([]string, 0)

	
	for _, member := range synsets[0].Members {
		if strings.ToLower(member) != word {
			synonyms = append(synonyms, member)
		}
	}

	return synonyms
}


func (sd *SubstrateDictionary) WordExists(coord *big.Int) bool {
	word := strings.ToLower(quantum.UTF8_DECODE(coord))
	_, exists := sd.LemmaIndex[word]
	return exists
}


func InitializeSemanticField(sc *quantum.SubstrateChain, dict *SubstrateDictionary, words []string) error {
	fmt.Printf("[NGAC] Initializing semantic field with %d words...\n", len(words))

	initialized := 0
	for _, word := range words {
		coord := quantum.UTF8_ENCODE(sc, strings.ToLower(word))

		
		if !dict.WordExists(coord) {
			continue
		}

		
		synsets := dict.QueryWord(coord)
		if len(synsets) == 0 {
			continue
		}

		
		sc.SetAmplitude(coord, complex(1, 0))

		
		basePhase := calculatePhaseFromSynsetID(synsets[0].SynsetId)
		sc.SetPhase(coord, complex(math.Cos(basePhase), math.Sin(basePhase)))

		
		synonyms := dict.GetSynonyms(coord)
		for _, syn := range synonyms {
			synCoord := quantum.UTF8_ENCODE(sc, strings.ToLower(syn))

			
			sc.SetAmplitude(synCoord, complex(1, 0))
			sc.SetPhase(synCoord, complex(math.Cos(basePhase), math.Sin(basePhase)))

			
			quantum.COMPOSE(sc, coord, synCoord)
		}

		initialized++
	}

	fmt.Printf("[NGAC] Initialized %d words in semantic field\n", initialized)
	return nil
}


func calculatePhaseFromSynsetID(synsetId string) float64 {
	hash := 0
	for _, ch := range synsetId {
		hash = (hash * 31) + int(ch)
	}
	return float64(hash%360) * (math.Pi / 180.0)
}


func (sd *SubstrateDictionary) GetSynsetsForWord(word string) []string {
	sd.mutex.RLock()
	defer sd.mutex.RUnlock()

	word = strings.ToLower(word)
	lex, exists := sd.LemmaIndex[word]
	if !exists {
		return nil
	}

	synsetIDs := make([]string, 0)
	for _, senseKey := range lex.SenseKeys {
		sense, ok := sd.SenseIndex[senseKey]
		if ok {
			synsetIDs = append(synsetIDs, sense.SynsetId)
		}
	}
	return synsetIDs
}


func (sd *SubstrateDictionary) GetSynsetByID(synsetID string) *Synset {
	sd.mutex.RLock()
	defer sd.mutex.RUnlock()
	return sd.SynsetIndex[synsetID]
}


func (sd *SubstrateDictionary) GetAllWords() []string {
	words := make([]string, 0, len(sd.LemmaIndex))
	for word := range sd.LemmaIndex {
		words = append(words, word)
	}
	return words
}


func (sd *SubstrateDictionary) GetWordsByPOS(pos string, limit int) []string {
	words := make([]string, 0)

	for lemma, lex := range sd.LemmaIndex {
		if lex.Type == pos {
			words = append(words, lemma)
			if limit > 0 && len(words) >= limit {
				break
			}
		}
	}

	return words
}


func (sd *SubstrateDictionary) GetRandomWords(n int) []string {
	allWords := sd.GetAllWords()
	if n > len(allWords) {
		n = len(allWords)
	}

	
	words := make([]string, 0, n)
	step := len(allWords) / n
	for i := 0; i < n; i++ {
		words = append(words, allWords[i*step])
	}

	return words
}


func ApplySimpleGrammar(words []string, dict *SubstrateDictionary, sc *quantum.SubstrateChain) string {
	if len(words) == 0 {
		return ""
	}

	result := make([]string, 0)

	for i, word := range words {
		coord := quantum.UTF8_ENCODE(sc, word)
		pos := dict.GetPartOfSpeech(coord)

		
		if pos == "n" && i == 0 {
			result = append(result, "the")
		} else if pos == "n" && i > 0 {
			prevCoord := quantum.UTF8_ENCODE(sc, words[i-1])
			prevPos := dict.GetPartOfSpeech(prevCoord)
			if prevPos == "n" {
				result = append(result, "and", "the")
			}
		}

		result = append(result, word)
	}

	return strings.Join(result, " ")
}


func (sd *SubstrateDictionary) CollapseToSynsets(hashBytes []byte) []*Synset {
	sd.mutex.RLock()
	defer sd.mutex.RUnlock()

	if len(hashBytes) < 8 {
		return nil
	}

	synsets := make([]*Synset, 0)

	
	synsetIDs := make([]string, 0, len(sd.SynsetIndex))
	for id := range sd.SynsetIndex {
		synsetIDs = append(synsetIDs, id)
	}

	if len(synsetIDs) == 0 {
		return nil
	}

	
	for i := 0; i+3 < len(hashBytes) && i < 16; i += 4 {
		
		idx := uint32(hashBytes[i])<<24 | uint32(hashBytes[i+1])<<16 |
			uint32(hashBytes[i+2])<<8 | uint32(hashBytes[i+3])

		
		synsetIdx := int(idx % uint32(len(synsetIDs)))
		synsetID := synsetIDs[synsetIdx]

		if synset, ok := sd.SynsetIndex[synsetID]; ok {
			synsets = append(synsets, synset)
		}
	}

	return synsets
}


func (sd *SubstrateDictionary) CollapseToWords(hashBytes []byte) []string {
	synsets := sd.CollapseToSynsets(hashBytes)
	if len(synsets) == 0 {
		return nil
	}

	words := make([]string, 0)
	seen := make(map[string]bool)

	for _, synset := range synsets {
		if len(synset.Members) > 0 {
			
			memberIdx := int(hashBytes[0]) % len(synset.Members)
			word := synset.Members[memberIdx]

			if !seen[word] {
				words = append(words, word)
				seen[word] = true
			}
		}
	}

	return words
}


func (sd *SubstrateDictionary) CollapseToSemanticVector(hashBytes []byte) map[string]float64 {
	sd.mutex.RLock()
	defer sd.mutex.RUnlock()

	
	typeCounts := make(map[string]int)
	total := 0

	synsets := sd.CollapseToSynsets(hashBytes)
	for _, synset := range synsets {
		typeCounts[synset.Type]++
		total++
	}

	
	vector := make(map[string]float64)
	if total > 0 {
		for typ, count := range typeCounts {
			vector[typ] = float64(count) / float64(total)
		}
	}

	return vector
}


func (sd *SubstrateDictionary) HashToSentence(hashBytes []byte, sc *quantum.SubstrateChain) string {
	words := sd.CollapseToWords(hashBytes)
	if len(words) == 0 {
		return ""
	}

	
	return ApplySimpleGrammar(words, sd, sc)
}


type CollapseContext struct {
	
	AccumulatedHash []byte

	
	ActiveSynsets []*Synset

	
	SemanticState map[string]float64

	
	GeneratedWords []string
}


func NewCollapseContext() *CollapseContext {
	return &CollapseContext{
		AccumulatedHash: make([]byte, 0),
		ActiveSynsets:   make([]*Synset, 0),
		SemanticState:   make(map[string]float64),
		GeneratedWords:  make([]string, 0),
	}
}


func (cc *CollapseContext) AddCollapse(sd *SubstrateDictionary, hashBytes []byte) {
	
	cc.AccumulatedHash = append(cc.AccumulatedHash, hashBytes...)

	
	newSynsets := sd.CollapseToSynsets(hashBytes)
	cc.ActiveSynsets = append(cc.ActiveSynsets, newSynsets...)

	
	newVector := sd.CollapseToSemanticVector(hashBytes)
	for k, v := range newVector {
		cc.SemanticState[k] = (cc.SemanticState[k] + v) / 2.0 
	}

	
	words := sd.CollapseToWords(hashBytes)
	cc.GeneratedWords = append(cc.GeneratedWords, words...)
}


func ClassifyWordType(dict *SubstrateDictionary, coord *big.Int) string {
	pos := dict.GetPartOfSpeech(coord)

	switch pos {
	case "n": 
		return "taxonomy"
	case "v": 
		return "contextual"
	case "a": 
		return "emotional"
	default:
		return "taxonomy"
	}
}
