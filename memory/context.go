

package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)


type ConversationTurn struct {
	Role      string    `json:"role"`      
	Content   string    `json:"content"`   
	Concepts  []string  `json:"concepts"`  
	Timestamp time.Time `json:"timestamp"`
	Pressure  PressureState `json:"pressure"` 
}


type WorkingMemory struct {
	mu sync.RWMutex

	
	Turns []ConversationTurn `json:"turns"`

	
	ActiveConcepts map[string]float64 `json:"active_concepts"` 

	
	FocusStack []string `json:"focus_stack"`

	
	SessionID    string    `json:"session_id"`
	SessionStart time.Time `json:"session_start"`
	LastActivity time.Time `json:"last_activity"`

	
	MaxTurns       int     `json:"max_turns"`       
	ConceptDecay   float64 `json:"concept_decay"`   
	MaxConcepts    int     `json:"max_concepts"`    
	FocusStackSize int     `json:"focus_stack_size"`

	
	storagePath string
}


func NewWorkingMemory(storagePath string) *WorkingMemory {
	return &WorkingMemory{
		Turns:          make([]ConversationTurn, 0),
		ActiveConcepts: make(map[string]float64),
		FocusStack:     make([]string, 0),
		SessionID:      generateSessionID(),
		SessionStart:   time.Now(),
		LastActivity:   time.Now(),
		MaxTurns:       50,    
		ConceptDecay:   0.8,   
		MaxConcepts:    100,   
		FocusStackSize: 5,
		storagePath:    storagePath,
	}
}


func generateSessionID() string {
	h := ConceptHash(time.Now().String())
	return h[:16]
}


func LoadWorkingMemory(storagePath string) (*WorkingMemory, error) {
	wm := NewWorkingMemory(storagePath)

	filePath := filepath.Join(storagePath, "context.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return wm, nil 
		}
		return nil, err
	}

	if err := json.Unmarshal(data, wm); err != nil {
		return nil, err
	}

	wm.storagePath = storagePath

	
	if time.Since(wm.LastActivity) > time.Hour {
		
		wm.Turns = wm.Turns[:0]
		wm.SessionID = generateSessionID()
		wm.SessionStart = time.Now()
		
		for c := range wm.ActiveConcepts {
			wm.ActiveConcepts[c] *= 0.3
		}
	}

	return wm, nil
}


func (wm *WorkingMemory) Save() error {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	if err := os.MkdirAll(wm.storagePath, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(wm, "", "  ")
	if err != nil {
		return err
	}

	filePath := filepath.Join(wm.storagePath, "context.json")
	return os.WriteFile(filePath, data, 0600)
}


func (wm *WorkingMemory) AddTurn(role, content string, concepts []string, pressure PressureState) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	turn := ConversationTurn{
		Role:      role,
		Content:   content,
		Concepts:  concepts,
		Timestamp: time.Now(),
		Pressure:  pressure,
	}

	
	for c := range wm.ActiveConcepts {
		wm.ActiveConcepts[c] *= wm.ConceptDecay
		if wm.ActiveConcepts[c] < 0.01 {
			delete(wm.ActiveConcepts, c)
		}
	}

	
	for _, c := range concepts {
		wm.ActiveConcepts[c] = 1.0
	}

	
	if len(wm.ActiveConcepts) > wm.MaxConcepts {
		wm.pruneWeakestConcepts()
	}

	
	wm.updateFocus(concepts)

	
	wm.Turns = append(wm.Turns, turn)

	
	if len(wm.Turns) > wm.MaxTurns {
		wm.Turns = wm.Turns[len(wm.Turns)-wm.MaxTurns:]
	}

	wm.LastActivity = time.Now()
}


