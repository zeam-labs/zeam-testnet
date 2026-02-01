package quantum

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"sync"
	"time"

	"zeam/node"

	"github.com/ethereum/go-ethereum/crypto"
)

type HashTensor struct {

	Data [][]byte

	Shape []int

	Pressure PressureMetrics

	Bounds *SemanticBounds
}

type SemanticBounds struct {

	ValidSynsets map[string]bool

	ValidCategories []string

	MinPauliX float64

	MaxPauliZ float64

	ConceptAnchors []string
}

type PayloadType byte

const (
	PayloadEmbed     PayloadType = 0x01
	PayloadAttend    PayloadType = 0x02
	PayloadTransform PayloadType = 0x03
	PayloadDecode    PayloadType = 0x04
	PayloadCombine   PayloadType = 0x05
)

type HashPayload struct {

	Version   byte
	Type      PayloadType
	LayerIdx  byte
	Operation byte

	Pressure PressureMetrics

	SeqPosition uint32
	BatchIdx    uint16

	BoundsHash [8]byte

	InputHash   [32]byte
	ContextHash [32]byte

	Data []byte
}

func (p *HashPayload) Encode() []byte {
	buf := make([]byte, 0, 128+len(p.Data))

	buf = append(buf, p.Version)
	buf = append(buf, byte(p.Type))
	buf = append(buf, p.LayerIdx)
	buf = append(buf, p.Operation)

	buf = append(buf, byte(p.Pressure.Hadamard*255))
	buf = append(buf, byte(p.Pressure.PauliX*255))
	buf = append(buf, byte(p.Pressure.PauliZ*255))
	buf = append(buf, byte(p.Pressure.Phase*255))

	buf = append(buf, byte(p.SeqPosition>>24), byte(p.SeqPosition>>16),
		byte(p.SeqPosition>>8), byte(p.SeqPosition))
	buf = append(buf, byte(p.BatchIdx>>8), byte(p.BatchIdx))

	buf = append(buf, p.BoundsHash[:]...)

	buf = append(buf, p.InputHash[:]...)
	buf = append(buf, p.ContextHash[:]...)

	buf = append(buf, p.Data...)

	return buf
}

func DecodePayload(data []byte) (*HashPayload, error) {
	if len(data) < 86 {
		return nil, fmt.Errorf("payload too short: %d bytes", len(data))
	}

	p := &HashPayload{
		Version:   data[0],
		Type:      PayloadType(data[1]),
		LayerIdx:  data[2],
		Operation: data[3],
		Pressure: PressureMetrics{
			Hadamard: float64(data[4]) / 255.0,
			PauliX:   float64(data[5]) / 255.0,
			PauliZ:   float64(data[6]) / 255.0,
			Phase:    float64(data[7]) / 255.0,
		},
		SeqPosition: binary.BigEndian.Uint32(data[8:12]),
		BatchIdx:    binary.BigEndian.Uint16(data[12:14]),
	}

	copy(p.BoundsHash[:], data[14:22])
	copy(p.InputHash[:], data[22:54])
	copy(p.ContextHash[:], data[54:86])

	if len(data) > 86 {
		p.Data = data[86:]
	}

	return p, nil
}

type HashNetwork struct {
	mu sync.RWMutex

	NumLayers    int
	EmbedDim     int
	NumHeads     int
	ContextLen   int

	Pressure PressureMetrics
	Context  [][]byte

	Bounds *SemanticBounds

	Dispatcher HashDispatcher
}

type HashDispatcher interface {

	Dispatch(payload []byte) ([][]byte, error)
}

type LocalHashDispatcher struct {

	NumNodes int

	Workers int
}

