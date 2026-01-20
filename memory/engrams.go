

package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)


type Engram struct {
	ID        string    `json:"id"`        
	Concepts  []string  `json:"concepts"`  
	Hashes    []string  `json:"hashes"`    
	Strength  float64   `json:"strength"`  
	Resonance float64   `json:"resonance"` 
	Created   time.Time `json:"created"`
	LastUsed  time.Time `json:"last_used"`
	Activations int     `json:"activations"`

	
	FormationPressure PressureState `json:"formation_pressure"`

	
	Context map[string]string `json:"context,omitempty"`
}


type EngramMemory struct {
	mu sync.RWMutex

	
	Engrams map[string]*Engram `json:"engrams"`

	
	ConceptIndex map[string][]string `json:"concept_index"`

	
	TotalEngrams     int       `json:"total_engrams"`
	TotalActivations int64     `json:"total_activations"`
	CreatedAt        time.Time `json:"created_at"`
	LastModified     time.Time `json:"last_modified"`

	
	DecayRate        float64 `json:"decay_rate"`        
	ResonanceDecay   float64 `json:"resonance_decay"`   
	MinStrength      float64 `json:"min_strength"`      
	MaxEngrams       int     `json:"max_engrams"`       
	MinConceptsMatch int     `json:"min_concepts_match"` 

	
	storagePath string
}


func NewEngramMemory(storagePath string) *EngramMemory {
	return &EngramMemory{
		Engrams:          make(map[string]*Engram),
		ConceptIndex:     make(map[string][]string),
		CreatedAt:        time.Now(),
		LastModified:     time.Now(),
		DecayRate:        0.02,  
		ResonanceDecay:   0.01,  
		MinStrength:      0.01,
		MaxEngrams:       10000, 
		MinConceptsMatch: 2,     
		storagePath:      storagePath,
	}
}


func LoadEngramMemory(storagePath string) (*EngramMemory, error) {
	em := NewEngramMemory(storagePath)

	filePath := filepath.Join(storagePath, "engrams.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return em, nil 
		}
		return nil, err
	}

	if err := json.Unmarshal(data, em); err != nil {
		return nil, err
	}

	em.storagePath = storagePath
	return em, nil
}


func (em *EngramMemory) Save() error {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if err := os.MkdirAll(em.storagePath, 0700); err != nil {
		return err
	}

	em.LastModified = time.Now()

	data, err := json.MarshalIndent(em, "", "  ")
	if err != nil {
		return err
	}

	filePath := filepath.Join(em.storagePath, "engrams.json")
	return os.WriteFile(filePath, data, 0600)
}


func generateEngramID(concepts []string) string {
	
	sorted := make([]string, len(concepts))
	copy(sorted, concepts)
	sort.Strings(sorted)

	
	combined := ""
	for _, c := range sorted {
		combined += c + "|"
	}
	h := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(h[:12])
}


