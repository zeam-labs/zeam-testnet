

package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)


type PositronicBrain struct {
	mu sync.RWMutex

	
	Pathways *PathwayMemory
	Engrams  *EngramMemory
	Context  *WorkingMemory

	
	Pressure PressureState

	
	ForkID       string    `json:"fork_id"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`

	
	storagePath string

	
	conceptExtractor func(text string) []string
}


func NewPositronicBrain(storagePath string, forkID string) *PositronicBrain {
	return &PositronicBrain{
		Pathways:     NewPathwayMemory(storagePath),
		Engrams:      NewEngramMemory(storagePath),
		Context:      NewWorkingMemory(storagePath),
		Pressure:     PressureState{Magnitude: 0.5, Coherence: 0.5, Tension: 0.5, Density: 0.5},
		ForkID:       forkID,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		storagePath:  storagePath,
	}
}


func LoadPositronicBrain(storagePath string, forkID string) (*PositronicBrain, error) {
	brain := NewPositronicBrain(storagePath, forkID)

	
	var err error

	brain.Pathways, err = LoadPathwayMemory(storagePath)
	if err != nil {
		return nil, err
	}

	brain.Engrams, err = LoadEngramMemory(storagePath)
	if err != nil {
		return nil, err
	}

	brain.Context, err = LoadWorkingMemory(storagePath)
	if err != nil {
		return nil, err
	}

	
	metaPath := filepath.Join(storagePath, "brain.json")
	if data, err := os.ReadFile(metaPath); err == nil {
		json.Unmarshal(data, brain)
	}

	brain.storagePath = storagePath
	brain.ForkID = forkID

	return brain, nil
}


func (b *PositronicBrain) Save() error {
	b.mu.Lock()
	b.LastActivity = time.Now()
	b.mu.Unlock()

	
	if err := b.Pathways.Save(); err != nil {
		return err
	}
	if err := b.Engrams.Save(); err != nil {
		return err
	}
	if err := b.Context.Save(); err != nil {
		return err
	}

	
	if err := os.MkdirAll(b.storagePath, 0700); err != nil {
		return err
	}

	meta := struct {
		ForkID       string    `json:"fork_id"`
		CreatedAt    time.Time `json:"created_at"`
		LastActivity time.Time `json:"last_activity"`
	}{
		ForkID:       b.ForkID,
		CreatedAt:    b.CreatedAt,
		LastActivity: b.LastActivity,
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	metaPath := filepath.Join(b.storagePath, "brain.json")
	return os.WriteFile(metaPath, data, 0600)
}


func (b *PositronicBrain) SetConceptExtractor(fn func(text string) []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.conceptExtractor = fn
}


func (b *PositronicBrain) SetPressure(p PressureState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Pressure = p
}


func (b *PositronicBrain) GetPressure() PressureState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.Pressure
}


func (b *PositronicBrain) ProcessInput(text string) (*InputProcessingResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	result := &InputProcessingResult{
		OriginalText: text,
		Timestamp:    time.Now(),
	}

	
	concepts := b.extractConcepts(text)
	result.ExtractedConcepts = concepts

	
	b.Context.AddTurn("user", text, concepts, b.Pressure)

	
	resonating := b.Engrams.Resonate(concepts, b.Pressure)
	result.ResonatingEngrams = resonating

	
	associated := b.Engrams.GetAssociatedConcepts(concepts, b.Pressure, 10)
	result.AssociatedConcepts = associated

	
	for _, r := range resonating {
		if r.Score > 0.3 { 
			b.Engrams.ActivateEngram(r.Engram.ID)
		}
	}

	
	pathwaySuggestions := make(map[string]float64)
	for _, c := range concepts {
		next := b.Pathways.SelectNextConcept(c, b.Pressure, nil)
		if next != "" {
			strength := b.Pathways.GetStrength(c, next)
			pathwaySuggestions[next] = strength
		}
	}
	result.PathwaySuggestions = pathwaySuggestions

	return result, nil
}


func (b *PositronicBrain) GenerateBounds() *MemoryBounds {
	b.mu.RLock()
	defer b.mu.RUnlock()

	bounds := &MemoryBounds{
		Concepts:    make(map[string]float64),
		FocusBias:   make([]string, 0),
		PressureMod: b.Pressure,
	}

	
	for _, c := range b.Context.GetFocus() {
		bounds.Concepts[c] = 1.0
		bounds.FocusBias = append(bounds.FocusBias, c)
	}

	
	for c, w := range b.Context.GetActiveConcepts() {
		if existing, ok := bounds.Concepts[c]; ok {
			bounds.Concepts[c] = max(existing, w)
		} else {
			bounds.Concepts[c] = w
		}
	}

	
	topConcepts := b.Context.GetTopConcepts(5)
	associated := b.Engrams.GetAssociatedConcepts(topConcepts, b.Pressure, 10)
	for _, c := range associated {
		if _, ok := bounds.Concepts[c]; !ok {
			bounds.Concepts[c] = 0.3 
		}
	}

	
	for _, c := range topConcepts {
		pathways := b.Pathways.GetOutgoingPathways(c)
		for _, p := range pathways {
			if _, ok := bounds.Concepts[p.ToWord]; !ok {
				bounds.Concepts[p.ToWord] = p.Strength * 0.5 
			}
		}
	}

	
	if b.Pressure.Coherence > 0.7 {
		for c, w := range bounds.Concepts {
			if w < 0.5 {
				bounds.Concepts[c] = w * (1 - (b.Pressure.Coherence - 0.7))
			}
		}
	}

	
	if b.Pressure.Tension > 0.7 {
		for c, w := range bounds.Concepts {
			if w < 0.5 {
				bounds.Concepts[c] = w * (1 + (b.Pressure.Tension - 0.7))
			}
		}
	}

	return bounds
}