func (d *LocalHashDispatcher) Dispatch(payload []byte) ([][]byte, error) {
	if d.NumNodes <= 0 {
		d.NumNodes = 64
	}

	numWorkers := d.Workers
	if numWorkers <= 0 {
		numWorkers = 8
	}
	if numWorkers > d.NumNodes {
		numWorkers = d.NumNodes
	}

	hashes := make([][]byte, d.NumNodes)

	if d.NumNodes <= 8 {
		for i := 0; i < d.NumNodes; i++ {
			nodePayload := make([]byte, len(payload)+2)
			copy(nodePayload, payload)
			nodePayload[len(payload)] = byte(i >> 8)
			nodePayload[len(payload)+1] = byte(i)
			hashes[i] = crypto.Keccak256(nodePayload)
		}
		return hashes, nil
	}

	var wg sync.WaitGroup
	chunkSize := (d.NumNodes + numWorkers - 1) / numWorkers

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > d.NumNodes {
			end = d.NumNodes
		}
		if start >= end {
			break
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()

			nodePayload := make([]byte, len(payload)+2)
			copy(nodePayload, payload)

			for i := start; i < end; i++ {
				nodePayload[len(payload)] = byte(i >> 8)
				nodePayload[len(payload)+1] = byte(i)
				hashes[i] = crypto.Keccak256(nodePayload)
			}
		}(start, end)
	}

	wg.Wait()
	return hashes, nil
}

type NetworkHashDispatcher struct {
	Feed     node.NetworkFeed
	NumNodes int
	Timeout  time.Duration
}

func (d *NetworkHashDispatcher) Dispatch(payload []byte) ([][]byte, error) {
	if d.NumNodes <= 0 {
		d.NumNodes = 64
	}
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	resultCh := d.Feed.Broadcast(payload)

	var txHash []byte
	select {
	case result := <-resultCh:
		if result.Err != nil {
			return nil, fmt.Errorf("network broadcast failed: %w", result.Err)
		}
		txHash = result.TxHash[:]
	case <-time.After(timeout):
		return nil, fmt.Errorf("network broadcast timed out")
	}

	hashes := make([][]byte, d.NumNodes)
	buf := make([]byte, 34)
	copy(buf, txHash)

	for i := 0; i < d.NumNodes; i++ {
		buf[32] = byte(i >> 8)
		buf[33] = byte(i)
		hashes[i] = crypto.Keccak256(buf)
	}

	return hashes, nil
}

func NewHashNetwork(numLayers, embedDim, numHeads, contextLen int) *HashNetwork {
	return &HashNetwork{
		NumLayers:  numLayers,
		EmbedDim:   embedDim,
		NumHeads:   numHeads,
		ContextLen: contextLen,
		Pressure: PressureMetrics{
			Hadamard: 0.5,
			PauliX:   0.5,
			PauliZ:   0.3,
			Phase:    0.5,
		},
		Context:    make([][]byte, 0),
		Dispatcher: &LocalHashDispatcher{NumNodes: 64},
	}
}

func (hn *HashNetwork) SetDispatcher(d HashDispatcher) {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	hn.Dispatcher = d
}

func (hn *HashNetwork) SetBounds(bounds *SemanticBounds) {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	hn.Bounds = bounds
}

func (hn *HashNetwork) SetPressure(p PressureMetrics) {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	hn.Pressure = p
}

func (hn *HashNetwork) HashEmbed(concepts []string, positions []int) (*HashTensor, error) {
	hn.mu.RLock()
	pressure := hn.Pressure
	bounds := hn.Bounds
	dispatcher := hn.Dispatcher
	hn.mu.RUnlock()

	embeddings := make([][]byte, len(concepts))

	for i, concept := range concepts {

		payload := &HashPayload{
			Version:     1,
			Type:        PayloadEmbed,
			LayerIdx:    0,
			Operation:   0,
			Pressure:    pressure,
			SeqPosition: uint32(positions[i]),
			Data:        []byte(concept),
		}

		if bounds != nil {
			boundsData := fmt.Sprintf("%v", bounds.ValidCategories)
			copy(payload.BoundsHash[:], crypto.Keccak256([]byte(boundsData))[:8])
		}

		encoded := payload.Encode()
		hashes, err := dispatcher.Dispatch(encoded)
		if err != nil {
			return nil, fmt.Errorf("dispatch failed for concept %s: %w", concept, err)
		}

		embeddings[i] = combineHashes(hashes)
	}

	return &HashTensor{
		Data:     embeddings,
		Shape:    []int{len(concepts), 32},
		Pressure: pressure,
		Bounds:   bounds,
	}, nil
}

