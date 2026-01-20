

package quantum

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)


type CollapseCompute struct {
	
	chainID *big.Int

	
	nonceOffset uint64

	
	nonceBase uint64

	
	sender common.Address

	
	results map[common.Hash]*CollapseResult
	mu      sync.RWMutex

	
	synsetMap map[byte]string 
}


type CollapseResult struct {
	
	Input []byte

	
	TxHash common.Hash

	
	Semantics []string

	
	Confidence float64

	
	Resonance map[string]float64
}


func NewCollapseCompute(chainID *big.Int, sender common.Address) *CollapseCompute {
	return &CollapseCompute{
		chainID:     chainID,
		nonceOffset: 100000, 
		nonceBase:   0,
		sender:      sender,
		results:     make(map[common.Hash]*CollapseResult),
		synsetMap:   initSynsetMap(),
	}
}


func initSynsetMap() map[byte]string {
	
	
	m := make(map[byte]string)

	
	for i := 0; i < 256; i++ {
		b := byte(i)
		switch {
		case b < 0x20:
			m[b] = "noun.object"
		case b < 0x40:
			m[b] = "verb.action"
		case b < 0x60:
			m[b] = "adj.property"
		case b < 0x80:
			m[b] = "noun.relation"
		case b < 0xA0:
			m[b] = "noun.quantity"
		case b < 0xC0:
			m[b] = "noun.state"
		case b < 0xE0:
			m[b] = "noun.event"
		default:
			m[b] = "noun.abstract"
		}
	}
	return m
}


func (cc *CollapseCompute) SetNonceBase(nonce uint64) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.nonceBase = nonce
}


func (cc *CollapseCompute) EncodeInput(input []byte, context []byte) *types.Transaction {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	
	data := make([]byte, 0, 65+len(input))

	
	data = append(data, 0x01)

	
	inputHash := sha256.Sum256(input)
	data = append(data, inputHash[:]...)

	
	contextHash := sha256.Sum256(context)
	data = append(data, contextHash[:]...)

	
	data = append(data, input...)

	
	farNonce := cc.nonceBase + cc.nonceOffset

	tx := types.NewTransaction(
		farNonce,
		cc.sender,     
		big.NewInt(0), 
		21000+uint64(len(data)*16), 
		big.NewInt(1), 
		data,
	)

	return tx
}


func (cc *CollapseCompute) CollapseOutput(txHash common.Hash, originalInput []byte) *CollapseResult {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	
	if result, ok := cc.results[txHash]; ok {
		return result
	}

	
	hashBytes := txHash.Bytes()

	
	semantics := make([]string, 0, 8)
	resonance := make(map[string]float64)

	
	for i := 0; i < 8 && i < len(hashBytes); i++ {
		category := cc.synsetMap[hashBytes[i]]
		semantics = append(semantics, category)

		
		resonance[category] += float64(hashBytes[i]) / 255.0
	}

	
	confidence := calculateEntropy(hashBytes)

	result := &CollapseResult{
		Input:      originalInput,
		TxHash:     txHash,
		Semantics:  semantics,
		Confidence: confidence,
		Resonance:  resonance,
	}

	cc.results[txHash] = result
	return result
}


func calculateEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}

	
	freq := make(map[byte]int)
	for _, b := range data {
		freq[b]++
	}

	
	var entropy float64
	n := float64(len(data))
	for _, count := range freq {
		if count > 0 {
			p := float64(count) / n
			entropy -= p * log2(p)
		}
	}

	
	return entropy / 8.0
}


func log2(x float64) float64 {
	if x <= 0 {
		return 0
	}
	
	return ln(x) / 0.693147180559945
}


func ln(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x == 1 {
		return 0
	}

	
	ln2 := 0.693147180559945
	reductions := 0
	for x > 2 {
		x /= 2
		reductions++
	}

	
	y := x - 1
	result := 0.0
	term := y
	for i := 1; i <= 100; i++ {
		if i%2 == 1 {
			result += term / float64(i)
		} else {
			result -= term / float64(i)
		}
		term *= y
		if term < 1e-15 && term > -1e-15 {
			break
		}
	}

	return result + float64(reductions)*ln2
}


func HashToCoordinate(txHash common.Hash) *big.Int {
	return new(big.Int).SetBytes(txHash.Bytes())
}


