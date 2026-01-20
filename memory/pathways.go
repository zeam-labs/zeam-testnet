

package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)


type Pathway struct {
	FromHash   string    `json:"from_hash"`   
	ToHash     string    `json:"to_hash"`     
	FromWord   string    `json:"from_word"`   
	ToWord     string    `json:"to_word"`     
	Strength   float64   `json:"strength"`    
	Activations int      `json:"activations"` 
	LastUsed   time.Time `json:"last_used"`   
	Created    time.Time `json:"created"`     
}


type PathwayMemory struct {
	mu sync.RWMutex

	
	Pathways map[string]*Pathway `json:"pathways"`

	
	BySource map[string][]string `json:"by_source"` 

	
	TotalActivations int64     `json:"total_activations"`
	CreatedAt        time.Time `json:"created_at"`
	LastModified     time.Time `json:"last_modified"`

	
	DecayRate      float64 `json:"decay_rate"`       
	ReinforceDelta float64 `json:"reinforce_delta"`  
	MaxStrength    float64 `json:"max_strength"`     
	MinStrength    float64 `json:"min_strength"`     

	
	storagePath string
}


func NewPathwayMemory(storagePath string) *PathwayMemory {
	pm := &PathwayMemory{
		Pathways:       make(map[string]*Pathway),
		BySource:       make(map[string][]string),
		CreatedAt:      time.Now(),
		LastModified:   time.Now(),
		DecayRate:      0.01,  
		ReinforceDelta: 0.05,  
		MaxStrength:    1.0,
		MinStrength:    0.001, 
		storagePath:    storagePath,
	}
	return pm
}


func LoadPathwayMemory(storagePath string) (*PathwayMemory, error) {
	pm := NewPathwayMemory(storagePath)

	filePath := filepath.Join(storagePath, "pathways.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return pm, nil 
		}
		return nil, err
	}

	if err := json.Unmarshal(data, pm); err != nil {
		return nil, err
	}

	pm.storagePath = storagePath
	return pm, nil
}


func (pm *PathwayMemory) Save() error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if err := os.MkdirAll(pm.storagePath, 0700); err != nil {
		return err
	}

	pm.LastModified = time.Now()

	data, err := json.MarshalIndent(pm, "", "  ")
	if err != nil {
		return err
	}

	filePath := filepath.Join(pm.storagePath, "pathways.json")
	return os.WriteFile(filePath, data, 0600)
}


func pathwayKey(fromHash, toHash string) string {
	return fromHash + ":" + toHash
}


func ConceptHash(word string) string {
	h := sha256.Sum256([]byte(word))
	return hex.EncodeToString(h[:8]) 
}


func (pm *PathwayMemory) Strengthen(fromWord, toWord string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	fromHash := ConceptHash(fromWord)
	toHash := ConceptHash(toWord)
	key := pathwayKey(fromHash, toHash)

	pathway, exists := pm.Pathways[key]
	if !exists {
		pathway = &Pathway{
			FromHash: fromHash,
			ToHash:   toHash,
			FromWord: fromWord,
			ToWord:   toWord,
			Strength: pm.ReinforceDelta,
			Created:  time.Now(),
		}
		pm.Pathways[key] = pathway

		
		pm.BySource[fromHash] = append(pm.BySource[fromHash], key)
	} else {
		
		pathway.Strength += pm.ReinforceDelta
		if pathway.Strength > pm.MaxStrength {
			pathway.Strength = pm.MaxStrength
		}
	}

	pathway.Activations++
	pathway.LastUsed = time.Now()
	pm.TotalActivations++
}


func (pm *PathwayMemory) StrengthenRoute(words []string) {
	for i := 0; i < len(words)-1; i++ {
		pm.Strengthen(words[i], words[i+1])
	}
}


func (pm *PathwayMemory) GetStrength(fromWord, toWord string) float64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	fromHash := ConceptHash(fromWord)
	toHash := ConceptHash(toWord)
	key := pathwayKey(fromHash, toHash)

	pathway, exists := pm.Pathways[key]
	if !exists {
		return 0
	}

	
	daysSinceUse := time.Since(pathway.LastUsed).Hours() / 24
	decayFactor := math.Pow(1-pm.DecayRate, daysSinceUse)

	return pathway.Strength * decayFactor
}


