package quantum

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sync"
)

type GroverOracle func(item []byte) bool

type GroverResult struct {
	Found      bool
	Item       []byte
	Index      int
	Iterations int
	Confidence float64
}

func GroverSearch(mesh *Mesh, searchSpace [][]byte, oracle GroverOracle) *GroverResult {
	n := len(searchSpace)
	if n == 0 {
		return &GroverResult{Found: false}
	}

	optimalIters := int(math.Pi / 4.0 * math.Sqrt(float64(n)))
	if optimalIters < 1 {
		optimalIters = 1
	}

	amplitudes := make([]float64, n)
	initialAmp := 1.0 / math.Sqrt(float64(n))
	for i := range amplitudes {
		amplitudes[i] = initialAmp
	}

	markedIndices := make([]int, 0)
	for i, item := range searchSpace {
		if oracle(item) {
			markedIndices = append(markedIndices, i)
		}
	}

	if len(markedIndices) == 0 {
		return &GroverResult{Found: false, Iterations: 0}
	}

	for iter := 0; iter < optimalIters; iter++ {

		for _, idx := range markedIndices {
			amplitudes[idx] = -amplitudes[idx]
		}

		mean := 0.0
		for _, amp := range amplitudes {
			mean += amp
		}
		mean /= float64(n)

		for i := range amplitudes {
			amplitudes[i] = 2*mean - amplitudes[i]
		}
	}

	maxProb := 0.0
	maxIdx := 0
	for i, amp := range amplitudes {
		prob := amp * amp
		if prob > maxProb {
			maxProb = prob
			maxIdx = i
		}
	}

	if oracle(searchSpace[maxIdx]) {
		return &GroverResult{
			Found:      true,
			Item:       searchSpace[maxIdx],
			Index:      maxIdx,
			Iterations: optimalIters,
			Confidence: maxProb,
		}
	}

	return &GroverResult{
		Found:      false,
		Iterations: optimalIters,
		Confidence: maxProb,
	}
}

func GroverSearchParallel(mesh *Mesh, searchSpace [][]byte, oracle GroverOracle) *GroverResult {
	if mesh == nil || len(searchSpace) == 0 {
		return &GroverResult{Found: false}
	}

	numChains := len(mesh.Chains)
	if numChains == 0 {
		return GroverSearch(mesh, searchSpace, oracle)
	}

	chunkSize := (len(searchSpace) + numChains - 1) / numChains
	results := make([]*GroverResult, numChains)
	var wg sync.WaitGroup

	chainIdx := 0
	for chainID := range mesh.Chains {
		start := chainIdx * chunkSize
		end := start + chunkSize
		if end > len(searchSpace) {
			end = len(searchSpace)
		}
		if start >= end {
			break
		}

		wg.Add(1)
		go func(idx, start, end int, cid string) {
			defer wg.Done()

			localSpace := searchSpace[start:end]
			localResult := groverSearchLocal(localSpace, oracle)

			if localResult.Found {

				localResult.Index += start
			}
			results[idx] = localResult

			if mesh.Pressure != nil {
				loc := BlockLocation{ChainID: cid, BlockIdx: 0}
				mesh.Pressure.AddPressure(loc, localResult.Confidence)
			}
		}(chainIdx, start, end, chainID)
		chainIdx++
	}

	wg.Wait()

	var best *GroverResult
	for _, r := range results {
		if r != nil && r.Found {
			if best == nil || r.Confidence > best.Confidence {
				best = r
			}
		}
	}

	if best != nil {
		return best
	}

	totalIters := 0
	for _, r := range results {
		if r != nil {
			totalIters += r.Iterations
		}
	}
	return &GroverResult{Found: false, Iterations: totalIters}
}

