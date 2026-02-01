package synthesis

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"zeam/attention"
	"zeam/embed"
	"zeam/grammar"
	"zeam/memory"
	"zeam/ngac"
	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
)

type NGACBackend struct {
	bridge      *NGACBridge
	ngac        *ngac.NGAC
	transformer *quantum.QuantumTransformer
	decoder     *quantum.ContextDecoder

	collapseCompute *quantum.CollapseCompute
	collapseContext *ngac.CollapseContext
	collapseEnabled bool

	hashNetwork *quantum.HashNetwork
	hashBridge  *HashBridge
	hashEnabled bool

	brain        *memory.PositronicBrain
	brainEnabled bool

	liveDispatcher LiveDispatcher

	embeddings      *embed.EmbeddingIndex
	contextWindow   *attention.ContextWindow
	constrained     *ConstrainedDecoder

	flowCompute   FlowComputeInterface
	flowCognition FlowCognitionInterface
	flowEnabled   bool

	l1PeerCount   int
	l1RelayCount  int
	lastBlockHash []byte
	lastBlockTime int64

	pressure quantum.PressureMetrics

	context *ConversationContext

	vocab map[uint32]string

	lastSemanticPath *SemanticPath

	mu sync.RWMutex
}

type FlowComputeInterface interface {
	NextBytes(n int) []byte
	NextUint64() uint64
	SelectIndex(n int) int
}

type FlowCognitionInterface interface {
	Inject(inj FlowInjection)
	Query(content string, concepts []string)
	Steer(concepts []string, strength float64)
	Focus(concepts []string)
	State() FlowCognitiveState
	Dynamics() FlowDynamicsState
}

type FlowInjection struct {
	Type     int
	Content  string
	Concepts []string
	Strength float64
}

type FlowCognitiveState struct {
	ActiveConcepts []string
	ConceptWeights map[string]float64
	ReadyToRespond bool
}

type FlowDynamicsState struct {
	Pressure  float64
	Tension   float64
	Rhythm    float64
	Coherence float64
}

type LiveDispatcher interface {
	Dispatch(ctx context.Context, payload []byte) (*DispatchResult, error)
	SetTimeout(timeout time.Duration)
}

type DispatchResult struct {
	Hashes    []common.Hash
	HashBytes [][]byte
	Duration  time.Duration
}

type ConversationContext struct {

	Topics []string

	RecentWords []string

	Intent string

	TurnCount int

	ConvPressure quantum.PressureMetrics
}

type SemanticPathStep struct {
	FromWord     string `json:"from_word"`
	ToWord       string `json:"to_word"`
	Relation     string `json:"relation"`
	HashByteUsed int    `json:"hash_byte"`
}

type SemanticPath struct {
	InputWords []string           `json:"input_words"`
	Steps      []SemanticPathStep `json:"steps"`
	OutputWords []string          `json:"output_words"`
	Style       int               `json:"style"`
}

func NewConversationContext() *ConversationContext {
	return &ConversationContext{
		Topics:      make([]string, 0),
		RecentWords: make([]string, 0),
		Intent:      "unknown",
		TurnCount:   0,
		ConvPressure: quantum.PressureMetrics{
			Hadamard: 0.5,
			PauliX: 0.5,
			PauliZ:   0.3,
			Phase:   0.5,
		},
	}
}

func DetectIntent(prompt string) string {
	lower := strings.ToLower(prompt)

	if strings.HasSuffix(lower, "?") ||
		strings.HasPrefix(lower, "what ") ||
		strings.HasPrefix(lower, "how ") ||
		strings.HasPrefix(lower, "why ") ||
		strings.HasPrefix(lower, "when ") ||
		strings.HasPrefix(lower, "where ") ||
		strings.HasPrefix(lower, "who ") ||
		strings.HasPrefix(lower, "can ") ||
		strings.HasPrefix(lower, "could ") ||
		strings.HasPrefix(lower, "would ") ||
		strings.HasPrefix(lower, "is ") ||
		strings.HasPrefix(lower, "are ") ||
		strings.HasPrefix(lower, "do ") ||
		strings.HasPrefix(lower, "does ") {
		return "question"
	}

	if strings.HasPrefix(lower, "hello") ||
		strings.HasPrefix(lower, "hi ") ||
		strings.HasPrefix(lower, "hey ") ||
		lower == "hi" || lower == "hello" || lower == "hey" {
		return "greeting"
	}

	if strings.HasPrefix(lower, "tell ") ||
		strings.HasPrefix(lower, "show ") ||
		strings.HasPrefix(lower, "explain ") ||
		strings.HasPrefix(lower, "describe ") ||
		strings.HasPrefix(lower, "define ") ||
		strings.HasPrefix(lower, "list ") {
		return "command"
	}

	return "statement"
}

func NewNGACBackend(oewnPath string) (*NGACBackend, error) {
	bridge, err := NewNGACBridge(oewnPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create NGAC bridge: %w", err)
	}

	vocab := make(map[uint32]string)
	vocab[0] = "<pad>"
	vocab[1] = "<unk>"
	vocab[2] = "<s>"
	vocab[3] = "</s>"

	mesh := bridge.GetMesh()
	cfg := quantum.DefaultTransformerConfig()
	transformer := quantum.NewQuantumTransformer(cfg, mesh)

	ngacInst := bridge.GetNGAC()
	var decoder *quantum.ContextDecoder
	if ngacInst != nil && ngacInst.Dictionary != nil {
		allWords := ngacInst.Dictionary.GetAllWords()
		synonymGroups := buildSynonymGroups(ngacInst)
		decoder = quantum.BuildDecoderFromNGAC(transformer.Substrate, allWords, synonymGroups)
		fmt.Printf("[NGAC-BACKEND] Built decoder with %d words\n", decoder.VocabSize())
	}

	hashNetwork := quantum.NewHashNetwork(4, 256, 4, 512)

	var hashBridge *HashBridge
	if ngacInst != nil {
		hashBridge, err = NewHashBridge(ngacInst, hashNetwork)
		if err != nil {
			fmt.Printf("[NGAC-BACKEND] Warning: failed to create HashBridge: %v\n", err)
		} else {
			fmt.Printf("[NGAC-BACKEND] Created HashNetwork (4 layers, 4 heads) with NGAC bridge\n")
		}
	}

	var embeddings *embed.EmbeddingIndex
	var contextWindow *attention.ContextWindow
	var constrained *ConstrainedDecoder
	if ngacInst != nil && ngacInst.Dictionary != nil {
		embeddings = embed.BuildEmbeddingIndex(ngacInst.Dictionary)
		contextWindow = attention.NewContextWindow(64, embeddings)
		constrained = &ConstrainedDecoder{
			Embeddings: embeddings,
			Context:    contextWindow,
			Dict:       ngacInst.Dictionary,
			Config:     DefaultConstrainedDecoderConfig(),
		}
		fmt.Printf("[NGAC-BACKEND] Built embedding index (%d synsets) + constrained decoder\n", embeddings.Size())
	}

	return &NGACBackend{
		bridge:        bridge,
		ngac:          ngacInst,
		transformer:   transformer,
		decoder:       decoder,
		hashNetwork:   hashNetwork,
		hashBridge:    hashBridge,
		embeddings:    embeddings,
		contextWindow: contextWindow,
		constrained:   constrained,
		context:       NewConversationContext(),
		vocab:         vocab,
		pressure: quantum.PressureMetrics{
			Hadamard: 0.5,
			PauliX: 0.5,
			PauliZ:   0.3,
			Phase:   0.5,
		},
	}, nil
}

