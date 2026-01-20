package llm

import (
	"testing"

	"zeam/quantum"
)

func TestHashNetwork_BasicOperations(t *testing.T) {
	
	network := quantum.NewHashNetwork(2, 128, 2, 256)

	
	concepts := []string{"hello", "world"}
	positions := []int{0, 1}

	
	embedded, err := network.HashEmbed(concepts, positions)
	if err != nil {
		t.Fatalf("HashEmbed failed: %v", err)
	}

	if len(embedded.Data) != 2 {
		t.Errorf("Expected 2 embeddings, got %d", len(embedded.Data))
	}

	for i, emb := range embedded.Data {
		if len(emb) != 32 {
			t.Errorf("Embedding %d has wrong size: %d (expected 32)", i, len(emb))
		}
	}

	t.Logf("Embedded %d concepts successfully", len(embedded.Data))
}

func TestHashNetwork_Forward(t *testing.T) {
	network := quantum.NewHashNetwork(2, 128, 2, 256)

	concepts := []string{"test", "input", "data"}

	indices, err := network.Forward(concepts)
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}

	if len(indices) != len(concepts) {
		t.Errorf("Expected %d indices, got %d", len(concepts), len(indices))
	}

	for i, idx := range indices {
		t.Logf("Index %d: category=%s synset=%d word=%d confidence=%.2f",
			i, idx.Category, idx.SynsetIndex, idx.WordIndex, idx.Confidence)
	}
}

func TestHashNetwork_PressureModulation(t *testing.T) {
	network := quantum.NewHashNetwork(2, 128, 2, 256)

	
	network.SetPressure(quantum.PressureMetrics{
		Magnitude: 0.8,
		Coherence: 0.9,
		Tension:   0.2,
		Density:   0.7,
	})

	concepts := []string{"pressure", "test"}
	indices1, err := network.Forward(concepts)
	if err != nil {
		t.Fatalf("Forward with high coherence failed: %v", err)
	}

	
	network.ResetContext()
	network.SetPressure(quantum.PressureMetrics{
		Magnitude: 0.3,
		Coherence: 0.2,
		Tension:   0.8,
		Density:   0.3,
	})

	indices2, err := network.Forward(concepts)
	if err != nil {
		t.Fatalf("Forward with low coherence failed: %v", err)
	}

	
	same := true
	for i := range indices1 {
		if indices1[i].SynsetIndex != indices2[i].SynsetIndex {
			same = false
			break
		}
	}

	t.Logf("High coherence vs low coherence results differ: %v", !same)
}

func TestHashPayload_EncodeDecode(t *testing.T) {
	original := &quantum.HashPayload{
		Version:     1,
		Type:        quantum.PayloadEmbed,
		LayerIdx:    2,
		Operation:   0,
		SeqPosition: 42,
		BatchIdx:    7,
		Pressure: quantum.PressureMetrics{
			Magnitude: 0.5,
			Coherence: 0.7,
			Tension:   0.3,
			Density:   0.6,
		},
		Data: []byte("test payload data"),
	}

	
	for i := 0; i < 32; i++ {
		original.InputHash[i] = byte(i)
	}

	encoded := original.Encode()
	t.Logf("Encoded payload: %d bytes", len(encoded))

	decoded, err := quantum.DecodePayload(encoded)
	if err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}

	if decoded.Version != original.Version {
		t.Errorf("Version mismatch: %d vs %d", decoded.Version, original.Version)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: %d vs %d", decoded.Type, original.Type)
	}
	if decoded.SeqPosition != original.SeqPosition {
		t.Errorf("SeqPosition mismatch: %d vs %d", decoded.SeqPosition, original.SeqPosition)
	}

	
	if abs(decoded.Pressure.Magnitude-original.Pressure.Magnitude) > 0.01 {
		t.Errorf("Magnitude mismatch: %.2f vs %.2f", decoded.Pressure.Magnitude, original.Pressure.Magnitude)
	}

	t.Logf("Payload encode/decode successful")
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestLocalHashDispatcher(t *testing.T) {
	dispatcher := &quantum.LocalHashDispatcher{NumNodes: 16}

	payload := []byte("test payload for dispatch")
	hashes, err := dispatcher.Dispatch(payload)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if len(hashes) != 16 {
		t.Errorf("Expected 16 hashes, got %d", len(hashes))
	}

	
	seen := make(map[string]bool)
	for i, h := range hashes {
		if len(h) != 32 {
			t.Errorf("Hash %d has wrong length: %d", i, len(h))
		}
		key := string(h)
		if seen[key] {
			t.Errorf("Duplicate hash at index %d", i)
		}
		seen[key] = true
	}

	t.Logf("LocalHashDispatcher produced %d unique hashes", len(hashes))
}

