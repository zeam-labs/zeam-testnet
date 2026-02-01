package grammar

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"sync"
)

type GrammarHash struct {

	OpType GrammarOp

	InputText    string
	InputConcept string
	InputPOS     PartOfSpeech
	SlotName     string
	PatternName  string

	ContextHash []byte
	Pressure    PressureContext
}

type GrammarOp uint8

const (
	OpDetectSentenceType GrammarOp = iota
	OpDetectIntent
	OpIdentifyPOS
	OpSelectPattern
	OpFillSlot
	OpSelectModifier
	OpAssembleSentence
)

type GrammarResult struct {

	Hash []byte

	SentenceType SentenceType
	Intent       string
	POS          PartOfSpeech
	Pattern      *SentencePattern
	Word         string
	Modifiers    []string
}

type DistributedGrammar struct {

	Dispatcher func(payload []byte) ([][]byte, error)

	POSLookup     func(word string) PartOfSpeech
	WordsByPOS    func(pos string, limit int) []string
	SynsetLookup  func(word string) []string
	SynsetByID    func(id string) *SynsetInfo
	GetSynonyms   func(word string) []string
	GetDefinition func(word string) string
}

func NewDistributedGrammar(dispatcher func(payload []byte) ([][]byte, error)) *DistributedGrammar {
	return &DistributedGrammar{
		Dispatcher: dispatcher,
	}
}

func EncodeGrammarPayload(gh *GrammarHash) []byte {

	payload := make([]byte, 0, 128)

	payload = append(payload, byte(gh.OpType))

	pressureBytes := make([]byte, 32)
	binary.BigEndian.PutUint64(pressureBytes[0:8], uint64(gh.Pressure.Hadamard*1e9))
	binary.BigEndian.PutUint64(pressureBytes[8:16], uint64(gh.Pressure.PauliX*1e9))
	binary.BigEndian.PutUint64(pressureBytes[16:24], uint64(gh.Pressure.PauliZ*1e9))
	binary.BigEndian.PutUint64(pressureBytes[24:32], uint64(gh.Pressure.Phase*1e9))
	payload = append(payload, pressureBytes...)

	if len(gh.ContextHash) >= 32 {
		payload = append(payload, gh.ContextHash[:32]...)
	} else {
		padding := make([]byte, 32)
		copy(padding, gh.ContextHash)
		payload = append(payload, padding...)
	}

	switch gh.OpType {
	case OpDetectSentenceType, OpDetectIntent:

		textHash := sha256.Sum256([]byte(gh.InputText))
		payload = append(payload, textHash[:]...)

	case OpIdentifyPOS:

		wordHash := sha256.Sum256([]byte(gh.InputConcept))
		payload = append(payload, wordHash[:]...)

	case OpSelectPattern:

		intentHash := sha256.Sum256([]byte(gh.InputText))
		payload = append(payload, intentHash[:]...)

	case OpFillSlot:

		combined := gh.SlotName + "|" + gh.InputConcept
		combinedHash := sha256.Sum256([]byte(combined))
		payload = append(payload, combinedHash[:]...)

		payload = append(payload, byte(posToIndex(gh.InputPOS)))

	case OpSelectModifier:

		combined := gh.InputConcept + "|" + gh.SlotName
		combinedHash := sha256.Sum256([]byte(combined))
		payload = append(payload, combinedHash[:]...)

	case OpAssembleSentence:

		patternHash := sha256.Sum256([]byte(gh.PatternName))
		payload = append(payload, patternHash[:]...)
	}

	return payload
}

func posToIndex(pos PartOfSpeech) int {
	switch pos {
	case POS_Noun:
		return 0
	case POS_Verb:
		return 1
	case POS_Adjective:
		return 2
	case POS_Adverb:
		return 3
	case POS_Determiner:
		return 4
	case POS_Pronoun:
		return 5
	case POS_Preposition:
		return 6
	case POS_Conjunction:
		return 7
	case POS_Auxiliary:
		return 8
	default:
		return 0
	}
}

func indexToPOS(idx int) PartOfSpeech {
	switch idx % 9 {
	case 0:
		return POS_Noun
	case 1:
		return POS_Verb
	case 2:
		return POS_Adjective
	case 3:
		return POS_Adverb
	case 4:
		return POS_Determiner
	case 5:
		return POS_Pronoun
	case 6:
		return POS_Preposition
	case 7:
		return POS_Conjunction
	case 8:
		return POS_Auxiliary
	default:
		return POS_Noun
	}
}

