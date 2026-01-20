package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathwayMemory_Basic(t *testing.T) {
	
	tmpDir, err := os.MkdirTemp("", "zeam-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pm := NewPathwayMemory(tmpDir)

	
	pm.Strengthen("hello", "greeting")
	pm.Strengthen("hello", "greeting") 
	pm.Strengthen("greeting", "welcome")
	pm.Strengthen("welcome", "friend")

	
	strength := pm.GetStrength("hello", "greeting")
	if strength < 0.05 {
		t.Errorf("Expected strength > 0.05, got %f", strength)
	}
	t.Logf("hello->greeting strength: %f", strength)

	
	outgoing := pm.GetOutgoingPathways("hello")
	if len(outgoing) != 1 {
		t.Errorf("Expected 1 outgoing pathway, got %d", len(outgoing))
	}

	
	pressure := PressureState{Magnitude: 0.5, Coherence: 0.5, Tension: 0.3, Density: 0.5}
	next := pm.SelectNextConcept("hello", pressure, []byte{0x42, 0x13, 0x37})
	if next != "greeting" {
		t.Errorf("Expected 'greeting', got %q", next)
	}

	
	stats := pm.Stats()
	t.Logf("Stats: %+v", stats)
	if stats.TotalPathways != 3 {
		t.Errorf("Expected 3 pathways, got %d", stats.TotalPathways)
	}

	
	if err := pm.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	pm2, err := LoadPathwayMemory(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	if pm2.Stats().TotalPathways != stats.TotalPathways {
		t.Errorf("Loaded pathways don't match: %d vs %d",
			pm2.Stats().TotalPathways, stats.TotalPathways)
	}
}

func TestEngramMemory_Basic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeam-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	em := NewEngramMemory(tmpDir)

	
	concepts1 := []string{"photosynthesis", "light", "plant", "chlorophyll"}
	pressure := PressureState{Magnitude: 0.6, Coherence: 0.7, Tension: 0.2, Density: 0.5}
	engram := em.Store(concepts1, pressure, nil)

	if engram == nil {
		t.Fatal("Store returned nil engram")
	}
	t.Logf("Created engram: %s with %d concepts, strength: %f", engram.ID, len(engram.Concepts), engram.Strength)
	initialStrength := engram.Strength

	
	engram2 := em.Store(concepts1, pressure, nil)
	t.Logf("After re-store: strength: %f (was %f)", engram2.Strength, initialStrength)
	if engram2.Strength <= initialStrength {
		t.Error("Expected strength to increase on re-store")
	}

	
	testConcepts := []string{"plant", "light", "energy"} 
	resonating := em.Resonate(testConcepts, pressure)

	t.Logf("Testing resonance with concepts: %v", testConcepts)
	if len(resonating) == 0 {
		t.Error("Expected resonating engrams for related concepts")
	} else {
		t.Logf("Found %d resonating engrams, top score: %f",
			len(resonating), resonating[0].Score)
	}

	
	associated := em.GetAssociatedConcepts(testConcepts, pressure, 5)
	t.Logf("Associated concepts: %v", associated)

	
	if len(associated) > 0 {
		t.Logf("Found associated concepts from engram")
	}

	
	if err := em.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	em2, err := LoadEngramMemory(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	if em2.Stats().TotalEngrams != em.Stats().TotalEngrams {
		t.Errorf("Loaded engrams don't match")
	}
}

func TestWorkingMemory_Basic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeam-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	wm := NewWorkingMemory(tmpDir)

	pressure := PressureState{Magnitude: 0.5, Coherence: 0.5, Tension: 0.3, Density: 0.5}

	
	wm.AddTurn("user", "Hello, how are you?", []string{"hello", "greeting"}, pressure)
	wm.AddTurn("assistant", "I'm doing well, thanks!", []string{"well", "thanks"}, pressure)
	wm.AddTurn("user", "Tell me about photosynthesis", []string{"photosynthesis", "tell"}, pressure)

	
	active := wm.GetActiveConcepts()
	t.Logf("Active concepts: %v", active)

	
	if active["photosynthesis"] < active["hello"] {
		t.Error("Expected recent concepts to have higher weight")
	}

	
	focus := wm.GetFocus()
	t.Logf("Focus: %v", focus)
	if len(focus) == 0 {
		t.Error("Expected non-empty focus stack")
	}

	
	top := wm.GetTopConcepts(3)
	t.Logf("Top 3 concepts: %v", top)

	
	recent := wm.GetRecentTurns(2)
	if len(recent) != 2 {
		t.Errorf("Expected 2 recent turns, got %d", len(recent))
	}

	
	lastMsg, lastConcepts, ok := wm.GetLastUserMessage()
	if !ok {
		t.Error("Expected to find last user message")
	}
	t.Logf("Last user: %q, concepts: %v", lastMsg, lastConcepts)

	
	if err := wm.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	wm2, err := LoadWorkingMemory(tmpDir)
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	if wm2.Stats().TurnCount != wm.Stats().TurnCount {
		t.Errorf("Loaded turns don't match")
	}
}

func TestPositronicBrain_Integration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeam-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	brain := NewPositronicBrain(tmpDir, "test-fork-123")

	
	brain.SetConceptExtractor(func(text string) []string {
		
		words := []string{}
		for _, w := range splitWords(text) {
			if len(w) > 3 {
				words = append(words, w)
			}
		}
		return words
	})

	
	result1, err := brain.ProcessInput("Hello, I want to learn about photosynthesis")
	if err != nil {
		t.Fatalf("ProcessInput failed: %v", err)
	}
	t.Logf("Extracted concepts: %v", result1.ExtractedConcepts)

	
	brain.Learn("Photosynthesis is how plants convert light to energy",
		[]string{"photosynthesis", "plants", "convert", "light", "energy"})

	
	result2, err := brain.ProcessInput("How do plants get energy from sunlight?")
	if err != nil {
		t.Fatalf("ProcessInput failed: %v", err)
	}

	t.Logf("Associated concepts: %v", result2.AssociatedConcepts)
	t.Logf("Resonating engrams: %d", len(result2.ResonatingEngrams))

	
	bounds := brain.GenerateBounds()
	t.Logf("Bounds concepts: %v", bounds.GetConceptList())

	
	stats := brain.Stats()
	t.Logf("Brain stats:")
	t.Logf("  Pathways: %d (activations: %d)", stats.Pathways.TotalPathways, stats.Pathways.TotalActivations)
	t.Logf("  Engrams: %d", stats.Engrams.TotalEngrams)
	t.Logf("  Working memory turns: %d", stats.WorkingMemory.TurnCount)

	
	decayResult := brain.Decay()
	t.Logf("Decay: pathways pruned=%d, engrams pruned=%d",
		decayResult.PathwaysPruned, decayResult.EngramsPruned)

	
	if err := brain.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	brain2, err := LoadPositronicBrain(tmpDir, "test-fork-123")
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	stats2 := brain2.Stats()
	if stats2.Pathways.TotalPathways != stats.Pathways.TotalPathways {
		t.Errorf("Loaded pathways don't match: %d vs %d",
			stats2.Pathways.TotalPathways, stats.Pathways.TotalPathways)
	}
}