func (wm *WorkingMemory) updateFocus(concepts []string) {
	
	newFocus := make([]string, 0, wm.FocusStackSize)

	
	for _, c := range concepts {
		if len(newFocus) >= wm.FocusStackSize {
			break
		}
		
		found := false
		for _, f := range newFocus {
			if f == c {
				found = true
				break
			}
		}
		if !found {
			newFocus = append(newFocus, c)
		}
	}

	
	for _, f := range wm.FocusStack {
		if len(newFocus) >= wm.FocusStackSize {
			break
		}
		found := false
		for _, n := range newFocus {
			if n == f {
				found = true
				break
			}
		}
		if !found {
			newFocus = append(newFocus, f)
		}
	}

	wm.FocusStack = newFocus
}


func (wm *WorkingMemory) pruneWeakestConcepts() {
	
	type weightedConcept struct {
		concept string
		weight  float64
	}
	list := make([]weightedConcept, 0, len(wm.ActiveConcepts))
	for c, w := range wm.ActiveConcepts {
		list = append(list, weightedConcept{c, w})
	}

	
	for i := 0; i < len(list)-1; i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].weight < list[i].weight {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	
	for i := 0; len(wm.ActiveConcepts) > wm.MaxConcepts && i < len(list); i++ {
		delete(wm.ActiveConcepts, list[i].concept)
	}
}


func (wm *WorkingMemory) GetActiveConcepts() map[string]float64 {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	
	result := make(map[string]float64, len(wm.ActiveConcepts))
	for c, w := range wm.ActiveConcepts {
		result[c] = w
	}
	return result
}


func (wm *WorkingMemory) GetTopConcepts(n int) []string {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	
	type weightedConcept struct {
		concept string
		weight  float64
	}
	list := make([]weightedConcept, 0, len(wm.ActiveConcepts))
	for c, w := range wm.ActiveConcepts {
		list = append(list, weightedConcept{c, w})
	}

	
	for i := 0; i < len(list)-1; i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].weight > list[i].weight {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	
	result := make([]string, 0, n)
	for i := 0; i < n && i < len(list); i++ {
		result = append(result, list[i].concept)
	}
	return result
}


func (wm *WorkingMemory) GetFocus() []string {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	result := make([]string, len(wm.FocusStack))
	copy(result, wm.FocusStack)
	return result
}


func (wm *WorkingMemory) GetRecentTurns(n int) []ConversationTurn {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	if n > len(wm.Turns) {
		n = len(wm.Turns)
	}

	result := make([]ConversationTurn, n)
	copy(result, wm.Turns[len(wm.Turns)-n:])
	return result
}


func (wm *WorkingMemory) GetLastUserMessage() (string, []string, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	for i := len(wm.Turns) - 1; i >= 0; i-- {
		if wm.Turns[i].Role == "user" {
			return wm.Turns[i].Content, wm.Turns[i].Concepts, true
		}
	}
	return "", nil, false
}


func (wm *WorkingMemory) GetLastResponse() (string, []string, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	for i := len(wm.Turns) - 1; i >= 0; i-- {
		if wm.Turns[i].Role == "assistant" {
			return wm.Turns[i].Content, wm.Turns[i].Concepts, true
		}
	}
	return "", nil, false
}


func (wm *WorkingMemory) Clear() {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	wm.Turns = wm.Turns[:0]
	wm.ActiveConcepts = make(map[string]float64)
	wm.FocusStack = wm.FocusStack[:0]
	wm.SessionID = generateSessionID()
	wm.SessionStart = time.Now()
	wm.LastActivity = time.Now()
}


func (wm *WorkingMemory) Stats() WorkingMemoryStats {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	return WorkingMemoryStats{
		SessionID:      wm.SessionID,
		TurnCount:      len(wm.Turns),
		ActiveConcepts: len(wm.ActiveConcepts),
		FocusItems:     len(wm.FocusStack),
		SessionAge:     time.Since(wm.SessionStart),
		LastActivity:   time.Since(wm.LastActivity),
	}
}


type WorkingMemoryStats struct {
	SessionID      string        `json:"session_id"`
	TurnCount      int           `json:"turn_count"`
	ActiveConcepts int           `json:"active_concepts"`
	FocusItems     int           `json:"focus_items"`
	SessionAge     time.Duration `json:"session_age"`
	LastActivity   time.Duration `json:"last_activity"`
}