func (pm *PathwayMemory) GetOutgoingPathways(fromWord string) []*Pathway {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	fromHash := ConceptHash(fromWord)
	keys := pm.BySource[fromHash]

	result := make([]*Pathway, 0, len(keys))
	for _, key := range keys {
		if p := pm.Pathways[key]; p != nil {
			
			daysSinceUse := time.Since(p.LastUsed).Hours() / 24
			decayFactor := math.Pow(1-pm.DecayRate, daysSinceUse)

			clone := *p
			clone.Strength *= decayFactor
			result = append(result, &clone)
		}
	}

	return result
}


func (pm *PathwayMemory) SelectNextConcept(fromWord string, pressure PressureState, hashSeed []byte) string {
	pathways := pm.GetOutgoingPathways(fromWord)
	if len(pathways) == 0 {
		return ""
	}

	
	weights := make([]float64, len(pathways))
	totalWeight := 0.0

	for i, p := range pathways {
		weight := p.Strength

		
		if pressure.Coherence > 0.5 {
			
			coherenceFactor := 1.0 + (pressure.Coherence-0.5)*2
			weight = math.Pow(weight, 1/coherenceFactor)
		}

		
		if pressure.Tension > 0.5 {
			
			tensionFactor := 1.0 + (pressure.Tension-0.5)*2
			weight = math.Pow(weight, tensionFactor)
		}

		
		weight *= (0.5 + pressure.Magnitude*0.5)

		weights[i] = weight
		totalWeight += weight
	}

	if totalWeight == 0 {
		return ""
	}

	
	var selector float64
	if len(hashSeed) >= 8 {
		
		var val uint64
		for i := 0; i < 8; i++ {
			val = (val << 8) | uint64(hashSeed[i])
		}
		selector = float64(val) / float64(^uint64(0))
	} else {
		selector = 0.5 
	}

	
	threshold := selector * totalWeight
	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if cumulative >= threshold {
			return pathways[i].ToWord
		}
	}

	return pathways[len(pathways)-1].ToWord
}


func (pm *PathwayMemory) Decay() int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pruned := 0
	toDelete := make([]string, 0)

	for key, p := range pm.Pathways {
		daysSinceUse := time.Since(p.LastUsed).Hours() / 24
		decayFactor := math.Pow(1-pm.DecayRate, daysSinceUse)
		p.Strength *= decayFactor

		if p.Strength < pm.MinStrength {
			toDelete = append(toDelete, key)
		}
	}

	
	for _, key := range toDelete {
		p := pm.Pathways[key]
		delete(pm.Pathways, key)

		
		if keys, ok := pm.BySource[p.FromHash]; ok {
			newKeys := make([]string, 0, len(keys)-1)
			for _, k := range keys {
				if k != key {
					newKeys = append(newKeys, k)
				}
			}
			pm.BySource[p.FromHash] = newKeys
		}

		pruned++
	}

	return pruned
}


func (pm *PathwayMemory) Stats() PathwayStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	totalStrength := 0.0
	strongestStrength := 0.0
	var strongestPathway *Pathway

	for _, p := range pm.Pathways {
		totalStrength += p.Strength
		if p.Strength > strongestStrength {
			strongestStrength = p.Strength
			strongestPathway = p
		}
	}

	stats := PathwayStats{
		TotalPathways:    len(pm.Pathways),
		TotalActivations: pm.TotalActivations,
		AverageStrength:  0,
	}

	if len(pm.Pathways) > 0 {
		stats.AverageStrength = totalStrength / float64(len(pm.Pathways))
	}

	if strongestPathway != nil {
		stats.StrongestFrom = strongestPathway.FromWord
		stats.StrongestTo = strongestPathway.ToWord
		stats.StrongestStrength = strongestPathway.Strength
	}

	return stats
}


type PathwayStats struct {
	TotalPathways     int     `json:"total_pathways"`
	TotalActivations  int64   `json:"total_activations"`
	AverageStrength   float64 `json:"average_strength"`
	StrongestFrom     string  `json:"strongest_from"`
	StrongestTo       string  `json:"strongest_to"`
	StrongestStrength float64 `json:"strongest_strength"`
}


type PressureState struct {
	Magnitude float64 `json:"magnitude"` 
	Coherence float64 `json:"coherence"` 
	Tension   float64 `json:"tension"`   
	Density   float64 `json:"density"`   
}