func detectSentenceTypeFromText(text string) SentenceType {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return TypeStatement
	}

	firstWord := strings.Trim(fields[0], ",.!?")

	greetings := map[string]bool{
		"hello": true, "hi": true, "hey": true, "greetings": true,
		"howdy": true, "hiya": true, "yo": true, "welcome": true,
	}
	if greetings[firstWord] {
		return TypeGreeting
	}

	if strings.HasSuffix(text, "?") {
		return TypeQuestion
	}
	if strings.HasSuffix(text, "!") {
		return TypeExclamation
	}

	questionWords := map[string]bool{
		"what": true, "who": true, "where": true, "when": true,
		"why": true, "how": true, "which": true, "whose": true,
	}
	if questionWords[firstWord] {
		return TypeQuestion
	}

	auxVerbs := map[string]bool{
		"is": true, "are": true, "was": true, "were": true,
		"do": true, "does": true, "did": true, "can": true,
		"could": true, "would": true, "should": true, "will": true,
	}
	if auxVerbs[firstWord] {
		return TypeQuestion
	}

	commandVerbs := map[string]bool{
		"tell": true, "explain": true, "describe": true, "show": true,
		"give": true, "list": true, "define": true, "help": true,
	}
	if commandVerbs[firstWord] {
		return TypeCommand
	}

	return TypeStatement
}

func detectIntentFromText(text string, sentType SentenceType) string {
	lower := strings.ToLower(text)

	switch sentType {
	case TypeGreeting:
		return "greeting"
	case TypeQuestion:

		if strings.Contains(lower, "what is") || strings.Contains(lower, "what are") {
			return "explanation"
		}
		if strings.Contains(lower, "how do") || strings.Contains(lower, "how does") {
			return "explanation"
		}
		return "question"
	case TypeCommand:
		if strings.Contains(lower, "explain") || strings.Contains(lower, "describe") {
			return "explanation"
		}
		if strings.Contains(lower, "tell") {
			return "explanation"
		}
		return "command"
	default:
		return "statement"
	}
}

func (dg *DistributedGrammar) ParseDistributed(text string, pressure PressureContext) (*ParsedSentence, error) {
	result := &ParsedSentence{
		Original: text,
		Words:    make([]*ParsedWord, 0),
		Concepts: make([]string, 0),
	}

	result.Type = detectSentenceTypeFromText(text)

	result.Intent = detectIntentFromText(text, result.Type)

	tokens := tokenizeSimple(text)

	initialHash := sha256.Sum256([]byte(text))
	contextHash := initialHash[:]
	for i, token := range tokens {
		posPayload := EncodeGrammarPayload(&GrammarHash{
			OpType:       OpIdentifyPOS,
			InputConcept: token,
			Pressure:     pressure,
			ContextHash:  contextHash,
		})

		posHashes, err := dg.Dispatcher(posPayload)
		if err != nil {
			continue
		}

		pos := dg.decodePOS(posHashes, token)

		word := &ParsedWord{
			Word:     token,
			Lemma:    token,
			POS:      pos,
			Position: i,
		}
		result.Words = append(result.Words, word)

		if len(posHashes) > 0 {
			contextHash = posHashes[0]
		}

		if isContentWordSimple(word) {
			result.Concepts = append(result.Concepts, token)
		}
	}

	dg.identifyRolesFromPOS(result)

	result.IsComplete = result.Subject != nil && result.Verb != nil

	return result, nil
}