func groverSearchLocal(searchSpace [][]byte, oracle GroverOracle) *GroverResult {
	n := len(searchSpace)
	if n == 0 {
		return &GroverResult{Found: false}
	}

	if n <= 4 {
		for i, item := range searchSpace {
			if oracle(item) {
				return &GroverResult{
					Found:      true,
					Item:       item,
					Index:      i,
					Iterations: 1,
					Confidence: 1.0,
				}
			}
		}
		return &GroverResult{Found: false, Iterations: 1}
	}

	optimalIters := int(math.Pi / 4.0 * math.Sqrt(float64(n)))
	if optimalIters < 1 {
		optimalIters = 1
	}

	amplitudes := make([]float64, n)
	initialAmp := 1.0 / math.Sqrt(float64(n))
	for i := range amplitudes {
		amplitudes[i] = initialAmp
	}

	marked := make([]bool, n)
	numMarked := 0
	for i, item := range searchSpace {
		if oracle(item) {
			marked[i] = true
			numMarked++
		}
	}

	if numMarked == 0 {
		return &GroverResult{Found: false, Iterations: 0}
	}

	for iter := 0; iter < optimalIters; iter++ {

		for i := range amplitudes {
			if marked[i] {
				amplitudes[i] = -amplitudes[i]
			}
		}

		mean := 0.0
		for _, amp := range amplitudes {
			mean += amp
		}
		mean /= float64(n)

		for i := range amplitudes {
			amplitudes[i] = 2*mean - amplitudes[i]
		}
	}

	maxProb := 0.0
	maxIdx := 0
	for i, amp := range amplitudes {
		prob := amp * amp
		if prob > maxProb {
			maxProb = prob
			maxIdx = i
		}
	}

	if marked[maxIdx] {
		return &GroverResult{
			Found:      true,
			Item:       searchSpace[maxIdx],
			Index:      maxIdx,
			Iterations: optimalIters,
			Confidence: maxProb,
		}
	}

	return &GroverResult{Found: false, Iterations: optimalIters, Confidence: maxProb}
}

func GroverHashSearch(mesh *Mesh, targetPrefix []byte, searchSpace [][]byte) *GroverResult {
	oracle := func(item []byte) bool {
		hash := sha256.Sum256(item)

		for i, b := range targetPrefix {
			if i >= len(hash) || hash[i] != b {
				return false
			}
		}
		return true
	}

	return GroverSearchParallel(mesh, searchSpace, oracle)
}

func GroverSemanticSearch(mesh *Mesh, targetHash []byte, synsetHashes [][]byte, maxDistance int) *GroverResult {
	oracle := func(item []byte) bool {
		dist := hammingDistance(item, targetHash)
		return dist <= maxDistance
	}

	return GroverSearchParallel(mesh, synsetHashes, oracle)
}

func hammingDistance(a, b []byte) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	dist := 0
	for i := 0; i < minLen; i++ {
		xor := a[i] ^ b[i]

		dist += popCount(xor)
	}

	dist += abs(len(a)-len(b)) * 8

	return dist
}