func (hn *HashNetwork) HashAttend(input *HashTensor, headIdx int) (*HashTensor, error) {
	hn.mu.RLock()
	pressure := hn.Pressure
	context := hn.Context
	dispatcher := hn.Dispatcher
	hn.mu.RUnlock()

	seqLen := len(input.Data)
	outputs := make([][]byte, seqLen)

	var contextHash [32]byte
	if len(context) > 0 {
		contextHash = [32]byte(combineHashes(context))
	}

	for pos := 0; pos < seqLen; pos++ {

		qPayload := &HashPayload{
			Version:     1,
			Type:        PayloadAttend,
			LayerIdx:    byte(headIdx),
			Operation:   0,
			Pressure:    pressure,
			SeqPosition: uint32(pos),
			ContextHash: contextHash,
		}
		copy(qPayload.InputHash[:], input.Data[pos])
		qPayload.Data = []byte("query")

		kPayload := *qPayload
		kPayload.Operation = 1
		kPayload.Data = []byte("key")

		vPayload := *qPayload
		vPayload.Operation = 2
		vPayload.Data = []byte("value")

		qHashes, _ := dispatcher.Dispatch(qPayload.Encode())
		kHashes, _ := dispatcher.Dispatch(kPayload.Encode())
		vHashes, _ := dispatcher.Dispatch(vPayload.Encode())

		Q := combineHashes(qHashes)
		K := combineHashes(kHashes)
		V := combineHashes(vHashes)

		attnScore := hammingSimilarity(Q, K)
		attnScore = modulateByPressure(attnScore, pressure)

		outputs[pos] = blendHashByScore(V, input.Data[pos], attnScore)
	}

	return &HashTensor{
		Data:     outputs,
		Shape:    input.Shape,
		Pressure: pressure,
		Bounds:   input.Bounds,
	}, nil
}

func (hn *HashNetwork) HashTransform(input *HashTensor, layerIdx int) (*HashTensor, error) {
	hn.mu.RLock()
	pressure := hn.Pressure
	dispatcher := hn.Dispatcher
	hn.mu.RUnlock()

	outputs := make([][]byte, len(input.Data))

	for pos, inputHash := range input.Data {
		payload := &HashPayload{
			Version:     1,
			Type:        PayloadTransform,
			LayerIdx:    byte(layerIdx),
			Operation:   0,
			Pressure:    pressure,
			SeqPosition: uint32(pos),
		}
		copy(payload.InputHash[:], inputHash)

		hashes, err := dispatcher.Dispatch(payload.Encode())
		if err != nil {
			return nil, err
		}

		outputs[pos] = combineHashesWithPressure(hashes, pressure)

		outputs[pos] = xorHashes(outputs[pos], inputHash)
	}

	return &HashTensor{
		Data:     outputs,
		Shape:    input.Shape,
		Pressure: pressure,
		Bounds:   input.Bounds,
	}, nil
}

func (hn *HashNetwork) HashDecode(input *HashTensor) ([]SemanticIndex, error) {
	hn.mu.RLock()
	bounds := hn.Bounds
	hn.mu.RUnlock()

	indices := make([]SemanticIndex, len(input.Data))

	for i, hash := range input.Data {
		idx := decodeHashToSemantic(hash, bounds)
		indices[i] = idx
	}

	return indices, nil
}

type SemanticIndex struct {

	Category string

	SynsetIndex uint32

	WordIndex uint16

	RelationType byte

	Confidence float64
}