func (dg *DistributedGrammar) BuildResponseDistributed(
	parsed *ParsedSentence,
	concepts []string,
	pressure PressureContext,
) (string, error) {
	if len(concepts) == 0 {
		return "", nil
	}

	patternPayload := EncodeGrammarPayload(&GrammarHash{
		OpType:    OpSelectPattern,
		InputText: parsed.Intent,
		Pressure:  pressure,
	})

	patternHashes, err := dg.Dispatcher(patternPayload)
	if err != nil {
		return "", err
	}

	pattern := dg.decodePattern(patternHashes, parsed.Intent)
	sentence := NewSentence(pattern)

	contextHash := patternHashes[0]
	usedWords := make(map[string]bool)
	conceptIdx := 0

	for _, slot := range pattern.Slots {
		if slot.FixedWord != "" {
			sentence.FillSlot(slot.Name, slot.FixedWord)
			continue
		}

		concept := ""
		if conceptIdx < len(concepts) {
			concept = concepts[conceptIdx]
			conceptIdx++
		}

		fillPayload := EncodeGrammarPayload(&GrammarHash{
			OpType:       OpFillSlot,
			SlotName:     slot.Name,
			InputConcept: concept,
			InputPOS:     slot.POS,
			Pressure:     pressure,
			ContextHash:  contextHash,
		})

		fillHashes, err := dg.Dispatcher(fillPayload)
		if err != nil {
			continue
		}

		word := dg.decodeWord(fillHashes, slot.POS, concepts, usedWords)
		if word == "" {
			continue
		}

		var modifiers []string
		if slot.Modifiable && pressure.Phase > 0.5 {
			modPayload := EncodeGrammarPayload(&GrammarHash{
				OpType:       OpSelectModifier,
				InputConcept: word,
				SlotName:     slot.Name,
				Pressure:     pressure,
				ContextHash:  fillHashes[0],
			})

			modHashes, err := dg.Dispatcher(modPayload)
			if err == nil {
				mod := dg.decodeModifier(modHashes, slot.POS, concepts, usedWords)
				if mod != "" {
					modifiers = append(modifiers, mod)
					usedWords[mod] = true
				}
			}
		}

		sentence.FillSlot(slot.Name, word, modifiers...)
		usedWords[word] = true

		if len(fillHashes) > 0 {
			contextHash = fillHashes[0]
		}
	}

	return sentence.Build(), nil
}

func (dg *DistributedGrammar) decodeSentenceType(hashes [][]byte) SentenceType {
	if len(hashes) == 0 {
		return TypeStatement
	}

	typeIdx := int(hashes[0][0]) % 5
	switch typeIdx {
	case 0:
		return TypeStatement
	case 1:
		return TypeQuestion
	case 2:
		return TypeCommand
	case 3:
		return TypeGreeting
	case 4:
		return TypeExclamation
	default:
		return TypeStatement
	}
}

func (dg *DistributedGrammar) decodeIntent(hashes [][]byte, sentType SentenceType) string {
	if len(hashes) == 0 {
		return "statement"
	}

	intents := map[SentenceType][]string{
		TypeStatement:   {"statement", "description", "acknowledgment"},
		TypeQuestion:    {"question", "explanation", "inquiry"},
		TypeCommand:     {"command", "request", "instruction"},
		TypeGreeting:    {"greeting", "salutation"},
		TypeExclamation: {"exclamation", "statement"},
	}

	options := intents[sentType]
	if len(options) == 0 {
		return "statement"
	}

	idx := int(hashes[0][1]) % len(options)
	return options[idx]
}

func (dg *DistributedGrammar) decodePOS(hashes [][]byte, word string) PartOfSpeech {

	if dg.POSLookup != nil {
		pos := dg.POSLookup(word)
		if pos != "" {
			return pos
		}
	}

	if len(hashes) == 0 {
		return POS_Noun
	}

	posIdx := int(hashes[0][0]) % 4
	return indexToPOS(posIdx)
}

func (dg *DistributedGrammar) decodePattern(hashes [][]byte, intent string) *SentencePattern {

	patterns := GetPatternsForIntent(intent)
	if len(patterns) == 0 {
		return PatternDeclarativeSVO
	}

	if len(hashes) == 0 {
		return patterns[0]
	}

	idx := int(hashes[0][0]) % len(patterns)
	return patterns[idx]
}