func buildSynonymGroups(n *ngac.NGAC) map[string][]string {
	groups := make(map[string][]string)
	if n == nil || n.Dictionary == nil {
		return groups
	}

	dict := n.Dictionary
	for synsetID, synset := range dict.SynsetIndex {
		if len(synset.Members) > 1 {
			groups[synsetID] = synset.Members
		}
	}

	return groups
}

func (b *NGACBackend) SetFlowCognition(compute FlowComputeInterface, cognition FlowCognitionInterface) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.flowCompute = compute
	b.flowCognition = cognition
	b.flowEnabled = compute != nil

	if b.flowEnabled {
		fmt.Printf("[NGAC-BACKEND] Flow cognition enabled - V3 generation active\n")
		fmt.Printf("[NGAC-BACKEND] The network is now continuously thinking\n")
	}
}

func (b *NGACBackend) RegisterWithQuantumService() {
	qs := quantum.GetService()

	if b.transformer != nil {
		qs.SetTransformer(b.transformer)
	}

	qs.OnPressureChange(func(p quantum.PressureMetrics) {
		b.mu.Lock()
		b.pressure = p
		b.mu.Unlock()
	})

	qs.OnResonance(func(r *quantum.ResonanceResult) {
		if r != nil {

			b.mu.Lock()
			b.pressure.PauliX = r.Strength / 10.0
			if b.pressure.PauliX > 1.0 {
				b.pressure.PauliX = 1.0
			}
			b.mu.Unlock()
		}
	})

	fmt.Printf("[NGAC-BACKEND] Registered with unified QuantumService\n")
}

func (b *NGACBackend) GetQuantumPressure() quantum.PressureMetrics {
	return quantum.GetService().GetPressure()
}

func (b *NGACBackend) QuantumSearch(query string) (string, float64) {
	if b.ngac == nil {
		return "", 0
	}
	return b.ngac.SearchWord(query)
}

func (b *NGACBackend) QuantumSemanticSearch(word string, maxResults int) []string {
	if b.ngac == nil {
		return nil
	}
	return b.ngac.SearchSimilar(word, maxResults)
}

func (b *NGACBackend) IsFlowEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.flowEnabled
}

func (b *NGACBackend) GetFlowDynamics() FlowDynamicsState {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.flowCognition != nil {
		return b.flowCognition.Dynamics()
	}
	return FlowDynamicsState{
		Pressure:  0.5,
		Tension:   0.3,
		Rhythm:    0.5,
		Coherence: 0.5,
	}
}

func (b *NGACBackend) SteerCognition(concepts []string, strength float64) {
	b.mu.RLock()
	cog := b.flowCognition
	b.mu.RUnlock()

	if cog != nil {
		cog.Steer(concepts, strength)
	}
}

func (b *NGACBackend) LoadModel(hash []byte) error {
	fmt.Printf("[NGAC-BACKEND] Model: OEWN semantic graph (%d words) + QuantumTransformer (%d layers)\n",
		b.bridge.WordCount(), len(b.transformer.Layers))

	if len(hash) > 0 {
		if err := b.transformer.ImportWeights(hash); err != nil {
			fmt.Printf("[NGAC-BACKEND] Warning: could not import weights: %v\n", err)
		} else {
			fmt.Printf("[NGAC-BACKEND] Imported transformer weights from hash\n")
		}
	}
	return nil
}

func (b *NGACBackend) Forward(tokens []uint32) ([]float32, error) {

	var text strings.Builder
	for _, t := range tokens {
		if word, ok := b.vocab[t]; ok {
			text.WriteString(word)
			text.WriteString(" ")
		}
	}

	_, pressure := b.transformer.Forward(text.String(), 1)

	logits := make([]float32, len(b.vocab)+100)
	for i := range logits {

		logits[i] = float32(pressure.Hadamard*0.5 + pressure.PauliX*0.3 + float64(i%10)*0.02)
	}

	return logits, nil
}

func (b *NGACBackend) ForwardLayer(layer int, hidden []float32) ([]float32, error) {
	if layer >= len(b.transformer.Layers) {
		return hidden, nil
	}

	return hidden, nil
}

func (b *NGACBackend) Sample(logits []float32, temp float32, topP float32, topK int) (uint32, error) {

	livePressure := b.GetQuantumPressure()

	b.mu.Lock()

	b.pressure.Hadamard = livePressure.Hadamard*0.6 + float64(temp)*0.4
	b.pressure.PauliX = livePressure.PauliX*0.6 + float64(1.0-topP)*0.4
	b.pressure.PauliZ = livePressure.PauliZ
	b.pressure.Phase = livePressure.Phase*0.6 + float64(topK)/100.0*0.4
	currentPressure := b.pressure
	b.mu.Unlock()

	bestIdx := uint32(0)
	bestScore := float32(0)
	for i, l := range logits {
		score := l * (1.0 + float32(currentPressure.Hadamard)*0.5)
		if score > bestScore {
			bestScore = score
			bestIdx = uint32(i)
		}
	}

	return bestIdx, nil
}

func (b *NGACBackend) Greedy(logits []float32) uint32 {
	bestIdx := uint32(0)
	bestVal := logits[0]
	for i, v := range logits {
		if v > bestVal {
			bestVal = v
			bestIdx = uint32(i)
		}
	}
	return bestIdx
}

func (b *NGACBackend) Decode(token uint32) string {
	if word, ok := b.vocab[token]; ok {
		return word
	}
	return "<unk>"
}

func (b *NGACBackend) Encode(text string) []uint32 {

	return []uint32{2, 1, 3}
}

func (b *NGACBackend) GenerateText(prompt string, maxTokens int, temp float32, topP float32, topK int) (string, error) {
	b.mu.Lock()
	ctx := b.context
	prevPressure := b.pressure
	b.mu.Unlock()

	inputPressure := b.computeInputPressure(prompt)

	blendedPressure := blendPressure(inputPressure, prevPressure, 0.4)

	blendedPressure.Hadamard = blendedPressure.Hadamard * float64(temp)
	blendedPressure.PauliX = blendedPressure.PauliX * float64(1.0-topP)

	fmt.Printf("[NGAC] Pressure: M=%.2f C=%.2f T=%.2f D=%.2f\n",
		blendedPressure.Hadamard, blendedPressure.PauliX,
		blendedPressure.PauliZ, blendedPressure.Phase)

	_, evolvedPressure := b.transformer.Forward(prompt, maxTokens)

	finalPressure := blendPressure(blendedPressure, evolvedPressure, 0.5)

	var response string

	if b.ngac != nil && b.ngac.Dictionary != nil {
		response = b.generateSemanticResponse(prompt, finalPressure)
	} else {
		response = "fallback"
	}

	if b.contextWindow != nil && b.ngac != nil && b.ngac.Dictionary != nil {
		b.contextWindow.AddTurn(prompt, b.ngac.Dictionary)
		if response != "" {
			b.contextWindow.AddTurn(response, b.ngac.Dictionary)
		}
	}

	b.mu.Lock()
	b.pressure = finalPressure
	ctx.TurnCount++
	ctx.ConvPressure = finalPressure
	b.mu.Unlock()

	fmt.Printf("[NGAC] Response: %q\n", response)
	return response, nil
}