func decodeHashToSemantic(hash []byte, bounds *SemanticBounds) SemanticIndex {
	if len(hash) < 16 {
		return SemanticIndex{}
	}

	catByte := hash[0]
	categories := []string{"n", "v", "a", "r"}
	if bounds != nil && len(bounds.ValidCategories) > 0 {
		categories = bounds.ValidCategories
	}
	category := categories[int(catByte)%len(categories)]

	synsetIdx := binary.BigEndian.Uint32(hash[2:6])

	wordIdx := binary.BigEndian.Uint16(hash[6:8])

	relType := hash[8] % 8

	entropy := hashEntropy(hash[9:13])
	confidence := entropy / 8.0

	return SemanticIndex{
		Category:     category,
		SynsetIndex:  synsetIdx,
		WordIndex:    wordIdx,
		RelationType: relType,
		Confidence:   confidence,
	}
}

func (hn *HashNetwork) Forward(concepts []string) ([]SemanticIndex, error) {

	positions := make([]int, len(concepts))
	for i := range positions {
		positions[i] = i
	}

	embedded, err := hn.HashEmbed(concepts, positions)
	if err != nil {
		return nil, fmt.Errorf("embed failed: %w", err)
	}

	current := embedded
	if hn.NumHeads > 1 {

		headOutputs := make([]*HashTensor, hn.NumHeads)
		headErrors := make([]error, hn.NumHeads)
		var wg sync.WaitGroup

		for head := 0; head < hn.NumHeads; head++ {
			wg.Add(1)
			go func(h int) {
				defer wg.Done()
				attended, err := hn.HashAttend(current, h)
				headOutputs[h] = attended
				headErrors[h] = err
			}(head)
		}
		wg.Wait()

		for h := 0; h < hn.NumHeads; h++ {
			if headErrors[h] != nil {
				return nil, fmt.Errorf("attend failed at head %d: %w", h, headErrors[h])
			}
		}

		combinedData := make([][]byte, len(current.Data))
		for i := range combinedData {
			combinedData[i] = make([]byte, 32)
			copy(combinedData[i], headOutputs[0].Data[i])
			for h := 1; h < hn.NumHeads; h++ {
				if i < len(headOutputs[h].Data) {
					xorUint64Chunks(combinedData[i], headOutputs[h].Data[i])
				}
			}
		}
		current = &HashTensor{
			Data:     combinedData,
			Shape:    current.Shape,
			Pressure: current.Pressure,
			Bounds:   current.Bounds,
		}
	} else {

		attended, err := hn.HashAttend(current, 0)
		if err != nil {
			return nil, fmt.Errorf("attend failed at head 0: %w", err)
		}
		current = attended
	}

	for layer := 0; layer < hn.NumLayers; layer++ {
		transformed, err := hn.HashTransform(current, layer)
		if err != nil {
			return nil, fmt.Errorf("transform failed at layer %d: %w", layer, err)
		}
		current = transformed
	}

	indices, err := hn.HashDecode(current)
	if err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}

	hn.mu.Lock()
	for _, hash := range current.Data {
		hn.Context = append(hn.Context, hash)

		if len(hn.Context) > hn.ContextLen {
			hn.Context = hn.Context[1:]
		}
	}
	hn.mu.Unlock()

	return indices, nil
}

func (hn *HashNetwork) ResetContext() {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	hn.Context = make([][]byte, 0)
}

func combineHashes(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return make([]byte, 32)
	}
	if len(hashes) == 1 {
		return hashes[0]
	}

	combined := make([]byte, 32)
	copy(combined, hashes[0])

	for i := 1; i < len(hashes); i++ {
		h := hashes[i]
		if len(h) >= 32 {

			xorUint64Chunks(combined, h)
		} else {

			for j := 0; j < len(h); j++ {
				combined[j] ^= h[j]
			}
		}
	}

	result := crypto.Keccak256(combined)
	return result
}