func (dg *DistributedGrammar) decodeWord(hashes [][]byte, targetPOS PartOfSpeech, concepts []string, used map[string]bool) string {
	if len(hashes) == 0 {
		return ""
	}

	availableConcepts := make([]string, 0)
	for _, c := range concepts {
		if !used[c] {
			if dg.POSLookup != nil {
				pos := dg.POSLookup(c)
				if pos == targetPOS {
					availableConcepts = append(availableConcepts, c)
				}
			} else {
				availableConcepts = append(availableConcepts, c)
			}
		}
	}

	if len(availableConcepts) > 0 {
		hashInt := new(big.Int).SetBytes(hashes[0]).Int64()
		if hashInt < 0 {
			hashInt = -hashInt
		}
		idx := int(hashInt % int64(len(availableConcepts)))
		return availableConcepts[idx]
	}

	if dg.SynsetLookup != nil && dg.SynsetByID != nil {
		relatedWords := make([]string, 0)
		for _, c := range concepts {
			if used[c] {
				continue
			}
			synsetIDs := dg.SynsetLookup(c)
			for _, sid := range synsetIDs {
				synset := dg.SynsetByID(sid)
				if synset != nil && synset.Type == string(targetPOS) {
					for _, member := range synset.Members {
						if !used[member] && member != c {
							relatedWords = append(relatedWords, member)
						}
					}
				}
			}
			if len(relatedWords) >= 10 {
				break
			}
		}
		if len(relatedWords) > 0 {
			hashInt := new(big.Int).SetBytes(hashes[0]).Int64()
			if hashInt < 0 {
				hashInt = -hashInt
			}
			idx := int(hashInt % int64(len(relatedWords)))
			return relatedWords[idx]
		}
	}

	if dg.GetSynonyms != nil && dg.POSLookup != nil {
		for _, c := range concepts {
			if used[c] {
				continue
			}
			synonyms := dg.GetSynonyms(c)
			for _, syn := range synonyms {
				if !used[syn] {
					pos := dg.POSLookup(syn)
					if pos == targetPOS {
						return syn
					}
				}
			}
		}
	}

	fallbacks := getFallbackWords(targetPOS)
	if len(fallbacks) > 0 {
		hashInt := new(big.Int).SetBytes(hashes[0]).Int64()
		if hashInt < 0 {
			hashInt = -hashInt
		}
		idx := int(hashInt % int64(len(fallbacks)))
		return fallbacks[idx]
	}

	return ""
}

func (dg *DistributedGrammar) decodeModifier(hashes [][]byte, targetPOS PartOfSpeech, concepts []string, used map[string]bool) string {
	if len(hashes) == 0 {
		return ""
	}

	modPOS := POS_Adjective
	if targetPOS == POS_Verb {
		modPOS = POS_Adverb
	}

	for _, c := range concepts {
		if used[c] {
			continue
		}
		if dg.POSLookup != nil {
			pos := dg.POSLookup(c)
			if pos == modPOS {
				return c
			}
		}
	}

	if dg.WordsByPOS != nil {
		words := dg.WordsByPOS(string(modPOS), 20)
		if len(words) > 0 {
			idx := int(hashes[0][0]) % len(words)
			return words[idx]
		}
	}

	return ""
}

func (dg *DistributedGrammar) identifyRolesFromPOS(ps *ParsedSentence) {

	verbIdx := -1
	for i, w := range ps.Words {
		if w.POS == POS_Verb {
			verbIdx = i
			w.Role = "verb"
			ps.Verb = w
			break
		}
	}

	for i, w := range ps.Words {
		if verbIdx >= 0 && i >= verbIdx {
			break
		}
		if w.POS == POS_Noun || w.POS == POS_Pronoun {
			w.Role = "subject"
			ps.Subject = w
			break
		}
	}

	if verbIdx >= 0 {
		for i := verbIdx + 1; i < len(ps.Words); i++ {
			w := ps.Words[i]
			if w.POS == POS_Noun || w.POS == POS_Pronoun {
				w.Role = "object"
				ps.Object = w
				break
			}
		}
	}
}

func tokenizeSimple(text string) []string {

	tokens := make([]string, 0)
	current := ""

	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
		} else if r == '.' || r == ',' || r == '!' || r == '?' || r == ';' || r == ':' {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}

	if current != "" {
		tokens = append(tokens, current)
	}

	return tokens
}

func isContentWordSimple(w *ParsedWord) bool {
	if len(w.Lemma) < 3 {
		return false
	}
	switch w.POS {
	case POS_Noun, POS_Verb, POS_Adjective, POS_Adverb:
		return true
	}
	return false
}