func TestSemanticBounds(t *testing.T) {
	bounds := &quantum.SemanticBounds{
		ValidSynsets:    map[string]bool{"n-12345": true, "v-67890": true},
		ValidCategories: []string{"n", "v"},
		MinCoherence:    0.4,
		MaxTension:      0.7,
		ConceptAnchors:  []string{"test", "example"},
	}

	network := quantum.NewHashNetwork(2, 128, 2, 256)
	network.SetBounds(bounds)

	concepts := []string{"bounded", "test"}
	indices, err := network.Forward(concepts)
	if err != nil {
		t.Fatalf("Forward with bounds failed: %v", err)
	}

	
	for i, idx := range indices {
		validCat := false
		for _, cat := range bounds.ValidCategories {
			if idx.Category == cat {
				validCat = true
				break
			}
		}
		if !validCat {
			t.Errorf("Index %d has invalid category: %s", i, idx.Category)
		}
	}

	t.Logf("Bounds test passed with %d indices", len(indices))
}


func TestHashBridge_Integration(t *testing.T) {
	
	oewnPath := "../oewn.json"
	backend, err := NewNGACBackend(oewnPath)
	if err != nil {
		t.Skipf("Skipping integration test - OEWN not available: %v", err)
		return
	}

	
	err = backend.EnableHashNetwork()
	if err != nil {
		t.Fatalf("EnableHashNetwork failed: %v", err)
	}

	
	response, err := backend.HashGenerate("What is the meaning of knowledge?", 10)
	if err != nil {
		t.Fatalf("HashGenerate failed: %v", err)
	}

	if response == "" {
		t.Error("HashGenerate returned empty response")
	}

	t.Logf("HashGenerate response: %q", response)

	
	concepts := []string{"knowledge", "understanding", "wisdom"}
	words, err := backend.HashForward(concepts)
	if err != nil {
		t.Fatalf("HashForward failed: %v", err)
	}

	t.Logf("HashForward: %v -> %v", concepts, words)

	
	if len(words) == 0 {
		t.Error("HashForward returned no words")
	}
}

func TestHashBridge_BoundsFromConcepts(t *testing.T) {
	oewnPath := "../oewn.json"
	backend, err := NewNGACBackend(oewnPath)
	if err != nil {
		t.Skipf("Skipping - OEWN not available: %v", err)
		return
	}

	bridge := backend.GetHashBridge()
	if bridge == nil {
		t.Fatal("HashBridge is nil")
	}

	
	concepts := []string{"computer", "software", "program"}
	bounds := bridge.BuildBoundsFromConcepts(concepts)

	if bounds == nil {
		t.Fatal("BuildBoundsFromConcepts returned nil")
	}

	t.Logf("Built bounds with %d valid synsets from %v", len(bounds.ValidSynsets), concepts)

	if len(bounds.ValidSynsets) == 0 {
		t.Error("No valid synsets found for concepts")
	}
}

func TestHashBridge_PressureInfluence(t *testing.T) {
	oewnPath := "../oewn.json"
	backend, err := NewNGACBackend(oewnPath)
	if err != nil {
		t.Skipf("Skipping - OEWN not available: %v", err)
		return
	}

	err = backend.EnableHashNetwork()
	if err != nil {
		t.Fatalf("EnableHashNetwork failed: %v", err)
	}

	
	backend.AddPressure("block", 0.9)

	response1, err := backend.HashGenerate("Hello there", 5)
	if err != nil {
		t.Fatalf("HashGenerate with high pressure failed: %v", err)
	}

	
	backend.ResetHashContext()
	backend.AddPressure("block", 0.1)

	response2, err := backend.HashGenerate("Hello there", 5)
	if err != nil {
		t.Fatalf("HashGenerate with low pressure failed: %v", err)
	}

	t.Logf("High pressure response: %q", response1)
	t.Logf("Low pressure response: %q", response2)

	
}