func (b *PositronicBrain) Learn(response string, outputConcepts []string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	
	b.Context.AddTurn("assistant", response, outputConcepts, b.Pressure)

	
	b.Pathways.StrengthenRoute(outputConcepts)

	
	lastInput, inputConcepts, ok := b.Context.GetLastUserMessage()
	if ok && lastInput != "" {
		for _, ic := range inputConcepts {
			for _, oc := range outputConcepts {
				if ic != oc {
					b.Pathways.Strengthen(ic, oc)
				}
			}
		}
	}

	
	allConcepts := make([]string, 0, len(inputConcepts)+len(outputConcepts))
	seen := make(map[string]bool)
	for _, c := range inputConcepts {
		if !seen[c] {
			allConcepts = append(allConcepts, c)
			seen[c] = true
		}
	}
	for _, c := range outputConcepts {
		if !seen[c] {
			allConcepts = append(allConcepts, c)
			seen[c] = true
		}
	}

	if len(allConcepts) >= 3 {
		b.Engrams.Store(allConcepts, b.Pressure, nil)
	}
}


func (b *PositronicBrain) extractConcepts(text string) []string {
	if b.conceptExtractor != nil {
		return b.conceptExtractor(text)
	}

	
	words := strings.Fields(strings.ToLower(text))
	concepts := make([]string, 0)
	for _, w := range words {
		
		w = strings.Trim(w, ".,!?;:\"'()[]{}")
		if len(w) > 3 { 
			concepts = append(concepts, w)
		}
	}
	return concepts
}


func (b *PositronicBrain) Decay() DecayResult {
	pathwaysPruned := b.Pathways.Decay()
	engramsPruned := b.Engrams.Decay()

	return DecayResult{
		PathwaysPruned: pathwaysPruned,
		EngramsPruned:  engramsPruned,
	}
}


func (b *PositronicBrain) Stats() BrainStats {
	return BrainStats{
		ForkID:        b.ForkID,
		CreatedAt:     b.CreatedAt,
		LastActivity:  b.LastActivity,
		Pathways:      b.Pathways.Stats(),
		Engrams:       b.Engrams.Stats(),
		WorkingMemory: b.Context.Stats(),
		Pressure:      b.GetPressure(),
	}
}


func (b *PositronicBrain) ClearContext() {
	b.Context.Clear()
}


func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}


type InputProcessingResult struct {
	OriginalText       string                 `json:"original_text"`
	ExtractedConcepts  []string               `json:"extracted_concepts"`
	ResonatingEngrams  []ResonanceResult      `json:"resonating_engrams"`
	AssociatedConcepts []string               `json:"associated_concepts"`
	PathwaySuggestions map[string]float64     `json:"pathway_suggestions"`
	Timestamp          time.Time              `json:"timestamp"`
}


type MemoryBounds struct {
	Concepts    map[string]float64 `json:"concepts"`     
	FocusBias   []string           `json:"focus_bias"`   
	PressureMod PressureState      `json:"pressure_mod"` 
}


func (mb *MemoryBounds) GetConceptList() []string {
	concepts := make([]string, 0, len(mb.Concepts))
	for c := range mb.Concepts {
		concepts = append(concepts, c)
	}
	return concepts
}


func (mb *MemoryBounds) GetWeightedConcepts(threshold float64) map[string]float64 {
	result := make(map[string]float64)
	for c, w := range mb.Concepts {
		if w >= threshold {
			result[c] = w
		}
	}
	return result
}


type DecayResult struct {
	PathwaysPruned int `json:"pathways_pruned"`
	EngramsPruned  int `json:"engrams_pruned"`
}


type BrainStats struct {
	ForkID        string             `json:"fork_id"`
	CreatedAt     time.Time          `json:"created_at"`
	LastActivity  time.Time          `json:"last_activity"`
	Pathways      PathwayStats       `json:"pathways"`
	Engrams       EngramStats        `json:"engrams"`
	WorkingMemory WorkingMemoryStats `json:"working_memory"`
	Pressure      PressureState      `json:"pressure"`
}