func getFallbackWords(pos PartOfSpeech) []string {
	switch pos {
	case POS_Noun:
		return []string{"system", "process", "concept", "aspect", "element", "idea", "matter", "subject", "topic", "point"}
	case POS_Verb:
		return []string{"involves", "represents", "includes", "relates to", "concerns", "describes", "indicates", "shows", "means", "requires"}
	case POS_Adjective:
		return []string{"relevant", "significant", "important", "notable", "key", "essential", "primary", "main", "central", "fundamental"}
	case POS_Adverb:
		return []string{"essentially", "generally", "typically", "specifically", "particularly", "notably", "primarily", "mainly"}
	default:
		return []string{}
	}
}

func (dg *DistributedGrammar) GenerateDistributed(
	inputText string,
	concepts []string,
	pressure PressureContext,
) (string, error) {

	parsed, err := dg.ParseDistributed(inputText, pressure)
	if err != nil {
		return "", err
	}

	fmt.Printf("[Grammar] Input: %q, Type: %v, Intent: %s\n", inputText, parsed.Type, parsed.Intent)

	if parsed.Intent == "greeting" {
		fmt.Printf("[Grammar] Detected greeting, returning Hello.\n")
		return "Hello.", nil
	}

	allConcepts := make([]string, 0, len(parsed.Concepts)+len(concepts))
	seen := make(map[string]bool)
	for _, c := range parsed.Concepts {
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

	return dg.BuildResponseDistributed(parsed, allConcepts, pressure)
}

type GrammarPayloadV2 struct {
	InputText    string
	Concepts     []string
	Pressure     PressureContext
	MaxSlots     int
	MaxModifiers int
}

func EncodeGrammarPayloadV2(gp *GrammarPayloadV2) []byte {

	payload := make([]byte, 0, 256)

	payload = append(payload, 0x02)

	numConcepts := len(gp.Concepts)
	if numConcepts > 255 {
		numConcepts = 255
	}
	payload = append(payload, byte(numConcepts>>8), byte(numConcepts))

	pressureBytes := make([]byte, 32)
	binary.BigEndian.PutUint64(pressureBytes[0:8], uint64(gp.Pressure.Hadamard*1e9))
	binary.BigEndian.PutUint64(pressureBytes[8:16], uint64(gp.Pressure.PauliX*1e9))
	binary.BigEndian.PutUint64(pressureBytes[16:24], uint64(gp.Pressure.PauliZ*1e9))
	binary.BigEndian.PutUint64(pressureBytes[24:32], uint64(gp.Pressure.Phase*1e9))
	payload = append(payload, pressureBytes...)

	inputHash := sha256.Sum256([]byte(gp.InputText))
	payload = append(payload, inputHash[:]...)

	for i, concept := range gp.Concepts {
		if i >= 16 {
			break
		}
		conceptHash := sha256.Sum256([]byte(concept))
		payload = append(payload, conceptHash[:16]...)
	}

	return payload
}

type HashStream struct {
	hashes    [][]byte
	hashIdx   int
	byteIdx   int
	bytesUsed int
}

func NewHashStream(hashes [][]byte) *HashStream {
	return &HashStream{
		hashes:  hashes,
		hashIdx: 0,
		byteIdx: 0,
	}
}

func (hs *HashStream) Next(n int) []byte {
	if len(hs.hashes) == 0 {
		return make([]byte, n)
	}

	result := make([]byte, n)
	written := 0

	for written < n {
		if hs.hashIdx >= len(hs.hashes) {

			hs.hashIdx = 0
			hs.byteIdx = 0

			if len(hs.hashes) > 0 && len(hs.hashes[0]) > 0 {
				hs.hashes[0][0] ^= byte(hs.bytesUsed)
			}
		}

		hash := hs.hashes[hs.hashIdx]
		available := len(hash) - hs.byteIdx
		need := n - written

		if available <= 0 {
			hs.hashIdx++
			hs.byteIdx = 0
			continue
		}

		take := available
		if take > need {
			take = need
		}

		copy(result[written:], hash[hs.byteIdx:hs.byteIdx+take])
		written += take
		hs.byteIdx += take
		hs.bytesUsed += take

		if hs.byteIdx >= len(hash) {
			hs.hashIdx++
			hs.byteIdx = 0
		}
	}

	return result
}

func (hs *HashStream) NextInt(max int) int {
	if max <= 0 {
		return 0
	}
	bytes := hs.Next(4)
	val := int(binary.BigEndian.Uint32(bytes))
	if val < 0 {
		val = -val
	}
	return val % max
}

func (dg *DistributedGrammar) GenerateDistributedV2(
	inputText string,
	concepts []string,
	pressure PressureContext,
) (string, error) {

	parsed := dg.parseLocal(inputText)

	fmt.Printf("[Grammar-V2] Input: %q, Type: %v, Intent: %s\n", inputText, parsed.Type, parsed.Intent)

	if parsed.Intent == "greeting" {
		return "Hello.", nil
	}

	allConcepts := mergeConceptsUnique(parsed.Concepts, concepts)
	if len(allConcepts) == 0 {
		return "", nil
	}

	payload := EncodeGrammarPayloadV2(&GrammarPayloadV2{
		InputText:    inputText,
		Concepts:     allConcepts,
		Pressure:     pressure,
		MaxSlots:     8,
		MaxModifiers: 2,
	})

	hashes, err := dg.Dispatcher(payload)
	if err != nil {
		return "", fmt.Errorf("L1 dispatch failed: %w", err)
	}

	fmt.Printf("[Grammar-V2] Got %d hashes from L1 (single broadcast)\n", len(hashes))

	stream := NewHashStream(hashes)

	pattern := dg.selectPatternFromStream(stream, parsed.Intent)
	sentence := NewSentence(pattern)

	usedWords := make(map[string]bool)
	conceptIdx := 0

	for _, slot := range pattern.Slots {
		if slot.FixedWord != "" {
			sentence.FillSlot(slot.Name, slot.FixedWord)
			continue
		}

		concept := ""
		if conceptIdx < len(allConcepts) {
			concept = allConcepts[conceptIdx]
			conceptIdx++
		}

		word := dg.selectWordFromStream(stream, slot.POS, concept, allConcepts, usedWords)
		if word == "" {
			continue
		}

		var modifiers []string
		if slot.Modifiable && pressure.Phase > 0.5 {
			mod := dg.selectModifierFromStream(stream, slot.POS, allConcepts, usedWords)
			if mod != "" {
				modifiers = append(modifiers, mod)
				usedWords[mod] = true
			}
		}

		sentence.FillSlot(slot.Name, word, modifiers...)
		usedWords[word] = true
	}

	return sentence.Build(), nil
}

func (dg *DistributedGrammar) parseLocal(text string) *ParsedSentence {
	result := &ParsedSentence{
		Original: text,
		Words:    make([]*ParsedWord, 0),
		Concepts: make([]string, 0),
	}

	result.Type = detectSentenceTypeFromText(text)
	result.Intent = detectIntentFromText(text, result.Type)

	tokens := tokenizeSimple(text)

	for i, token := range tokens {
		var pos PartOfSpeech = POS_Noun
		if dg.POSLookup != nil {
			pos = dg.POSLookup(token)
			if pos == "" {
				pos = POS_Noun
			}
		}

		word := &ParsedWord{
			Word:     token,
			Lemma:    strings.ToLower(token),
			POS:      pos,
			Position: i,
		}
		result.Words = append(result.Words, word)

		if isContentWordSimple(word) {
			result.Concepts = append(result.Concepts, token)
		}
	}

	dg.identifyRolesFromPOS(result)
	result.IsComplete = result.Subject != nil && result.Verb != nil

	return result
}

func (dg *DistributedGrammar) selectPatternFromStream(stream *HashStream, intent string) *SentencePattern {
	patterns := GetPatternsForIntent(intent)
	if len(patterns) == 0 {
		return PatternDeclarativeSVO
	}
	idx := stream.NextInt(len(patterns))
	return patterns[idx]
}

func (dg *DistributedGrammar) selectWordFromStream(
	stream *HashStream,
	targetPOS PartOfSpeech,
	primaryConcept string,
	allConcepts []string,
	used map[string]bool,
) string {

	available := make([]string, 0)
	for _, c := range allConcepts {
		if used[c] {
			continue
		}
		if dg.POSLookup != nil {
			pos := dg.POSLookup(c)
			if pos == targetPOS {
				available = append(available, c)
			}
		}
	}
	if len(available) > 0 {
		return available[stream.NextInt(len(available))]
	}

	if dg.SynsetLookup != nil && dg.SynsetByID != nil {
		related := make([]string, 0)
		for _, c := range allConcepts {
			if used[c] {
				continue
			}
			synsetIDs := dg.SynsetLookup(c)
			for _, sid := range synsetIDs {
				synset := dg.SynsetByID(sid)
				if synset != nil && synset.Type == string(targetPOS) {
					for _, member := range synset.Members {
						if !used[member] {
							related = append(related, member)
						}
					}
				}
			}
			if len(related) >= 10 {
				break
			}
		}
		if len(related) > 0 {
			return related[stream.NextInt(len(related))]
		}
	}

	if dg.GetSynonyms != nil && dg.POSLookup != nil {
		for _, c := range allConcepts {
			if used[c] {
				continue
			}
			synonyms := dg.GetSynonyms(c)
			matching := make([]string, 0)
			for _, syn := range synonyms {
				if !used[syn] && dg.POSLookup(syn) == targetPOS {
					matching = append(matching, syn)
				}
			}
			if len(matching) > 0 {
				return matching[stream.NextInt(len(matching))]
			}
		}
	}

	fallbacks := getFallbackWords(targetPOS)
	if len(fallbacks) > 0 {
		return fallbacks[stream.NextInt(len(fallbacks))]
	}

	return ""
}

func (dg *DistributedGrammar) selectModifierFromStream(
	stream *HashStream,
	targetPOS PartOfSpeech,
	concepts []string,
	used map[string]bool,
) string {
	modPOS := POS_Adjective
	if targetPOS == POS_Verb {
		modPOS = POS_Adverb
	}

	for _, c := range concepts {
		if used[c] {
			continue
		}
		if dg.POSLookup != nil && dg.POSLookup(c) == modPOS {
			return c
		}
	}

	if dg.WordsByPOS != nil {
		words := dg.WordsByPOS(string(modPOS), 20)
		if len(words) > 0 {
			return words[stream.NextInt(len(words))]
		}
	}

	return ""
}

func mergeConceptsUnique(a, b []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(a)+len(b))
	for _, c := range a {
		if !seen[c] {
			result = append(result, c)
			seen[c] = true
		}
	}
	for _, c := range b {
		if !seen[c] {
			result = append(result, c)
			seen[c] = true
		}
	}
	return result
}