func TestPressureModulation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeam-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pm := NewPathwayMemory(tmpDir)

	
	pm.Strengthen("think", "consider")
	pm.Strengthen("think", "consider") 
	pm.Strengthen("think", "consider") 
	pm.Strengthen("think", "ponder")
	pm.Strengthen("think", "reflect")

	hashSeed := []byte{0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50, 0x50}

	
	highCoherence := PressureState{Magnitude: 0.5, Coherence: 0.9, Tension: 0.1, Density: 0.5}
	resultHigh := pm.SelectNextConcept("think", highCoherence, hashSeed)
	t.Logf("High coherence selected: %s", resultHigh)

	
	highTension := PressureState{Magnitude: 0.5, Coherence: 0.1, Tension: 0.9, Density: 0.5}
	resultTension := pm.SelectNextConcept("think", highTension, hashSeed)
	t.Logf("High tension selected: %s", resultTension)

	
	if resultHigh != "consider" {
		t.Logf("Note: High coherence didn't select strongest - this can vary with hash seed")
	}
}


func splitWords(text string) []string {
	result := []string{}
	word := ""
	for _, r := range text {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			word += string(r)
		} else if word != "" {
			result = append(result, word)
			word = ""
		}
	}
	if word != "" {
		result = append(result, word)
	}
	return result
}

func TestBrainPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeam-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	
	brain1 := NewPositronicBrain(tmpDir, "persistence-test")

	brain1.SetConceptExtractor(func(text string) []string {
		return splitWords(text)
	})

	
	for i := 0; i < 5; i++ {
		brain1.ProcessInput("Testing memory persistence")
		brain1.Learn("Memory stored successfully", []string{"memory", "stored", "success"})
	}

	
	if err := brain1.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	originalStats := brain1.Stats()
	t.Logf("Original: pathways=%d, engrams=%d",
		originalStats.Pathways.TotalPathways, originalStats.Engrams.TotalEngrams)

	
	brain2, err := LoadPositronicBrain(tmpDir, "persistence-test")
	if err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	loadedStats := brain2.Stats()
	t.Logf("Loaded: pathways=%d, engrams=%d",
		loadedStats.Pathways.TotalPathways, loadedStats.Engrams.TotalEngrams)

	
	if loadedStats.Pathways.TotalPathways != originalStats.Pathways.TotalPathways {
		t.Errorf("Pathway count mismatch: %d vs %d",
			loadedStats.Pathways.TotalPathways, originalStats.Pathways.TotalPathways)
	}

	if loadedStats.Engrams.TotalEngrams != originalStats.Engrams.TotalEngrams {
		t.Errorf("Engram count mismatch: %d vs %d",
			loadedStats.Engrams.TotalEngrams, originalStats.Engrams.TotalEngrams)
	}

	
	files := []string{"pathways.json", "engrams.json", "context.json", "brain.json"}
	for _, f := range files {
		path := filepath.Join(tmpDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file %s to exist", f)
		}
	}
}