func (em *EngramMemory) Store(concepts []string, pressure PressureState, context map[string]string) *Engram {
	if len(concepts) < 2 {
		return nil 
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	
	id := generateEngramID(concepts)

	engram, exists := em.Engrams[id]
	if exists {
		
		engram.Strength += 0.1
		if engram.Strength > 1.0 {
			engram.Strength = 1.0
		}
		engram.Resonance += 0.05
		if engram.Resonance > 1.0 {
			engram.Resonance = 1.0
		}
		engram.Activations++
		engram.LastUsed = time.Now()

		
		if engram.Context == nil {
			engram.Context = make(map[string]string)
		}
		for k, v := range context {
			engram.Context[k] = v
		}
	} else {
		
		hashes := make([]string, len(concepts))
		for i, c := range concepts {
			hashes[i] = ConceptHash(c)
		}

		engram = &Engram{
			ID:                id,
			Concepts:          concepts,
			Hashes:            hashes,
			Strength:          0.3, 
			Resonance:         0.5, 
			Created:           time.Now(),
			LastUsed:          time.Now(),
			Activations:       1,
			FormationPressure: pressure,
			Context:           context,
		}
		em.Engrams[id] = engram
		em.TotalEngrams++

		
		for _, hash := range hashes {
			em.ConceptIndex[hash] = append(em.ConceptIndex[hash], id)
		}

		
		if len(em.Engrams) > em.MaxEngrams {
			em.pruneWeakest()
		}
	}

	em.TotalActivations++
	return engram
}


func (em *EngramMemory) Resonate(concepts []string, pressure PressureState) []ResonanceResult {
	em.mu.RLock()
	defer em.mu.RUnlock()

	
	inputHashes := make(map[string]bool)
	for _, c := range concepts {
		inputHashes[ConceptHash(c)] = true
	}

	
	candidates := make(map[string]int) 
	for hash := range inputHashes {
		for _, engramID := range em.ConceptIndex[hash] {
			candidates[engramID]++
		}
	}

	
	results := make([]ResonanceResult, 0)
	for engramID, matchCount := range candidates {
		if matchCount < em.MinConceptsMatch {
			continue
		}

		engram := em.Engrams[engramID]
		if engram == nil {
			continue
		}

		
		score := em.calculateResonance(engram, matchCount, len(inputHashes), pressure)
		if score > 0.1 { 
			results = append(results, ResonanceResult{
				Engram:     engram,
				Score:      score,
				MatchCount: matchCount,
			})
		}
	}

	
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}


func (em *EngramMemory) calculateResonance(engram *Engram, matchCount, inputCount int, pressure PressureState) float64 {
	
	overlapRatio := float64(matchCount) / float64(len(engram.Concepts))
	inputCoverage := float64(matchCount) / float64(inputCount)
	baseScore := (overlapRatio + inputCoverage) / 2

	
	daysSinceUse := time.Since(engram.LastUsed).Hours() / 24
	decayedResonance := engram.Resonance * math.Pow(1-em.ResonanceDecay, daysSinceUse)

	
	decayedStrength := engram.Strength * math.Pow(1-em.DecayRate, daysSinceUse)

	score := baseScore * decayedResonance * decayedStrength

	
	pressureSimilarity := em.pressureSimilarity(engram.FormationPressure, pressure)
	if pressure.Coherence > 0.5 {
		score *= (0.5 + pressureSimilarity*0.5)
	}

	
	if pressure.Tension > 0.5 {
		score *= (1.5 - pressure.Tension)
	}

	return score
}


func (em *EngramMemory) pressureSimilarity(a, b PressureState) float64 {
	dM := math.Abs(a.Magnitude - b.Magnitude)
	dC := math.Abs(a.Coherence - b.Coherence)
	dT := math.Abs(a.Tension - b.Tension)
	dD := math.Abs(a.Density - b.Density)

	avgDiff := (dM + dC + dT + dD) / 4
	return 1 - avgDiff
}


func (em *EngramMemory) ActivateEngram(engramID string) {
	em.mu.Lock()
	defer em.mu.Unlock()

	if engram, ok := em.Engrams[engramID]; ok {
		engram.Activations++
		engram.LastUsed = time.Now()
		engram.Strength += 0.05
		if engram.Strength > 1.0 {
			engram.Strength = 1.0
		}
		engram.Resonance += 0.02
		if engram.Resonance > 1.0 {
			engram.Resonance = 1.0
		}
		em.TotalActivations++
	}
}


func (em *EngramMemory) GetAssociatedConcepts(concepts []string, pressure PressureState, maxResults int) []string {
	resonating := em.Resonate(concepts, pressure)

	
	inputSet := make(map[string]bool)
	for _, c := range concepts {
		inputSet[c] = true
	}

	associated := make(map[string]float64) 
	for _, result := range resonating {
		for _, c := range result.Engram.Concepts {
			if !inputSet[c] {
				associated[c] += result.Score
			}
		}
	}

	
	type scoredConcept struct {
		concept string
		score   float64
	}
	scored := make([]scoredConcept, 0, len(associated))
	for c, s := range associated {
		scored = append(scored, scoredConcept{c, s})
	}
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	
	result := make([]string, 0, maxResults)
	for i := 0; i < len(scored) && i < maxResults; i++ {
		result = append(result, scored[i].concept)
	}

	return result
}


func (em *EngramMemory) pruneWeakest() {
	

	type scored struct {
		id    string
		score float64
	}
	scores := make([]scored, 0, len(em.Engrams))

	for id, e := range em.Engrams {
		daysSinceUse := time.Since(e.LastUsed).Hours() / 24
		decayedStrength := e.Strength * math.Pow(1-em.DecayRate, daysSinceUse)
		scores = append(scores, scored{id, decayedStrength})
	}

	
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score < scores[j].score
	})

	
	toRemove := len(scores) / 10
	if toRemove < 1 {
		toRemove = 1
	}

	for i := 0; i < toRemove && i < len(scores); i++ {
		em.removeEngram(scores[i].id)
	}
}


func (em *EngramMemory) removeEngram(id string) {
	engram, ok := em.Engrams[id]
	if !ok {
		return
	}

	
	for _, hash := range engram.Hashes {
		if ids, ok := em.ConceptIndex[hash]; ok {
			newIDs := make([]string, 0, len(ids)-1)
			for _, eid := range ids {
				if eid != id {
					newIDs = append(newIDs, eid)
				}
			}
			em.ConceptIndex[hash] = newIDs
		}
	}

	delete(em.Engrams, id)
	em.TotalEngrams--
}


func (em *EngramMemory) Decay() int {
	em.mu.Lock()
	defer em.mu.Unlock()

	pruned := 0
	toDelete := make([]string, 0)

	for id, e := range em.Engrams {
		daysSinceUse := time.Since(e.LastUsed).Hours() / 24
		e.Strength *= math.Pow(1-em.DecayRate, daysSinceUse)
		e.Resonance *= math.Pow(1-em.ResonanceDecay, daysSinceUse)

		if e.Strength < em.MinStrength {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		em.removeEngram(id)
		pruned++
	}

	return pruned
}


func (em *EngramMemory) Stats() EngramStats {
	em.mu.RLock()
	defer em.mu.RUnlock()

	totalStrength := 0.0
	strongestStrength := 0.0
	var strongestEngram *Engram

	for _, e := range em.Engrams {
		totalStrength += e.Strength
		if e.Strength > strongestStrength {
			strongestStrength = e.Strength
			strongestEngram = e
		}
	}

	stats := EngramStats{
		TotalEngrams:     len(em.Engrams),
		TotalActivations: em.TotalActivations,
		AverageStrength:  0,
	}

	if len(em.Engrams) > 0 {
		stats.AverageStrength = totalStrength / float64(len(em.Engrams))
	}

	if strongestEngram != nil {
		stats.StrongestID = strongestEngram.ID
		stats.StrongestConcepts = strongestEngram.Concepts
		stats.StrongestStrength = strongestEngram.Strength
	}

	return stats
}


type ResonanceResult struct {
	Engram     *Engram `json:"engram"`
	Score      float64 `json:"score"`
	MatchCount int     `json:"match_count"`
}


type EngramStats struct {
	TotalEngrams      int      `json:"total_engrams"`
	TotalActivations  int64    `json:"total_activations"`
	AverageStrength   float64  `json:"average_strength"`
	StrongestID       string   `json:"strongest_id"`
	StrongestConcepts []string `json:"strongest_concepts"`
	StrongestStrength float64  `json:"strongest_strength"`
}