type FlowComputeInterface interface {
	NextBytes(n int) []byte
	NextUint64() uint64
	SelectIndex(n int) int
}

type FlowToHashStream struct {
	flow FlowComputeInterface
}

func NewFlowToHashStream(flow FlowComputeInterface) *FlowToHashStream {
	return &FlowToHashStream{flow: flow}
}

func (fh *FlowToHashStream) Next(n int) []byte {
	return fh.flow.NextBytes(n)
}

func (fh *FlowToHashStream) NextInt(max int) int {
	if max <= 0 {
		return 0
	}
	return fh.flow.SelectIndex(max)
}

func (dg *DistributedGrammar) GenerateFlowPowered(
	inputText string,
	concepts []string,
	pressure PressureContext,
	flowCompute FlowComputeInterface,
) (string, error) {
	if flowCompute == nil {
		return "", fmt.Errorf("flow compute required for flow-powered generation")
	}

	adapter := NewFlowToHashStream(flowCompute)

	parsed := dg.parseLocal(inputText)

	fmt.Printf("[Grammar-V3-Flow] Input: %q, Intent: %s (NO BROADCAST - using mempool flow)\n",
		inputText, parsed.Intent)

	if parsed.Intent == "greeting" {
		return "Hello.", nil
	}

	allConcepts := mergeConceptsUnique(parsed.Concepts, concepts)
	if len(allConcepts) == 0 {
		allConcepts = []string{"concept", "topic", "matter"}
	}

	patterns := GetPatternsForIntent(parsed.Intent)
	if len(patterns) == 0 {
		patterns = []*SentencePattern{PatternDeclarativeSVO}
	}
	patternIdx := adapter.NextInt(len(patterns))
	pattern := patterns[patternIdx]

	sentence := NewSentence(pattern)
	usedWords := make(map[string]bool)
	conceptIdx := 0

	for _, slot := range pattern.Slots {
		if slot.FixedWord != "" {
			sentence.FillSlot(slot.Name, slot.FixedWord)
			continue
		}

		concept := ""
		if conceptIdx < len(allConcepts) {
			concept = allConcepts[conceptIdx]
			conceptIdx++
		}

		word := dg.selectWordFromFlowAdapter(adapter, slot.POS, concept, allConcepts, usedWords)
		if word == "" {
			continue
		}

		var modifiers []string
		if slot.Modifiable && pressure.Phase > 0.5 {
			mod := dg.selectModifierFromFlowAdapter(adapter, slot.POS, allConcepts, usedWords)
			if mod != "" {
				modifiers = []string{mod}
				usedWords[mod] = true
			}
		}

		sentence.FillSlot(slot.Name, word, modifiers...)
		usedWords[word] = true
	}

	result := sentence.Build()
	if result == "" {
		result = "The concept involves the matter."
	}

	return result, nil
}

