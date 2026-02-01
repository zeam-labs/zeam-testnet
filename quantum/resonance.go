package quantum

import (
	"math"
	"sort"
	"time"
)

type ResonanceResult struct {
	Location BlockLocation
	Strength float64
	Count    int
	Block    *Block
	Data     interface{}
}

func FindResonance(chains map[string]*Chain) *ResonanceResult {

	blockCount := make(map[BlockLocation]int)
	blockPhase := make(map[BlockLocation][]float64)

	origin := GetWaveOrigin()
	elapsed := time.Since(origin.PushTime).Seconds()

	for chainID := range chains {
		loc, exists := GetCurrentBlock(chainID)
		if !exists {
			continue
		}

		blockCount[loc]++

		freq := GetChainFrequency(chainID)
		phase := math.Sin(elapsed * freq * 2 * math.Pi)
		blockPhase[loc] = append(blockPhase[loc], phase)
	}

	var bestLoc BlockLocation
	bestStrength := 0.0
	bestCount := 0

	for loc, count := range blockCount {

		phaseSum := 0.0
		for _, p := range blockPhase[loc] {
			phaseSum += p
		}

		strength := float64(count) * math.Abs(phaseSum)

		if strength > bestStrength {
			bestStrength = strength
			bestLoc = loc
			bestCount = count
		}
	}

	if bestCount == 0 {
		return nil
	}

	chain, exists := chains[bestLoc.ChainID]
	if !exists {
		return nil
	}

	block, exists := chain.GetBlock(bestLoc.BlockIdx)
	if !exists {
		return nil
	}

	return &ResonanceResult{
		Location: bestLoc,
		Strength: bestStrength,
		Count:    bestCount,
		Block:    &block,
	}
}

func FindTopResonances(chains map[string]*Chain, n int) []*ResonanceResult {

	blockCount := make(map[BlockLocation]int)
	blockPhase := make(map[BlockLocation][]float64)

	origin := GetWaveOrigin()
	elapsed := time.Since(origin.PushTime).Seconds()

	for chainID := range chains {
		loc, exists := GetCurrentBlock(chainID)
		if !exists {
			continue
		}

		blockCount[loc]++

		freq := GetChainFrequency(chainID)
		phase := math.Sin(elapsed * freq * 2 * math.Pi)
		blockPhase[loc] = append(blockPhase[loc], phase)
	}

	type candidate struct {
		loc      BlockLocation
		strength float64
		count    int
	}

	candidates := make([]candidate, 0, len(blockCount))
	for loc, count := range blockCount {
		phaseSum := 0.0
		for _, p := range blockPhase[loc] {
			phaseSum += p
		}
		strength := float64(count) * math.Abs(phaseSum)

		candidates = append(candidates, candidate{
			loc:      loc,
			strength: strength,
			count:    count,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].strength > candidates[j].strength
	})

	if n > len(candidates) {
		n = len(candidates)
	}

	results := make([]*ResonanceResult, 0, n)
	for i := 0; i < n; i++ {
		c := candidates[i]
		chain, exists := chains[c.loc.ChainID]
		if !exists {
			continue
		}

		block, exists := chain.GetBlock(c.loc.BlockIdx)
		if !exists {
			continue
		}

		results = append(results, &ResonanceResult{
			Location: c.loc,
			Strength: c.strength,
			Count:    c.count,
			Block:    &block,
		})
	}

	return results
}

func MeasurePhaseCoherence(chains map[string]*Chain) float64 {
	origin := GetWaveOrigin()
	elapsed := time.Since(origin.PushTime).Seconds()

	var phases []float64
	for chainID := range chains {
		freq := GetChainFrequency(chainID)
		phase := math.Sin(elapsed * freq * 2 * math.Pi)
		phases = append(phases, phase)
	}

	if len(phases) == 0 {
		return 0
	}

	sumCos := 0.0
	sumSin := 0.0
	for _, p := range phases {
		sumCos += math.Cos(p)
		sumSin += math.Sin(p)
	}

	coherence := math.Sqrt(sumCos*sumCos+sumSin*sumSin) / float64(len(phases))
	return coherence
}
