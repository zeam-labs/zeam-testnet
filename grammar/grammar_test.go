

package grammar

import (
	"math/big"
	"testing"
)

func TestSentencePatterns(t *testing.T) {
	
	sentence := NewSentence(PatternDeclarativeSVO)

	sentence.FillSlot("subject", "system")
	sentence.FillSlot("verb", "processes")
	sentence.FillSlot("object", "data")

	result := sentence.Build()
	t.Logf("SVO result: %s", result)

	expected := "The system processes the data."
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestSentenceWithModifiers(t *testing.T) {
	sentence := NewSentence(PatternDeclarativeSVO)

	sentence.FillSlot("subject", "system", "quantum")
	sentence.FillSlot("verb", "processes", "efficiently")
	sentence.FillSlot("object", "data", "complex")

	result := sentence.Build()
	t.Logf("Modified result: %s", result)

	
	if result == "" {
		t.Error("Expected non-empty sentence")
	}
}

func TestDefinitionPattern(t *testing.T) {
	sentence := NewSentence(PatternDefinition)

	sentence.FillSlot("subject", "photosynthesis")
	sentence.FillSlot("copula", "is")       
	sentence.FillSlot("category", "process")
	sentence.FillSlot("relative", "that")   
	sentence.FillSlot("verb", "converts")
	sentence.FillSlot("object", "light")

	result := sentence.Build()
	t.Logf("Definition result: %s", result)

	if result == "" {
		t.Error("Expected non-empty sentence")
	}
}

func TestSentenceParser_Statement(t *testing.T) {
	parser := NewSentenceParser(nil, nil)

	result := parser.Parse("The cat sits on the mat.")

	if result.Type != TypeStatement {
		t.Errorf("Expected Statement, got %d", result.Type)
	}

	t.Logf("Parsed: %s", result.String())
	t.Logf("Concepts: %v", result.Concepts)

	if len(result.Concepts) == 0 {
		t.Error("Expected some concepts")
	}
}

func TestSentenceParser_Question(t *testing.T) {
	parser := NewSentenceParser(nil, nil)

	result := parser.Parse("What is photosynthesis?")

	if result.Type != TypeQuestion {
		t.Errorf("Expected Question, got %d", result.Type)
	}

	if result.Intent != "explanation" {
		t.Errorf("Expected intent 'explanation', got %q", result.Intent)
	}

	t.Logf("Parsed: %s", result.String())
}

func TestSentenceParser_Command(t *testing.T) {
	parser := NewSentenceParser(nil, nil)

	result := parser.Parse("Tell me about quantum mechanics.")

	if result.Type != TypeCommand {
		t.Errorf("Expected Command, got %d", result.Type)
	}

	t.Logf("Parsed: %s", result.String())

	
	if result.Subject == nil {
		t.Error("Expected implicit subject for command")
	}
}

func TestSentenceParser_Greeting(t *testing.T) {
	parser := NewSentenceParser(nil, nil)

	result := parser.Parse("Hello, how are you?")

	if result.Type != TypeGreeting {
		t.Errorf("Expected Greeting, got %d", result.Type)
	}

	if result.Intent != "greeting" {
		t.Errorf("Expected intent 'greeting', got %q", result.Intent)
	}

	t.Logf("Parsed: %s", result.String())
}

func TestSentenceParser_ContentWords(t *testing.T) {
	parser := NewSentenceParser(nil, nil)

	result := parser.Parse("The quick brown fox jumps over the lazy dog.")

	content := result.GetContentWords()
	t.Logf("Content words: %d", len(content))

	for _, w := range content {
		t.Logf("  %s (%s)", w.Word, w.POS)
	}

	
	for _, w := range content {
		if w.Word == "the" || w.Word == "over" {
			t.Errorf("Stop word %q should not be in content words", w.Word)
		}
	}
}

func TestSentenceParser_TopicExtraction(t *testing.T) {
	parser := NewSentenceParser(nil, nil)

	result := parser.Parse("How do plants convert sunlight into energy?")

	topics := result.ExtractTopicConcepts()
	t.Logf("Topics: %v", topics)

	if len(topics) == 0 {
		t.Error("Expected some topics")
	}
}

func TestSentenceBuilder_Basic(t *testing.T) {
	builder := NewSentenceBuilder()

	
	builder.GetWordsByPOS = func(pos string) []string {
		switch pos {
		case "n":
			return []string{"system", "process", "concept", "data", "information"}
		case "v":
			return []string{"involves", "represents", "includes", "processes", "converts"}
		case "a":
			return []string{"important", "significant", "complex", "simple"}
		default:
			return []string{}
		}
	}

	builder.GetPOS = func(word string) string {
		switch word {
		case "photosynthesis", "plant", "light", "energy":
			return "n"
		case "convert", "converts":
			return "v"
		case "complex":
			return "a"
		default:
			return "n"
		}
	}

	parser := NewSentenceParser(nil, nil)
	parsed := parser.Parse("What is photosynthesis?")

	concepts := []string{"photosynthesis", "plant", "light", "energy"}
	pressure := PressureContext{
		Magnitude: 0.5,
		Coherence: 0.5,
		Tension:   0.3,
		Density:   0.5,
	}

	result := builder.BuildResponse(parsed, concepts, pressure, big.NewInt(42))
	t.Logf("Generated response: %s", result)

	if result == "" {
		t.Error("Expected non-empty response")
	}
}

func TestGetPatternForIntent(t *testing.T) {
	tests := []struct {
		intent       string
		expectedName string
	}{
		{"greeting", "greeting"},
		{"statement", "declarative_svo"},
		{"explanation", "explanation"},
		{"command", "imperative"},
		{"unknown", "declarative_svo"}, 
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			pattern := GetPatternForIntent(tt.intent)
			if pattern.Name != tt.expectedName {
				t.Errorf("For intent %q: expected %q, got %q",
					tt.intent, tt.expectedName, pattern.Name)
			}
		})
	}
}