func (b *NGACBackend) computeInputPressure(prompt string) quantum.PressureMetrics {
	lower := strings.ToLower(prompt)
	words := strings.Fields(lower)

	pressure := b.GetQuantumPressure()

	if strings.HasSuffix(prompt, "?") {
		pressure.PauliZ += 0.3
	}

	if strings.HasSuffix(prompt, "!") {
		pressure.Hadamard += 0.3
	}

	pressure.Phase = float64(len(words)) / 20.0
	if pressure.Phase > 1.0 {
		pressure.Phase = 1.0
	}

	for _, word := range words {
		word = strings.Trim(word, ".,?!;:'\"()[]")
		switch word {

		case "urgent", "critical", "emergency", "now", "immediately", "help":
			pressure.Hadamard += 0.2

		case "what", "why", "how", "when", "where", "who":
			pressure.PauliZ += 0.1

		case "status", "info", "tell", "explain", "describe":
			pressure.PauliX += 0.1

		case "hello", "hi", "hey", "greetings":
			pressure.PauliZ -= 0.2
			pressure.PauliX += 0.2
		}
	}

	if pressure.Hadamard > 1.0 {
		pressure.Hadamard = 1.0
	}
	if pressure.PauliX > 1.0 {
		pressure.PauliX = 1.0
	}
	if pressure.PauliZ > 1.0 {
		pressure.PauliZ = 1.0
	}
	if pressure.PauliZ < 0.0 {
		pressure.PauliZ = 0.0
	}

	return pressure
}

func (b *NGACBackend) ConstrainedGenerate(prompt string, pressure quantum.PressureMetrics) (string, error) {
	b.mu.RLock()
	cd := b.constrained
	cw := b.contextWindow
	b.mu.RUnlock()

	if cd == nil {
		return "", fmt.Errorf("constrained decoder not initialized")
	}

	dict := b.ngac.Dictionary
	if dict == nil {
		return "", fmt.Errorf("OEWN dictionary not loaded")
	}

	parser := grammar.NewSentenceParser(
		func(word string) grammar.PartOfSpeech {
			coord := quantum.UTF8_ENCODE(b.ngac.Substrate, word)
			pos := dict.GetPartOfSpeech(coord)
			switch pos {
			case "n":
				return grammar.POS_Noun
			case "v":
				return grammar.POS_Verb
			case "a":
				return grammar.POS_Adjective
			case "r":
				return grammar.POS_Adverb
			default:
				return grammar.POS_Noun
			}
		},
		func(word string) []string {
			return dict.GetSynsetsForWord(word)
		},
	)
	parsed := parser.Parse(prompt)

	if cw != nil {
		cw.AddTurn(prompt, dict)
	}

	promptHash := sha256.Sum256([]byte(prompt))
	seed := promptHash[:]

	response := cd.GenerateConstrained(parsed, pressure, seed)

	if cw != nil && response != "" {
		cw.AddTurn(response, dict)
	}

	if response == "" {
		return "", fmt.Errorf("constrained decoder produced empty response")
	}

	return response, nil
}

func (b *NGACBackend) gatherRelatedConcepts(synsets []string, dict *ngac.SubstrateDictionary, maxWords int, pressure quantum.PressureMetrics) string {
	if dict == nil || len(synsets) == 0 {
		return ""
	}

	seen := make(map[string]bool)
	var words []string

	for _, synsetID := range synsets {
		synset := dict.GetSynsetByID(synsetID)
		if synset == nil {
			continue
		}

		for _, member := range synset.Members {
			if !seen[member] && len(words) < maxWords {

				if pressure.PauliX > 0.5 && len(member) > 12 {
					continue
				}
				words = append(words, member)
				seen[member] = true
			}
		}

		if len(words) >= maxWords {
			break
		}
	}

	if len(words) == 0 {
		return ""
	}

	return strings.Join(words, ", ")
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isStopWord(word string) bool {
	stops := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"into": true, "through": true, "during": true, "before": true,
		"after": true, "above": true, "below": true, "between": true,
		"and": true, "but": true, "or": true, "nor": true, "so": true,
		"yet": true, "both": true, "either": true, "neither": true,
		"not": true, "only": true, "own": true, "same": true, "than": true,
		"too": true, "very": true, "just": true, "also": true,
		"i": true, "you": true, "he": true, "she": true, "it": true,
		"we": true, "they": true, "me": true, "him": true, "her": true,
		"us": true, "them": true, "my": true, "your": true, "his": true,
		"its": true, "our": true, "their": true, "this": true, "that": true,
		"these": true, "those": true, "what": true, "which": true, "who": true,
		"whom": true, "whose": true, "where": true, "when": true, "why": true,
		"how": true, "all": true, "each": true, "every": true, "any": true,
		"some": true, "no": true, "none": true, "one": true, "two": true,
		"hello": true, "hi": true, "hey": true,
	}
	return stops[word]
}

func assembleSemanticResponse(words []string, concepts []string, pressure quantum.PressureMetrics, dict *ngac.SubstrateDictionary) string {
	if len(words) == 0 {
		return ""
	}

	var parts []string

	if pressure.PauliX > 0.6 && len(concepts) > 0 {

		parts = append(parts, fmt.Sprintf("Regarding %s:", concepts[0]))
	}

	for i := 0; i < len(words); i += 3 {
		end := i + 3
		if end > len(words) {
			end = len(words)
		}
		chunk := words[i:end]
		parts = append(parts, strings.Join(chunk, ", "))
	}

	response := strings.Join(parts, " ")

	return formatSentence(response)
}

func assembleResponse(words []string, n *ngac.NGAC, pressure quantum.PressureMetrics) string {
	if len(words) == 0 {
		return ""
	}

	if n != nil && n.Dictionary != nil && n.Substrate != nil {
		response := ngac.ApplySimpleGrammar(words, n.Dictionary, n.Substrate)
		return formatSentence(response)
	}

	return formatSentence(strings.Join(words, " "))
}

func (b *NGACBackend) assembleGrammaticalResponse(prompt string, concepts []string, pressure quantum.PressureMetrics, hashSeed *big.Int) string {
	if len(concepts) == 0 {
		return ""
	}

	dict := b.ngac.Dictionary
	if dict == nil {
		return formatSentence(strings.Join(concepts, " "))
	}

	parser := grammar.NewSentenceParser(
		func(word string) grammar.PartOfSpeech {

			coord := quantum.UTF8_ENCODE(b.ngac.Substrate, word)
			pos := dict.GetPartOfSpeech(coord)
			switch pos {
			case "n":
				return grammar.POS_Noun
			case "v":
				return grammar.POS_Verb
			case "a":
				return grammar.POS_Adjective
			case "r":
				return grammar.POS_Adverb
			default:
				return grammar.POS_Noun
			}
		},
		func(word string) []string {
			return dict.GetSynsetsForWord(word)
		},
	)

	parsed := parser.Parse(prompt)

	builder := grammar.NewSentenceBuilder()

	builder.GetWordsByPOS = func(pos string) []string {
		return dict.GetWordsByPOS(pos, 100)
	}

	builder.GetPOS = func(word string) string {
		coord := quantum.UTF8_ENCODE(b.ngac.Substrate, word)
		return dict.GetPartOfSpeech(coord)
	}

	builder.GetSynsets = func(word string) []string {
		return dict.GetSynsetsForWord(word)
	}

	builder.GetSynsetByID = func(id string) *grammar.SynsetInfo {
		synset := dict.GetSynsetByID(id)
		if synset == nil {
			return nil
		}
		return &grammar.SynsetInfo{
			ID:          synset.SynsetId,
			Type:        synset.Type,
			Members:     synset.Members,
			Definitions: synset.Definitions,
			Domain:      synset.Domain,
		}
	}

	builder.GetDefinition = func(word string) string {
		coord := quantum.UTF8_ENCODE(b.ngac.Substrate, word)
		return dict.GetDefinition(coord)
	}

	builder.GetSynonyms = func(word string) []string {
		coord := quantum.UTF8_ENCODE(b.ngac.Substrate, word)
		return dict.GetSynonyms(coord)
	}

	builder.HashFunc = quantum.FeistelHash

	grammarPressure := grammar.PressureContext{
		Hadamard: pressure.Hadamard,
		PauliX: pressure.PauliX,
		PauliZ:   pressure.PauliZ,
		Phase:   pressure.Phase,
	}

	if parsed.Intent == "greeting" {
		return grammar.GetSimpleResponse("greeting", hashSeed)
	}

	response := builder.BuildResponse(parsed, concepts, grammarPressure, hashSeed)

	if response == "" {

		response = builder.GenerateMultiSentence(parsed, concepts, grammarPressure, hashSeed, 2)
	}

	if response == "" {

		return formatSentence(strings.Join(concepts, ", "))
	}

	return response
}