func (dg *DistributedGrammar) selectWordFromFlowAdapter(
	adapter *FlowToHashStream,
	targetPOS PartOfSpeech,
	primaryConcept string,
	allConcepts []string,
	used map[string]bool,
) string {

	available := make([]string, 0)
	for _, c := range allConcepts {
		if used[c] {
			continue
		}
		if dg.POSLookup != nil {
			pos := dg.POSLookup(c)
			if pos == targetPOS {
				available = append(available, c)
			}
		}
	}
	if len(available) > 0 {
		return available[adapter.NextInt(len(available))]
	}

	if dg.SynsetLookup != nil && dg.SynsetByID != nil {
		related := make([]string, 0)
		for _, c := range allConcepts {
			if used[c] {
				continue
			}
			synsetIDs := dg.SynsetLookup(c)
			for _, sid := range synsetIDs {
				synset := dg.SynsetByID(sid)
				if synset != nil && synset.Type == string(targetPOS) {
					for _, member := range synset.Members {
						if !used[member] {
							related = append(related, member)
						}
					}
				}
			}
			if len(related) >= 10 {
				break
			}
		}
		if len(related) > 0 {
			return related[adapter.NextInt(len(related))]
		}
	}

	if dg.GetSynonyms != nil && dg.POSLookup != nil {
		for _, c := range allConcepts {
			if used[c] {
				continue
			}
			synonyms := dg.GetSynonyms(c)
			matching := make([]string, 0)
			for _, syn := range synonyms {
				if !used[syn] && dg.POSLookup(syn) == targetPOS {
					matching = append(matching, syn)
				}
			}
			if len(matching) > 0 {
				return matching[adapter.NextInt(len(matching))]
			}
		}
	}

	fallbacks := getFallbackWords(targetPOS)
	if len(fallbacks) > 0 {
		return fallbacks[adapter.NextInt(len(fallbacks))]
	}

	return ""
}