func TestSentenceCompleteness(t *testing.T) {
	sentence := NewSentence(PatternDeclarativeSVO)

	
	if sentence.IsComplete() {
		t.Error("Empty sentence should not be complete")
	}

	sentence.FillSlot("subject", "system")
	if sentence.IsComplete() {
		t.Error("Sentence without verb should not be complete")
	}

	sentence.FillSlot("verb", "processes")
	if sentence.IsComplete() {
		t.Error("Sentence without object should not be complete")
	}

	sentence.FillSlot("object", "data")
	if !sentence.IsComplete() {
		t.Error("Fully filled sentence should be complete")
	}
}

func TestGetNextSlot(t *testing.T) {
	sentence := NewSentence(PatternDeclarativeSVO)

	slot := sentence.GetNextSlot()
	if slot == nil {
		t.Fatal("Expected next slot")
	}
	if slot.Name != "subject" {
		t.Errorf("Expected 'subject', got %q", slot.Name)
	}

	sentence.FillSlot("subject", "system")

	slot = sentence.GetNextSlot()
	if slot.Name != "verb" {
		t.Errorf("Expected 'verb', got %q", slot.Name)
	}
}

func TestRequiredPOS(t *testing.T) {
	sentence := NewSentence(PatternDeclarativeSVO)

	required := sentence.GetRequiredPOS()
	t.Logf("Required POS: %v", required)

	
	if len(required) != 3 {
		t.Errorf("Expected 3 required POS, got %d", len(required))
	}

	sentence.FillSlot("subject", "system")
	required = sentence.GetRequiredPOS()

	if len(required) != 2 {
		t.Errorf("Expected 2 required POS after filling subject, got %d", len(required))
	}
}

func TestPatternMatching(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"What is the meaning of life?", "explanation"},
		{"Tell me about quantum physics", "command"},
		{"Hello there!", "greeting"},
		{"Yes, I agree", "acknowledgment"},
		{"Random statement here", ""}, 
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := MatchPattern(tt.input)
			if result != tt.expected {
				t.Errorf("For %q: expected %q, got %q", tt.input, tt.expected, result)
			}
		})
	}
}

func TestSentenceBuilderFallback(t *testing.T) {
	
	builder := NewSentenceBuilder()

	parser := NewSentenceParser(nil, nil)
	parsed := parser.Parse("What is something?")

	concepts := []string{"something"} 
	pressure := PressureContext{
		Magnitude: 0.5,
		Coherence: 0.5,
		Tension:   0.3,
		Density:   0.5,
	}

	result := builder.BuildResponse(parsed, concepts, pressure, big.NewInt(42))
	t.Logf("Fallback response: %s", result)

	
	if result == "" {
		t.Error("Expected non-empty response even with fallbacks")
	}
}

func TestGreetingResponse(t *testing.T) {
	response := GetSimpleResponse("greeting", big.NewInt(0))
	t.Logf("Greeting response: %s", response)

	if response == "" {
		t.Error("Expected non-empty greeting")
	}
}

func TestMultiSentence(t *testing.T) {
	builder := NewSentenceBuilder()

	builder.GetWordsByPOS = func(pos string) []string {
		switch pos {
		case "n":
			return []string{"system", "process", "concept", "data", "energy", "light"}
		case "v":
			return []string{"involves", "represents", "converts", "produces"}
		default:
			return []string{}
		}
	}

	parser := NewSentenceParser(nil, nil)
	parsed := parser.Parse("Explain photosynthesis in detail.")

	concepts := []string{"photosynthesis", "plant", "light", "energy", "chlorophyll", "sugar"}
	pressure := PressureContext{
		Magnitude: 0.5,
		Coherence: 0.6,
		Tension:   0.3,
		Density:   0.6,
	}

	result := builder.GenerateMultiSentence(parsed, concepts, pressure, big.NewInt(42), 2)
	t.Logf("Multi-sentence response: %s", result)

	if result == "" {
		t.Error("Expected non-empty multi-sentence response")
	}
}