func CoordinateToSemantics(coord *big.Int) []string {
	if coord == nil {
		return nil
	}

	
	bytes := coord.Bytes()
	if len(bytes) == 0 {
		return nil
	}

	
	semantics := make([]string, 0)

	for i := 0; i+1 < len(bytes) && i < 16; i += 2 {
		
		region := uint16(bytes[i])<<8 | uint16(bytes[i+1])

		
		pos := []string{"noun", "verb", "adj", "adv"}[region%4]

		
		fields := []string{
			"entity", "act", "artifact", "attribute",
			"body", "cognition", "communication", "event",
			"feeling", "food", "group", "location",
			"motive", "object", "person", "phenomenon",
		}
		field := fields[int(region/4)%len(fields)]

		semantics = append(semantics, pos+"."+field)
	}

	return semantics
}


type CollapseQuery struct {
	
	Query []byte

	
	Context []byte

	
	TargetSpace []string

	
	OutputDims int
}


func (cc *CollapseCompute) PrepareQuery(text string, context string, targetSynsets []string) *CollapseQuery {
	
	queryBytes := []byte(text)

	
	contextBytes := []byte(context)

	return &CollapseQuery{
		Query:       queryBytes,
		Context:     contextBytes,
		TargetSpace: targetSynsets,
		OutputDims:  len(targetSynsets),
	}
}


func (cc *CollapseCompute) ExecuteCollapse(query *CollapseQuery, signer *types.EIP155Signer, key []byte) (*types.Transaction, error) {
	
	tx := cc.EncodeInput(query.Query, query.Context)

	
	privateKey, err := crypto.ToECDSA(key)
	if err != nil {
		return nil, err
	}

	signedTx, err := types.SignTx(tx, signer, privateKey)
	if err != nil {
		return nil, err
	}

	return signedTx, nil
}


func (cc *CollapseCompute) InterpretCollapse(txHash common.Hash, query *CollapseQuery) *CollapseResult {
	result := cc.CollapseOutput(txHash, query.Query)

	
	if len(query.TargetSpace) > 0 {
		filtered := make([]string, 0)
		for _, sem := range result.Semantics {
			for _, target := range query.TargetSpace {
				if sem == target {
					filtered = append(filtered, sem)
					break
				}
			}
		}
		if len(filtered) > 0 {
			result.Semantics = filtered
		}
	}

	return result
}


type CollapseChain struct {
	compute *CollapseCompute
	steps   []*CollapseResult
	mu      sync.Mutex
}


func NewCollapseChain(cc *CollapseCompute) *CollapseChain {
	return &CollapseChain{
		compute: cc,
		steps:   make([]*CollapseResult, 0),
	}
}


func (chain *CollapseChain) AddStep(result *CollapseResult) {
	chain.mu.Lock()
	defer chain.mu.Unlock()
	chain.steps = append(chain.steps, result)
}


func (chain *CollapseChain) GetContext() []byte {
	chain.mu.Lock()
	defer chain.mu.Unlock()

	
	context := make([]byte, 0, len(chain.steps)*32)
	for _, step := range chain.steps {
		context = append(context, step.TxHash.Bytes()...)
	}
	return context
}


func (chain *CollapseChain) FinalResult() *CollapseResult {
	chain.mu.Lock()
	defer chain.mu.Unlock()

	if len(chain.steps) == 0 {
		return nil
	}

	
	allSemantics := make([]string, 0)
	totalResonance := make(map[string]float64)
	totalConfidence := 0.0

	for _, step := range chain.steps {
		allSemantics = append(allSemantics, step.Semantics...)
		for k, v := range step.Resonance {
			totalResonance[k] += v
		}
		totalConfidence += step.Confidence
	}

	
	finalHash := sha256.Sum256(chain.GetContext())

	return &CollapseResult{
		Input:      chain.steps[0].Input,
		TxHash:     common.BytesToHash(finalHash[:]),
		Semantics:  allSemantics,
		Confidence: totalConfidence / float64(len(chain.steps)),
		Resonance:  totalResonance,
	}
}


func BytesToHex(b []byte) string {
	return hex.EncodeToString(b)
}


func HexToBytes(s string) ([]byte, error) {
	return hex.DecodeString(s)
}


func EncodeBigInt(n *big.Int) []byte {
	if n == nil {
		return []byte{0}
	}
	return n.Bytes()
}


func DecodeBigInt(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}


func PackUint64(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}


func UnpackUint64(b []byte) uint64 {
	if len(b) < 8 {
		padded := make([]byte, 8)
		copy(padded[8-len(b):], b)
		b = padded
	}
	return binary.BigEndian.Uint64(b)
}