func (b *NGACBackend) assembleDistributedGrammar(prompt string, concepts []string, pressure quantum.PressureMetrics) (string, error) {
	if len(concepts) == 0 {
		return "", nil
	}

	dg := grammar.NewDistributedGrammar(func(payload []byte) ([][]byte, error) {

		if b.liveDispatcher != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := b.liveDispatcher.Dispatch(ctx, payload)
			if err != nil {
				return nil, err
			}
			return result.HashBytes, nil
		}

		return b.localGrammarDispatch(payload), nil
	})

	dict := b.ngac.Dictionary
	if dict != nil {
		dg.POSLookup = func(word string) grammar.PartOfSpeech {
			coord := quantum.UTF8_ENCODE(b.ngac.Substrate, word)
			pos := dict.GetPartOfSpeech(coord)
			switch pos {
			case "n":
				return grammar.POS_Noun
			case "v":
				return grammar.POS_Verb
			case "a":
				return grammar.POS_Adjective
			case "r":
				return grammar.POS_Adverb
			default:
				return ""
			}
		}

		dg.WordsByPOS = func(pos string, limit int) []string {
			return dict.GetWordsByPOS(pos, limit)
		}

		dg.SynsetLookup = func(word string) []string {
			return dict.GetSynsetsForWord(word)
		}

		dg.SynsetByID = func(id string) *grammar.SynsetInfo {
			synset := dict.GetSynsetByID(id)
			if synset == nil {
				return nil
			}
			return &grammar.SynsetInfo{
				ID:          synset.SynsetId,
				Type:        synset.Type,
				Members:     synset.Members,
				Definitions: synset.Definitions,
				Domain:      synset.Domain,
			}
		}

		dg.GetSynonyms = func(word string) []string {
			coord := quantum.UTF8_ENCODE(b.ngac.Substrate, word)
			return dict.GetSynonyms(coord)
		}

		dg.GetDefinition = func(word string) string {
			coord := quantum.UTF8_ENCODE(b.ngac.Substrate, word)
			return dict.GetDefinition(coord)
		}
	}

	var grammarPressure grammar.PressureContext

	flowEnabled := b.flowEnabled
	flowCompute := b.flowCompute
	flowCognition := b.flowCognition

	if flowEnabled && flowCognition != nil {

		dynamics := flowCognition.Dynamics()
		quantumPressure := b.GetQuantumPressure()

		grammarPressure = grammar.PressureContext{
			Hadamard: dynamics.Pressure*0.5 + quantumPressure.Hadamard*0.5,
			PauliX:   dynamics.Coherence*0.5 + quantumPressure.PauliX*0.5,
			PauliZ:   dynamics.Tension*0.5 + quantumPressure.PauliZ*0.5,
			Phase:    dynamics.Rhythm*0.5 + quantumPressure.Phase*0.5,
		}

		flowCognition.Steer(concepts, 0.6)

		fmt.Printf("[NGAC-BACKEND] V3 Flow+Quantum: H=%.2f X=%.2f Z=%.2f P=%.2f\n",
			grammarPressure.Hadamard, grammarPressure.PauliX, grammarPressure.PauliZ, grammarPressure.Phase)
	} else {
		grammarPressure = grammar.PressureContext{
			Hadamard: pressure.Hadamard,
			PauliX: pressure.PauliX,
			PauliZ:   pressure.PauliZ,
			Phase:   pressure.Phase,
		}
	}

	type genResult struct {
		text string
		err  error
	}
	resultCh := make(chan genResult, 1)

	go func() {
		var text string
		var err error
		if flowEnabled && flowCompute != nil {

			text, err = dg.GenerateFlowPowered(prompt, concepts, grammarPressure, flowCompute)
		} else {

			text, err = dg.GenerateDistributedV2(prompt, concepts, grammarPressure)
		}
		resultCh <- genResult{text, err}
	}()

	select {
	case result := <-resultCh:
		return result.text, result.err
	case <-time.After(10 * time.Second):
		fmt.Println("[NGAC-BACKEND] Warning: Grammar generation timed out after 10s")
		return "Generation timed out.", fmt.Errorf("grammar generation timeout")
	}
}

func (b *NGACBackend) localGrammarDispatch(payload []byte) [][]byte {

	hashes := make([][]byte, 4)

	current := sha256.Sum256(payload)
	hashes[0] = current[:]

	for i := 1; i < 4; i++ {
		current = sha256.Sum256(current[:])
		hashes[i] = current[:]
	}

	return hashes
}

func formatSentence(text string) string {
	if len(text) == 0 {
		return ""
	}

	text = strings.ToUpper(string(text[0])) + text[1:]

	if !strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "!") && !strings.HasSuffix(text, "?") {
		text += "."
	}

	return text
}

func (b *NGACBackend) generateSemanticResponse(prompt string, pressure quantum.PressureMetrics) string {
	dict := b.ngac.Dictionary

	words := strings.Fields(strings.ToLower(prompt))
	contentWords := make([]string, 0)
	for _, word := range words {
		word = strings.Trim(word, ".,?!;:'\"()[]")
		if !isStopWord(word) && len(word) > 2 {
			contentWords = append(contentWords, word)
		}
	}

	inputSynsets := make(map[string]*ngac.Synset)
	inputConcepts := make([]string, 0)

	for _, word := range contentWords {
		synsetIDs := dict.GetSynsetsForWord(word)
		for _, synsetID := range synsetIDs {
			synset := dict.GetSynsetByID(synsetID)
			if synset != nil {
				inputSynsets[synsetID] = synset

				if !contains(inputConcepts, word) {
					inputConcepts = append(inputConcepts, word)
				}
			}
		}
	}

	intent := DetectIntent(prompt)

	responseWords := make([]string, 0)

	switch intent {
	case "greeting":

		responseWords = b.navigateToSocialResponse(inputSynsets, pressure, dict)

	case "question":

		responseWords = b.navigateToExplanation(inputSynsets, inputConcepts, pressure, dict)

	case "command":

		responseWords = b.navigateToAction(inputSynsets, inputConcepts, pressure, dict)

	default:

		responseWords = b.navigateToAcknowledgment(inputSynsets, inputConcepts, pressure, dict)
	}

	if len(responseWords) == 0 {
		responseWords = b.generatePressureResponse(intent, pressure, dict)
	}

	if len(responseWords) == 0 {
		return "Observing. Awaiting further input."
	}

	fmt.Printf("[NGAC] liveDispatcher=%v, responseWords=%v\n", b.liveDispatcher != nil, responseWords)
	if b.liveDispatcher != nil {
		distributedResponse, err := b.assembleDistributedGrammar(prompt, responseWords, pressure)
		fmt.Printf("[NGAC] distributedResponse=%q, err=%v\n", distributedResponse, err)
		if err == nil && distributedResponse != "" {
			return distributedResponse
		}

		if err != nil {
			fmt.Printf("[NGAC] Distributed grammar failed: %v, using local\n", err)
		}
	}

	promptHash := sha256.Sum256([]byte(prompt))
	hashSeed := new(big.Int).SetBytes(promptHash[:16])

	grammaticalResponse := b.assembleGrammaticalResponse(prompt, responseWords, pressure, hashSeed)
	fmt.Printf("[NGAC] grammaticalResponse=%q\n", grammaticalResponse)
	if grammaticalResponse != "" {
		return grammaticalResponse
	}

	fmt.Printf("[NGAC] Falling back to assembleResponse\n")
	return assembleResponse(responseWords, b.ngac, pressure)
}

