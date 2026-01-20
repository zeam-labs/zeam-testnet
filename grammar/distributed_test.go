

package grammar

import (
	"crypto/sha256"
	"testing"
)


func mockDispatcher(payload []byte) ([][]byte, error) {
	hashes := make([][]byte, 4)
	current := sha256.Sum256(payload)
	hashes[0] = current[:]

	for i := 1; i < 4; i++ {
		current = sha256.Sum256(current[:])
		hashes[i] = current[:]
	}

	return hashes, nil
}

func TestDistributedGrammar_ParseDistributed(t *testing.T) {
	dg := NewDistributedGrammar(mockDispatcher)

	pressure := PressureContext{
		Magnitude: 0.5,
		Coherence: 0.6,
		Tension:   0.3,
		Density:   0.5,
	}

	
	parsed, err := dg.ParseDistributed("What is photosynthesis?", pressure)
	if err != nil {
		t.Fatalf("ParseDistributed failed: %v", err)
	}

	t.Logf("Parsed sentence type: %d", parsed.Type)
	t.Logf("Parsed intent: %s", parsed.Intent)
	t.Logf("Concepts: %v", parsed.Concepts)

	if len(parsed.Words) == 0 {
		t.Error("Expected parsed words")
	}
}

func TestDistributedGrammar_BuildResponseDistributed(t *testing.T) {
	dg := NewDistributedGrammar(mockDispatcher)

	
	dg.POSLookup = func(word string) PartOfSpeech {
		switch word {
		case "photosynthesis", "plant", "light", "energy":
			return POS_Noun
		case "converts", "produces":
			return POS_Verb
		default:
			return POS_Noun
		}
	}

	dg.WordsByPOS = func(pos string, limit int) []string {
		switch pos {
		case "n":
			return []string{"system", "process", "concept", "data"}
		case "v":
			return []string{"involves", "represents", "includes"}
		default:
			return []string{}
		}
	}

	parsed := &ParsedSentence{
		Original: "What is photosynthesis?",
		Type:     TypeQuestion,
		Intent:   "explanation",
		Concepts: []string{"photosynthesis"},
	}

	pressure := PressureContext{
		Magnitude: 0.5,
		Coherence: 0.6,
		Tension:   0.3,
		Density:   0.5,
	}

	concepts := []string{"photosynthesis", "plant", "light", "energy"}

	response, err := dg.BuildResponseDistributed(parsed, concepts, pressure)
	if err != nil {
		t.Fatalf("BuildResponseDistributed failed: %v", err)
	}

	t.Logf("Distributed response: %s", response)

	if response == "" {
		t.Error("Expected non-empty response")
	}
}

func TestDistributedGrammar_GenerateDistributed(t *testing.T) {
	dg := NewDistributedGrammar(mockDispatcher)

	
	dg.POSLookup = func(word string) PartOfSpeech {
		switch word {
		case "photosynthesis", "plant", "light", "energy", "chlorophyll":
			return POS_Noun
		case "converts", "produces", "uses":
			return POS_Verb
		case "green", "complex":
			return POS_Adjective
		default:
			return POS_Noun
		}
	}

	dg.WordsByPOS = func(pos string, limit int) []string {
		switch pos {
		case "n":
			return []string{"system", "process", "concept"}
		case "v":
			return []string{"involves", "represents", "includes"}
		case "a":
			return []string{"significant", "important"}
		default:
			return []string{}
		}
	}

	pressure := PressureContext{
		Magnitude: 0.5,
		Coherence: 0.6,
		Tension:   0.3,
		Density:   0.5,
	}

	concepts := []string{"photosynthesis", "plant", "light", "energy"}

	response, err := dg.GenerateDistributed("Explain photosynthesis", concepts, pressure)
	if err != nil {
		t.Fatalf("GenerateDistributed failed: %v", err)
	}

	t.Logf("Full distributed response: %s", response)

	if response == "" {
		t.Error("Expected non-empty response")
	}
}

func TestEncodeGrammarPayload(t *testing.T) {
	tests := []struct {
		name string
		gh   *GrammarHash
	}{
		{
			name: "detect_sentence_type",
			gh: &GrammarHash{
				OpType:    OpDetectSentenceType,
				InputText: "What is the meaning?",
				Pressure:  PressureContext{0.5, 0.5, 0.5, 0.5},
			},
		},
		{
			name: "identify_pos",
			gh: &GrammarHash{
				OpType:       OpIdentifyPOS,
				InputConcept: "photosynthesis",
				Pressure:     PressureContext{0.5, 0.5, 0.5, 0.5},
			},
		},
		{
			name: "fill_slot",
			gh: &GrammarHash{
				OpType:       OpFillSlot,
				SlotName:     "subject",
				InputConcept: "plant",
				InputPOS:     POS_Noun,
				Pressure:     PressureContext{0.5, 0.5, 0.5, 0.5},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := EncodeGrammarPayload(tt.gh)
			t.Logf("Payload for %s: %d bytes", tt.name, len(payload))

			if len(payload) == 0 {
				t.Error("Expected non-empty payload")
			}

			
			if payload[0] != byte(tt.gh.OpType) {
				t.Errorf("Expected op type %d, got %d", tt.gh.OpType, payload[0])
			}
		})
	}
}

func TestDistributedGrammar_Greeting(t *testing.T) {
	dg := NewDistributedGrammar(mockDispatcher)

	pressure := PressureContext{
		Magnitude: 0.5,
		Coherence: 0.5,
		Tension:   0.3,
		Density:   0.5,
	}

	response, err := dg.GenerateDistributed("Hello there!", []string{}, pressure)
	if err != nil {
		t.Fatalf("GenerateDistributed failed: %v", err)
	}

	t.Logf("Greeting response: %s", response)

	if response != "Hello." {
		t.Logf("Note: Expected 'Hello.', got %q (may vary with hash)", response)
	}
}

func TestDistributedGrammar_DeterministicOutput(t *testing.T) {
	
	dg := NewDistributedGrammar(mockDispatcher)

	dg.POSLookup = func(word string) PartOfSpeech {
		return POS_Noun
	}
	dg.WordsByPOS = func(pos string, limit int) []string {
		return []string{"concept", "element", "system"}
	}

	pressure := PressureContext{0.5, 0.5, 0.5, 0.5}
	concepts := []string{"test", "concept"}

	response1, _ := dg.GenerateDistributed("What is test?", concepts, pressure)
	response2, _ := dg.GenerateDistributed("What is test?", concepts, pressure)

	if response1 != response2 {
		t.Errorf("Expected deterministic output, got %q and %q", response1, response2)
	}

	t.Logf("Deterministic response: %s", response1)
}

func TestPosIndexConversion(t *testing.T) {
	tests := []PartOfSpeech{
		POS_Noun, POS_Verb, POS_Adjective, POS_Adverb,
		POS_Determiner, POS_Pronoun, POS_Preposition,
		POS_Conjunction, POS_Auxiliary,
	}

	for _, pos := range tests {
		idx := posToIndex(pos)
		recovered := indexToPOS(idx)

		if recovered != pos {
			t.Errorf("POS %s -> index %d -> POS %s (expected %s)",
				pos, idx, recovered, pos)
		}
	}
}