func xorUint64Chunks(dst, src []byte) {

	_ = dst[31]
	_ = src[31]

	d0 := binary.LittleEndian.Uint64(dst[0:8])
	s0 := binary.LittleEndian.Uint64(src[0:8])
	binary.LittleEndian.PutUint64(dst[0:8], d0^s0)

	d1 := binary.LittleEndian.Uint64(dst[8:16])
	s1 := binary.LittleEndian.Uint64(src[8:16])
	binary.LittleEndian.PutUint64(dst[8:16], d1^s1)

	d2 := binary.LittleEndian.Uint64(dst[16:24])
	s2 := binary.LittleEndian.Uint64(src[16:24])
	binary.LittleEndian.PutUint64(dst[16:24], d2^s2)

	d3 := binary.LittleEndian.Uint64(dst[24:32])
	s3 := binary.LittleEndian.Uint64(src[24:32])
	binary.LittleEndian.PutUint64(dst[24:32], d3^s3)
}

func combineHashesWithPressure(hashes [][]byte, pressure PressureMetrics) []byte {
	if len(hashes) == 0 {
		return make([]byte, 32)
	}

	useCount := int(float64(len(hashes)) * pressure.Hadamard)
	if useCount < 1 {
		useCount = 1
	}
	if useCount > len(hashes) {
		useCount = len(hashes)
	}

	combined := make([]byte, 32)
	copy(combined, hashes[0])

	for i := 1; i < useCount; i++ {
		weight := 1.0 - (float64(i) / float64(useCount) * (1.0 - pressure.PauliX))
		for j := 0; j < 32 && j < len(hashes[i]); j++ {
			if weight > 0.5 {
				combined[j] ^= hashes[i][j]
			}
		}
	}

	return crypto.Keccak256(combined)
}

func hammingSimilarity(a, b []byte) float64 {
	if len(a) != len(b) {
		return 0
	}

	totalBits := len(a) * 8
	diffBits := 0

	for i := range a {
		diffBits += bits.OnesCount8(a[i] ^ b[i])
	}

	return 1.0 - (float64(diffBits) / float64(totalBits))
}

func modulateByPressure(score float64, pressure PressureMetrics) float64 {

	if pressure.PauliX > 0.5 {

		if score > 0.5 {
			score = score + (1-score)*(pressure.PauliX-0.5)*2
		} else {
			score = score - score*(pressure.PauliX-0.5)*2
		}
	}

	if pressure.PauliZ > 0.5 {

		score = score + (0.5-score)*(pressure.PauliZ-0.5)*2
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

func blendHashByScore(a, b []byte, score float64) []byte {
	result := make([]byte, 32)

	for i := 0; i < 32; i++ {
		if i < len(a) && i < len(b) {

			threshold := byte(score * 255)
			mask := byte(0)
			for bit := 0; bit < 8; bit++ {
				if byte((i*8+bit)*7%256) < threshold {
					mask |= (1 << bit)
				}
			}
			result[i] = (a[i] & mask) | (b[i] & ^mask)
		}
	}

	return result
}

func xorHashes(a, b []byte) []byte {
	result := make([]byte, 32)

	if len(a) >= 32 && len(b) >= 32 {
		copy(result, a[:32])
		xorUint64Chunks(result, b)
		return result
	}

	for i := 0; i < 32; i++ {
		if i < len(a) && i < len(b) {
			result[i] = a[i] ^ b[i]
		} else if i < len(a) {
			result[i] = a[i]
		} else if i < len(b) {
			result[i] = b[i]
		}
	}
	return result
}

func hashEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}

	counts := make([]int, 256)
	for _, b := range data {
		counts[b]++
	}

	entropy := 0.0
	n := float64(len(data))
	for _, count := range counts {
		if count > 0 {
			p := float64(count) / n
			entropy -= p * math.Log2(p)
		}
	}

	return entropy
}