func (b *NGACBackend) navigateToSocialResponse(synsets map[string]*ngac.Synset, pressure quantum.PressureMetrics, dict *ngac.SubstrateDictionary) []string {
	words := make([]string, 0)

	greetingSynsets := dict.GetSynsetsForWord("greeting")
	for _, synsetID := range greetingSynsets {
		synset := dict.GetSynsetByID(synsetID)
		if synset != nil && len(synset.Members) > 0 {
			words = append(words, synset.Members[0])
			break
		}
	}

	for _, synset := range synsets {
		if len(synset.Members) > 0 {
			for _, member := range synset.Members {
				if !contains(words, member) {
					words = append(words, member)
					if len(words) >= 3 {
						break
					}
				}
			}
		}
		if len(words) >= 3 {
			break
		}
	}

	return words
}

func (b *NGACBackend) navigateToExplanation(synsets map[string]*ngac.Synset, concepts []string, pressure quantum.PressureMetrics, dict *ngac.SubstrateDictionary) []string {
	words := make([]string, 0)

	for _, synset := range synsets {

		if len(synset.Definitions) > 0 {

			defWords := strings.Fields(synset.Definitions[0])
			for _, dw := range defWords {
				dw = strings.Trim(dw, ".,;:'\"()[]")
				if !isStopWord(dw) && len(dw) > 2 {
					words = append(words, dw)

					if len(words) >= int(5+pressure.PauliX*5) {
						break
					}
				}
			}
		}

		if pressure.Phase > 0.5 && len(synset.Members) > 1 {
			for i, member := range synset.Members {
				if i > 0 && i <= int(pressure.Phase*3)+1 {
					if !contains(words, member) {
						words = append(words, member)
					}
				}
			}
		}

		if len(words) >= int(8+pressure.Hadamard*8) {
			break
		}
	}

	return words
}

func (b *NGACBackend) navigateToAction(synsets map[string]*ngac.Synset, concepts []string, pressure quantum.PressureMetrics, dict *ngac.SubstrateDictionary) []string {
	words := make([]string, 0)

	for _, synset := range synsets {

		if synset.Type == "v" {
			for _, member := range synset.Members {
				if !contains(words, member) {
					words = append(words, member)
				}
				if len(words) >= 3 {
					break
				}
			}
		}

		if len(synset.Definitions) > 0 {
			defWords := strings.Fields(synset.Definitions[0])
			for _, dw := range defWords {
				dw = strings.Trim(dw, ".,;:'\"()[]")
				if !isStopWord(dw) && len(dw) > 2 && !contains(words, dw) {
					words = append(words, dw)
					if len(words) >= 5 {
						break
					}
				}
			}
		}
		if len(words) >= 5 {
			break
		}
	}

	return words
}

func (b *NGACBackend) navigateToAcknowledgment(synsets map[string]*ngac.Synset, concepts []string, pressure quantum.PressureMetrics, dict *ngac.SubstrateDictionary) []string {
	words := make([]string, 0)

	for _, synset := range synsets {

		for _, member := range synset.Members {
			if !contains(concepts, member) && !contains(words, member) {
				words = append(words, member)
				if len(words) >= 3 {
					break
				}
			}
		}

		if len(synset.Definitions) > 0 && len(words) < 5 {
			defWords := strings.Fields(synset.Definitions[0])
			for _, dw := range defWords {
				dw = strings.Trim(dw, ".,;:'\"()[]")
				if !isStopWord(dw) && len(dw) > 2 && !contains(words, dw) {
					words = append(words, dw)
					if len(words) >= 5 {
						break
					}
				}
			}
		}
		if len(words) >= 5 {
			break
		}
	}

	return words
}

func (b *NGACBackend) generatePressureResponse(intent string, pressure quantum.PressureMetrics, dict *ngac.SubstrateDictionary) []string {
	words := make([]string, 0)

	var seedTerms []string
	switch intent {
	case "question":

		seedTerms = []string{"knowledge", "understanding", "meaning", "concept", "inquiry", "thought"}
	case "greeting":
		seedTerms = []string{"greeting", "salutation", "welcome", "acknowledgment"}
	case "command":
		seedTerms = []string{"action", "process", "operation", "task", "execute"}
	default:

		seedTerms = []string{"observation", "perception", "awareness", "consideration", "reflection"}
	}

	for _, term := range seedTerms {
		synsetIDs := dict.GetSynsetsForWord(term)
		for _, synsetID := range synsetIDs {
			synset := dict.GetSynsetByID(synsetID)
			if synset != nil {

				memberCount := int(1 + pressure.Hadamard*2)
				for i, member := range synset.Members {
					if i < memberCount && !contains(words, member) {
						words = append(words, member)
					}
				}

				if len(synset.Definitions) > 0 && pressure.PauliX > 0.3 {
					defWords := strings.Fields(synset.Definitions[0])
					for _, dw := range defWords {
						dw = strings.Trim(dw, ".,;:'\"()[]")
						if !isStopWord(dw) && len(dw) > 3 && !contains(words, dw) {
							words = append(words, dw)
							if len(words) >= int(5+pressure.PauliX*5) {
								break
							}
						}
					}
				}
			}
			if len(words) >= 6 {
				break
			}
		}
		if len(words) >= 6 {
			break
		}
	}

	return words
}

func (b *NGACBackend) OnChainEvent(eventType string, amount float64, metadata string) {

	b.bridge.OnChainEvent(eventType, amount, metadata)

	b.transformer.OnChainEvent(eventType, amount)

	b.mu.Lock()
	b.pressure = b.bridge.GetPressure()
	b.mu.Unlock()
}

func (b *NGACBackend) GetPressure() quantum.PressureMetrics {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pressure
}

func (b *NGACBackend) AddPressure(source string, amount float64) {

	eventType := strings.ToUpper(source)
	b.OnChainEvent(eventType, amount, "")
}

func (b *NGACBackend) GetNGAC() *ngac.NGAC {
	return b.ngac
}

func (b *NGACBackend) GetBridge() *NGACBridge {
	return b.bridge
}

func (b *NGACBackend) GetTransformer() *quantum.QuantumTransformer {
	return b.transformer
}

func (b *NGACBackend) GetSemanticPath() *SemanticPath {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastSemanticPath
}

func (b *NGACBackend) WordCount() int {
	return b.bridge.WordCount()
}

func (b *NGACBackend) ExportWeights() []byte {
	return b.transformer.ExportWeights()
}

func (b *NGACBackend) ImportWeights(data []byte) error {
	return b.transformer.ImportWeights(data)
}

func (b *NGACBackend) UpdateNetworkState(peerCount, relayCount int, blockHash []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.l1PeerCount = peerCount
	b.l1RelayCount = relayCount
	if len(blockHash) > 0 {
		b.lastBlockHash = blockHash
		b.lastBlockTime = time.Now().Unix()
	}

}

func (b *NGACBackend) GetNetworkState() (peers, relays int, blockAge int64) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	age := int64(0)
	if b.lastBlockTime > 0 {
		age = time.Now().Unix() - b.lastBlockTime
	}
	return b.l1PeerCount, b.l1RelayCount, age
}

func (b *NGACBackend) GetLayerStats() []map[string]interface{} {
	return b.transformer.GetLayerStats()
}