func (dg *DistributedGrammar) selectModifierFromFlowAdapter(
	adapter *FlowToHashStream,
	targetPOS PartOfSpeech,
	concepts []string,
	used map[string]bool,
) string {
	modPOS := POS_Adjective
	if targetPOS == POS_Verb {
		modPOS = POS_Adverb
	}

	for _, c := range concepts {
		if used[c] {
			continue
		}
		if dg.POSLookup != nil && dg.POSLookup(c) == modPOS {
			return c
		}
	}

	if dg.WordsByPOS != nil {
		words := dg.WordsByPOS(string(modPOS), 20)
		if len(words) > 0 {
			return words[adapter.NextInt(len(words))]
		}
	}

	return ""
}

type FlowGrammarStats struct {
	TotalGenerations uint64
	TotalBytesUsed   uint64
	AvgBytesPerGen   float64
}

type DistributedGrammarV3 struct {
	*DistributedGrammar
	flowCompute FlowComputeInterface
	stats       FlowGrammarStats
	mu          sync.Mutex
}

func NewDistributedGrammarV3(dg *DistributedGrammar, flowCompute FlowComputeInterface) *DistributedGrammarV3 {
	return &DistributedGrammarV3{
		DistributedGrammar: dg,
		flowCompute:        flowCompute,
	}
}

func (dg3 *DistributedGrammarV3) Generate(inputText string, concepts []string, pressure PressureContext) (string, error) {
	result, err := dg3.DistributedGrammar.GenerateFlowPowered(inputText, concepts, pressure, dg3.flowCompute)

	dg3.mu.Lock()
	dg3.stats.TotalGenerations++
	dg3.mu.Unlock()

	return result, err
}

func (dg3 *DistributedGrammarV3) Stats() FlowGrammarStats {
	dg3.mu.Lock()
	defer dg3.mu.Unlock()
	return dg3.stats
}