func popCount(b byte) int {
	b = (b & 0x55) + ((b >> 1) & 0x55)
	b = (b & 0x33) + ((b >> 2) & 0x33)
	b = (b & 0x0F) + ((b >> 4) & 0x0F)
	return int(b)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func GroverAmplitudeEstimation(mesh *Mesh, searchSpace [][]byte, oracle GroverOracle) (int, float64) {
	n := len(searchSpace)
	if n == 0 {
		return 0, 0
	}

	sampleSize := int(math.Sqrt(float64(n)))
	if sampleSize < 10 {
		sampleSize = min(10, n)
	}

	marked := 0
	for i := 0; i < sampleSize; i++ {
		idx := (i * n) / sampleSize
		if oracle(searchSpace[idx]) {
			marked++
		}
	}

	estimatedCount := (marked * n) / sampleSize
	confidence := 1.0 - 1.0/math.Sqrt(float64(sampleSize))

	return estimatedCount, confidence
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func GroverWithAdaptiveIterations(mesh *Mesh, searchSpace [][]byte, oracle GroverOracle) *GroverResult {
	n := len(searchSpace)
	if n == 0 {
		return &GroverResult{Found: false}
	}

	estMarked, _ := GroverAmplitudeEstimation(mesh, searchSpace, oracle)
	if estMarked == 0 {

		return GroverSearchParallel(mesh, searchSpace, oracle)
	}

	optimalIters := int(math.Pi / 4.0 * math.Sqrt(float64(n)/float64(estMarked)))
	if optimalIters < 1 {
		optimalIters = 1
	}
	if optimalIters > int(math.Sqrt(float64(n))) {
		optimalIters = int(math.Sqrt(float64(n)))
	}

	amplitudes := make([]float64, n)
	initialAmp := 1.0 / math.Sqrt(float64(n))
	for i := range amplitudes {
		amplitudes[i] = initialAmp
	}

	marked := make([]bool, n)
	for i, item := range searchSpace {
		if oracle(item) {
			marked[i] = true
		}
	}

	for iter := 0; iter < optimalIters; iter++ {

		for i := range amplitudes {
			if marked[i] {
				amplitudes[i] = -amplitudes[i]
			}
		}

		mean := 0.0
		for _, amp := range amplitudes {
			mean += amp
		}
		mean /= float64(n)

		for i := range amplitudes {
			amplitudes[i] = 2*mean - amplitudes[i]
		}
	}

	maxProb := 0.0
	maxIdx := 0
	for i, amp := range amplitudes {
		prob := amp * amp
		if prob > maxProb {
			maxProb = prob
			maxIdx = i
		}
	}

	if marked[maxIdx] {
		return &GroverResult{
			Found:      true,
			Item:       searchSpace[maxIdx],
			Index:      maxIdx,
			Iterations: optimalIters,
			Confidence: maxProb,
		}
	}

	return &GroverResult{Found: false, Iterations: optimalIters}
}

type DatabaseRecord struct {
	Key   []byte
	Value []byte
	Hash  []byte
}

func GroverDatabaseSearch(mesh *Mesh, records []DatabaseRecord, predicate func(DatabaseRecord) bool) *GroverResult {

	searchSpace := make([][]byte, len(records))
	for i, rec := range records {
		searchSpace[i] = rec.Hash
	}

	oracle := func(item []byte) bool {

		for _, rec := range records {
			if bytesEqual(rec.Hash, item) {
				return predicate(rec)
			}
		}
		return false
	}

	result := GroverSearchParallel(mesh, searchSpace, oracle)

	if result.Found {
		for _, rec := range records {
			if bytesEqual(rec.Hash, result.Item) {
				result.Item = rec.Value
				break
			}
		}
	}

	return result
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func GroverMinimumFinding(mesh *Mesh, values []uint64) (int, uint64) {
	n := len(values)
	if n == 0 {
		return -1, 0
	}

	if n <= 100 {
		minIdx := 0
		minVal := values[0]
		for i, v := range values {
			if v < minVal {
				minVal = v
				minIdx = i
			}
		}
		return minIdx, minVal
	}

	threshold := values[n/2]
	minIdx := n / 2

	iters := int(math.Sqrt(float64(n)) * 1.5)

	for iter := 0; iter < iters; iter++ {

		oracle := func(item []byte) bool {
			if len(item) < 8 {
				return false
			}
			val := binary.BigEndian.Uint64(item)
			return val < threshold
		}

		searchSpace := make([][]byte, n)
		for i, v := range values {
			searchSpace[i] = make([]byte, 8)
			binary.BigEndian.PutUint64(searchSpace[i], v)
		}

		result := groverSearchLocal(searchSpace, oracle)

		if result.Found && len(result.Item) >= 8 {
			foundVal := binary.BigEndian.Uint64(result.Item)
			if foundVal < threshold {
				threshold = foundVal
				minIdx = result.Index
			}
		}
	}

	return minIdx, threshold
}