func (b *NGACBackend) EnableCollapseCompute(chainID *big.Int, sender common.Address) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.collapseCompute = quantum.NewCollapseCompute(chainID, sender)
	b.collapseContext = ngac.NewCollapseContext()
	b.collapseEnabled = true

	fmt.Printf("[NGAC] Collapse compute enabled - L1 nodes will perform hash computation\n")
}

func (b *NGACBackend) SetCollapseNonce(nonce uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.collapseCompute != nil {
		b.collapseCompute.SetNonceBase(nonce)
	}
}

func (b *NGACBackend) SetLiveDispatcher(dispatcher LiveDispatcher) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.liveDispatcher = dispatcher
}

func (b *NGACBackend) CollapseGenerate(prompt string) (string, common.Hash, error) {

	b.mu.RLock()
	collapseEnabled := b.collapseEnabled
	collapseCompute := b.collapseCompute
	dict := b.ngac.Dictionary
	flowEnabled := b.flowEnabled
	flowCompute := b.flowCompute
	liveDispatcher := b.liveDispatcher
	collapseCtx := []byte{}
	if b.collapseContext != nil {
		collapseCtx = make([]byte, len(b.collapseContext.AccumulatedHash))
		copy(collapseCtx, b.collapseContext.AccumulatedHash)
	}
	b.mu.RUnlock()

	if !collapseEnabled || collapseCompute == nil {
		return "", common.Hash{}, fmt.Errorf("collapse compute not enabled")
	}

	inputWords := strings.Fields(strings.ToLower(prompt))
	inputSynsets := make(map[string]*ngac.Synset)
	for _, word := range inputWords {
		word = strings.Trim(word, ".,?!;:'\"()[]")
		if !isStopWord(word) && len(word) > 2 {
			synsetIDs := dict.GetSynsetsForWord(word)
			for _, synsetID := range synsetIDs {
				synset := dict.GetSynsetByID(synsetID)
				if synset != nil {
					inputSynsets[synsetID] = synset
				}
			}
		}
	}

	var txHash common.Hash
	var allHashBytes [][]byte

	if flowEnabled && flowCompute != nil {

		flowBytes := flowCompute.NextBytes(32)
		if len(flowBytes) >= 32 {

			combined := make([]byte, 32)
			copy(combined, flowBytes)
			promptBytes := []byte(prompt)
			for i := 0; i < len(promptBytes) && i < 32; i++ {
				combined[i] ^= promptBytes[i]
			}
			for i := 0; i < len(collapseCtx) && i < 32; i++ {
				combined[i] ^= collapseCtx[i]
			}
			txHash = common.BytesToHash(combined)
			allHashBytes = [][]byte{txHash.Bytes()}

			if flowCompute.NextUint64()%100 == 0 {
				fmt.Println("[NGAC] V3 flow mode - harvesting mempool entropy")
			}
		} else {

			txHash = b.collapseHashWithNetwork(prompt, collapseCtx)
			allHashBytes = [][]byte{txHash.Bytes()}
		}
	} else if liveDispatcher != nil {

		fmt.Println("[NGAC] Using V2 live dispatch mode")
		payload := b.encodeCollapsePayload(prompt, collapseCtx)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := liveDispatcher.Dispatch(ctx, payload)
		if err != nil {
			fmt.Printf("[NGAC] Live dispatch failed, falling back to local: %v\n", err)
			txHash = b.collapseHashWithNetwork(prompt, collapseCtx)
			allHashBytes = [][]byte{txHash.Bytes()}
		} else {
			if len(result.HashBytes) > 0 {
				combined := combineNetworkHashes(result.HashBytes)
				txHash = common.BytesToHash(combined)
				allHashBytes = result.HashBytes
				fmt.Printf("[NGAC] Live dispatch: %d hashes from network in %v\n",
					len(result.Hashes), result.Duration)
			} else {
				fmt.Println("[NGAC] Live dispatch returned 0 hashes, using local fallback")
				txHash = b.collapseHashWithNetwork(prompt, collapseCtx)
				allHashBytes = [][]byte{txHash.Bytes()}
			}
		}
	} else {

		fmt.Println("[NGAC] Using fallback mode (network state entropy)")
		txHash = b.collapseHashWithNetwork(prompt, collapseCtx)
		allHashBytes = [][]byte{txHash.Bytes()}
	}

	hashBytes := txHash.Bytes()

	var words []string
	intent := DetectIntent(prompt)

	if len(inputSynsets) > 0 {

		words = b.collapseNavigate(inputSynsets, hashBytes, intent, dict)
	} else {

		words = b.collapseFromIntent(intent, hashBytes, dict)
	}

	if len(allHashBytes) > 1 {
		for i := 1; i < len(allHashBytes) && len(words) < 12; i++ {
			extraWords := b.collapseFromIntent(intent, allHashBytes[i], dict)
			for _, w := range extraWords {
				if !contains(words, w) {
					words = append(words, w)
					if len(words) >= 12 {
						break
					}
				}
			}
		}
	}

	b.mu.Lock()
	if b.collapseContext != nil {
		b.collapseContext.AddCollapse(dict, hashBytes)
	}
	b.mu.Unlock()

	var response string
	if len(words) > 0 {

		promptHash := sha256.Sum256([]byte(prompt))
		hashSeed := new(big.Int).SetBytes(promptHash[:16])

		pressure := b.computeInputPressure(prompt)

		if b.liveDispatcher != nil {
			distributedResponse, err := b.assembleDistributedGrammar(prompt, words, pressure)
			if err == nil && distributedResponse != "" {
				response = distributedResponse
			}
		}

		if response == "" {
			response = b.assembleGrammaticalResponse(prompt, words, pressure, hashSeed)
		}

		if response == "" {
			response = assembleResponse(words, b.ngac, pressure)
		}
	} else {
		response = "Processing query through semantic field."
	}

	b.mu.Lock()
	if b.contextWindow != nil && b.ngac != nil && b.ngac.Dictionary != nil {
		b.contextWindow.AddTurn(prompt, b.ngac.Dictionary)
		if response != "" {
			b.contextWindow.AddTurn(response, b.ngac.Dictionary)
		}
	}
	b.mu.Unlock()

	fmt.Printf("[NGAC] Collapse: %s -> %s (hash: %s, network_hashes: %d)\n",
		truncate(prompt, 30), truncate(response, 30), txHash.Hex()[:16], len(allHashBytes))

	return response, txHash, nil
}

func (b *NGACBackend) collapseNavigate(inputSynsets map[string]*ngac.Synset, hashBytes []byte, intent string, dict *ngac.SubstrateDictionary) []string {
	words := make([]string, 0)
	seen := make(map[string]bool)

	path := &SemanticPath{
		InputWords: make([]string, 0),
		Steps:      make([]SemanticPathStep, 0),
	}

	for _, synset := range inputSynsets {
		if len(synset.Members) > 0 {
			path.InputWords = append(path.InputWords, synset.Members[0])
		}
	}

	navStyle := int(hashBytes[0]) % 3
	path.Style = navStyle

	relationNames := []string{"synonym", "definition", "hypernym"}

	for _, synset := range inputSynsets {
		fromWord := ""
		if len(synset.Members) > 0 {
			fromWord = synset.Members[0]
		}

		switch navStyle {
		case 0:
			memberIdx := int(hashBytes[1]) % len(synset.Members)
			if memberIdx < len(synset.Members) {
				word := synset.Members[memberIdx]
				if !seen[word] && !isStopWord(word) {
					words = append(words, word)
					seen[word] = true

					path.Steps = append(path.Steps, SemanticPathStep{
						FromWord:     fromWord,
						ToWord:       word,
						Relation:     relationNames[navStyle],
						HashByteUsed: int(hashBytes[1]),
					})
				}
			}

		case 1:
			if len(synset.Definitions) > 0 {
				defWords := strings.Fields(synset.Definitions[0])

				startIdx := int(hashBytes[2]) % max(1, len(defWords))
				for i := startIdx; i < len(defWords) && len(words) < 6; i++ {
					dw := strings.Trim(defWords[i], ".,;:'\"()[]")
					if !isStopWord(dw) && len(dw) > 2 && !seen[dw] {
						words = append(words, dw)
						seen[dw] = true

						path.Steps = append(path.Steps, SemanticPathStep{
							FromWord:     fromWord,
							ToWord:       dw,
							Relation:     relationNames[navStyle],
							HashByteUsed: int(hashBytes[2]),
						})
					}
				}
			}

		case 2:

			if len(synset.Definitions) > 0 && len(synset.Members) > 0 {

				word := synset.Members[0]
				if !seen[word] {
					words = append(words, word)
					seen[word] = true
					path.Steps = append(path.Steps, SemanticPathStep{
						FromWord:     fromWord,
						ToWord:       word,
						Relation:     "synonym",
						HashByteUsed: 0,
					})
				}

				defWords := strings.Fields(synset.Definitions[0])
				for i := 0; i < 3 && i < len(defWords); i++ {
					dw := strings.Trim(defWords[i], ".,;:'\"()[]")
					if !isStopWord(dw) && len(dw) > 2 && !seen[dw] {
						words = append(words, dw)
						seen[dw] = true
						path.Steps = append(path.Steps, SemanticPathStep{
							FromWord:     word,
							ToWord:       dw,
							Relation:     relationNames[navStyle],
							HashByteUsed: i,
						})
					}
				}
			}
		}

		if len(words) >= 5 {
			break
		}
	}

	path.OutputWords = words
	b.mu.Lock()
	b.lastSemanticPath = path
	b.mu.Unlock()

	return words
}

func (b *NGACBackend) collapseFromIntent(intent string, hashBytes []byte, dict *ngac.SubstrateDictionary) []string {
	words := make([]string, 0)

	var seedTerms []string
	switch intent {
	case "greeting":
		seedTerms = []string{"welcome", "greet", "acknowledge", "respond"}
	case "question":
		seedTerms = []string{"consider", "examine", "understand", "explain", "know"}
	case "command":
		seedTerms = []string{"execute", "perform", "process", "complete"}
	default:
		seedTerms = []string{"observe", "note", "acknowledge", "process"}
	}

	seedIdx := int(hashBytes[0]) % len(seedTerms)
	synsetIDs := dict.GetSynsetsForWord(seedTerms[seedIdx])

	for _, synsetID := range synsetIDs {
		synset := dict.GetSynsetByID(synsetID)
		if synset != nil {

			if len(synset.Members) > 0 {
				memberIdx := int(hashBytes[1]) % len(synset.Members)
				words = append(words, synset.Members[memberIdx])
			}

			if len(synset.Definitions) > 0 {
				defWords := strings.Fields(synset.Definitions[0])
				for i, dw := range defWords {
					if i > 3 {
						break
					}
					dw = strings.Trim(dw, ".,;:'\"()[]")
					if !isStopWord(dw) && len(dw) > 3 {
						words = append(words, dw)
					}
				}
			}
			break
		}
	}

	return words
}

func (b *NGACBackend) collapseHashWithNetwork(prompt string, context []byte) common.Hash {

	data := make([]byte, 0, len(prompt)+len(context)+32)
	data = append(data, 0x02)
	data = append(data, []byte(prompt)...)
	data = append(data, context...)

	data = append(data, byte(b.l1PeerCount))
	data = append(data, byte(b.l1RelayCount))

	if len(b.lastBlockHash) > 0 {
		data = append(data, b.lastBlockHash...)
	}

	ts := time.Now().Unix()
	data = append(data, byte(ts), byte(ts>>8), byte(ts>>16), byte(ts>>24))

	data = append(data, byte(b.pressure.Hadamard*255))
	data = append(data, byte(b.pressure.PauliX*255))
	data = append(data, byte(b.pressure.PauliZ*255))
	data = append(data, byte(b.pressure.Phase*255))

	hash := sha256.Sum256(data)
	return common.BytesToHash(hash[:])
}

func (b *NGACBackend) encodeCollapsePayload(prompt string, collapseCtx []byte) []byte {
	promptBytes := []byte(prompt)
	payload := make([]byte, 0, 7+len(collapseCtx)+len(promptBytes))

	payload = append(payload, 0x03)

	payload = append(payload, byte(b.pressure.Hadamard*255))
	payload = append(payload, byte(b.pressure.PauliX*255))
	payload = append(payload, byte(b.pressure.PauliZ*255))
	payload = append(payload, byte(b.pressure.Phase*255))

	payload = append(payload, byte(len(collapseCtx)>>8), byte(len(collapseCtx)))
	payload = append(payload, collapseCtx...)

	payload = append(payload, promptBytes...)

	return payload
}

func combineNetworkHashes(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return make([]byte, 32)
	}
	if len(hashes) == 1 {
		return hashes[0]
	}

	combined := make([]byte, 32)
	copy(combined, hashes[0])

	for i := 1; i < len(hashes); i++ {
		for j := 0; j < 32 && j < len(hashes[i]); j++ {
			combined[j] ^= hashes[i][j]
		}
	}

	final := sha256.Sum256(combined)
	return final[:]
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func (b *NGACBackend) CollapseChainGenerate(prompts []string) (string, []common.Hash, error) {

	b.mu.RLock()
	collapseEnabled := b.collapseEnabled
	flowEnabled := b.flowEnabled
	flowCompute := b.flowCompute
	liveDispatcher := b.liveDispatcher
	dict := b.ngac.Dictionary
	ngacInst := b.ngac
	pressure := b.pressure
	b.mu.RUnlock()

	if !collapseEnabled {
		return "", nil, fmt.Errorf("collapse compute not enabled")
	}

	hashes := make([]common.Hash, 0, len(prompts))
	allWords := make([]string, 0)

	chainContext := []byte{}
	for _, prompt := range prompts {
		var txHash common.Hash

		if flowEnabled && flowCompute != nil {

			flowBytes := flowCompute.NextBytes(32)
			if len(flowBytes) >= 32 {
				combined := make([]byte, 32)
				copy(combined, flowBytes)
				promptBytes := []byte(prompt)
				for i := 0; i < len(promptBytes) && i < 32; i++ {
					combined[i] ^= promptBytes[i]
				}
				for i := 0; i < len(chainContext) && i < 32; i++ {
					combined[i] ^= chainContext[i]
				}
				txHash = common.BytesToHash(combined)
			} else {
				txHash = b.collapseHashWithNetwork(prompt, chainContext)
			}
		} else if liveDispatcher != nil {

			payload := b.encodeCollapsePayload(prompt, chainContext)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			result, err := liveDispatcher.Dispatch(ctx, payload)
			cancel()

			if err != nil || len(result.HashBytes) == 0 {
				txHash = b.collapseHashWithNetwork(prompt, chainContext)
			} else {
				combinedHash := combineNetworkHashes(result.HashBytes)
				txHash = common.BytesToHash(combinedHash)
			}
		} else {

			txHash = b.collapseHashWithNetwork(prompt, chainContext)
		}

		hashes = append(hashes, txHash)

		if dict != nil {
			words := dict.CollapseToWords(txHash.Bytes())
			allWords = append(allWords, words...)
		}

		chainContext = append(chainContext, txHash.Bytes()...)
	}

	var response string
	if len(allWords) > 0 {
		response = assembleResponse(allWords, ngacInst, pressure)
	} else {
		response = "chain-collapse"
	}

	return response, hashes, nil
}

func (b *NGACBackend) GetCollapseContext() *ngac.CollapseContext {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.collapseContext
}

func (b *NGACBackend) ResetCollapseContext() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.collapseContext = ngac.NewCollapseContext()
}

func (b *NGACBackend) IsCollapseEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.collapseEnabled
}

func (b *NGACBackend) EnableHashNetwork() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hashBridge == nil {
		return fmt.Errorf("HashBridge not initialized")
	}

	b.hashEnabled = true
	fmt.Printf("[NGAC] Hash-native network enabled\n")
	return nil
}

func (b *NGACBackend) SetHashDispatcher(dispatcher quantum.HashDispatcher) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hashNetwork != nil {
		b.hashNetwork.SetDispatcher(dispatcher)
		fmt.Printf("[NGAC] Hash-native network dispatcher set\n")
	}
}

func (b *NGACBackend) HashGenerate(prompt string, maxWords int) (string, error) {
	b.mu.Lock()
	if !b.hashEnabled || b.hashBridge == nil {
		b.mu.Unlock()
		return "", fmt.Errorf("hash-native network not enabled")
	}

	b.hashBridge.SetPressure(b.pressure)
	hashBridge := b.hashBridge
	b.mu.Unlock()

	response, err := hashBridge.Generate(prompt, maxWords)
	if err != nil {
		return "", fmt.Errorf("hash generation failed: %w", err)
	}

	fmt.Printf("[NGAC] HashGenerate: %s -> %s\n", truncate(prompt, 30), truncate(response, 30))
	return response, nil
}

func (b *NGACBackend) HashForward(concepts []string) ([]string, error) {
	b.mu.Lock()
	if !b.hashEnabled || b.hashBridge == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("hash-native network not enabled")
	}

	b.hashBridge.SetPressure(b.pressure)
	hashBridge := b.hashBridge
	b.mu.Unlock()

	return hashBridge.Forward(concepts)
}

func (b *NGACBackend) GetHashNetwork() *quantum.HashNetwork {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.hashNetwork
}

func (b *NGACBackend) GetHashBridge() *HashBridge {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.hashBridge
}

func (b *NGACBackend) IsHashEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.hashEnabled
}

func (b *NGACBackend) ResetHashContext() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hashNetwork != nil {
		b.hashNetwork.ResetContext()
	}
}

func (b *NGACBackend) EnableBrain(storagePath string, forkID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	brain, err := memory.LoadPositronicBrain(storagePath, forkID)
	if err != nil {
		return fmt.Errorf("failed to load brain: %w", err)
	}

	if b.ngac != nil && b.ngac.Dictionary != nil {
		brain.SetConceptExtractor(func(text string) []string {
			return b.extractConceptsFromText(text)
		})
	}

	b.brain = brain
	b.brainEnabled = true

	fmt.Printf("[NGAC] Positronic brain enabled (pathways: %d, engrams: %d)\n",
		brain.Stats().Pathways.TotalPathways, brain.Stats().Engrams.TotalEngrams)

	return nil
}

func (b *NGACBackend) extractConceptsFromText(text string) []string {
	if b.ngac == nil || b.ngac.Dictionary == nil {
		return nil
	}

	dict := b.ngac.Dictionary
	words := strings.Fields(strings.ToLower(text))
	concepts := make([]string, 0)
	seen := make(map[string]bool)

	for _, word := range words {
		word = strings.Trim(word, ".,?!;:'\"()[]")
		if isStopWord(word) || len(word) < 3 {
			continue
		}

		synsetIDs := dict.GetSynsetsForWord(word)
		if len(synsetIDs) > 0 && !seen[word] {
			concepts = append(concepts, word)
			seen[word] = true
		}
	}

	return concepts
}

func (b *NGACBackend) BrainProcess(text string) (*memory.InputProcessingResult, error) {
	b.mu.Lock()
	if !b.brainEnabled || b.brain == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("positronic brain not enabled")
	}

	b.brain.SetPressure(memory.PressureState{
		Hadamard: b.pressure.Hadamard,
		PauliX: b.pressure.PauliX,
		PauliZ:   b.pressure.PauliZ,
		Phase:   b.pressure.Phase,
	})
	brain := b.brain
	b.mu.Unlock()

	return brain.ProcessInput(text)
}

func (b *NGACBackend) BrainLearn(response string, concepts []string) {
	b.mu.Lock()
	if !b.brainEnabled || b.brain == nil {
		b.mu.Unlock()
		return
	}
	brain := b.brain
	b.mu.Unlock()

	brain.Learn(response, concepts)
}

func (b *NGACBackend) BrainGenerateBounds() *memory.MemoryBounds {
	b.mu.RLock()
	if !b.brainEnabled || b.brain == nil {
		b.mu.RUnlock()
		return nil
	}
	brain := b.brain
	b.mu.RUnlock()

	return brain.GenerateBounds()
}

func (b *NGACBackend) BrainSave() error {
	b.mu.RLock()
	if !b.brainEnabled || b.brain == nil {
		b.mu.RUnlock()
		return nil
	}
	brain := b.brain
	b.mu.RUnlock()

	return brain.Save()
}

func (b *NGACBackend) BrainStats() *memory.BrainStats {
	b.mu.RLock()
	if !b.brainEnabled || b.brain == nil {
		b.mu.RUnlock()
		return nil
	}
	brain := b.brain
	b.mu.RUnlock()

	stats := brain.Stats()
	return &stats
}

func (b *NGACBackend) BrainClearContext() {
	b.mu.Lock()
	if b.brainEnabled && b.brain != nil {
		b.brain.ClearContext()
	}
	b.mu.Unlock()
}

func (b *NGACBackend) BrainDecay() *memory.DecayResult {
	b.mu.Lock()
	if !b.brainEnabled || b.brain == nil {
		b.mu.Unlock()
		return nil
	}
	brain := b.brain
	b.mu.Unlock()

	result := brain.Decay()
	return &result
}

func (b *NGACBackend) IsBrainEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.brainEnabled
}

func (b *NGACBackend) GetBrain() *memory.PositronicBrain {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.brain
}

func (b *NGACBackend) BrainGenerateWithMemory(prompt string, maxTokens int) (string, error) {

	result, err := b.BrainProcess(prompt)
	if err != nil {

		return b.GenerateText(prompt, maxTokens, 1.0, 0.9, 40)
	}

	memBounds := b.BrainGenerateBounds()

	allConcepts := make([]string, 0)
	allConcepts = append(allConcepts, result.ExtractedConcepts...)
	allConcepts = append(allConcepts, result.AssociatedConcepts...)

	for concept := range result.PathwaySuggestions {
		allConcepts = append(allConcepts, concept)
	}

	if b.IsHashEnabled() && b.hashBridge != nil {
		b.mu.Lock()

		if memBounds != nil && len(memBounds.Concepts) > 0 {
			bounds := b.hashBridge.BuildBoundsFromConcepts(memBounds.GetConceptList())
			b.hashBridge.network.SetBounds(bounds)
		}
		b.mu.Unlock()

		response, err := b.HashGenerate(prompt, maxTokens)
		if err == nil {

			outputConcepts := b.extractConceptsFromText(response)
			b.BrainLearn(response, outputConcepts)
			return response, nil
		}
	}

	response, err := b.GenerateText(prompt, maxTokens, 1.0, 0.9, 40)
	if err != nil {
		return "", err
	}

	outputConcepts := b.extractConceptsFromText(response)
	b.BrainLearn(response, outputConcepts)

	return response, nil
}
