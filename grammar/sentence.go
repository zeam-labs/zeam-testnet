package grammar

import (
	"strings"
)

type PartOfSpeech string

const (
	POS_Noun       PartOfSpeech = "n"
	POS_Verb       PartOfSpeech = "v"
	POS_Adjective  PartOfSpeech = "a"
	POS_Adverb     PartOfSpeech = "r"
	POS_Determiner PartOfSpeech = "det"
	POS_Pronoun    PartOfSpeech = "pron"
	POS_Preposition PartOfSpeech = "prep"
	POS_Conjunction PartOfSpeech = "conj"
	POS_Auxiliary  PartOfSpeech = "aux"
	POS_Any        PartOfSpeech = "*"
)

type GrammarSlot struct {
	Name        string
	POS         PartOfSpeech
	Required    bool
	Modifiable  bool
	Article     string
	FixedWord   string
}

type SentencePattern struct {
	Name        string
	Description string
	Slots       []GrammarSlot
	Punctuation string
	IntentType  string
}

type FilledSlot struct {
	Slot      GrammarSlot
	MainWord  string
	Modifiers []string
}

type Sentence struct {
	Pattern     *SentencePattern
	FilledSlots []FilledSlot
}

var (

	PatternDeclarativeSVO = &SentencePattern{
		Name:        "declarative_svo",
		Description: "Subject-Verb-Object statement",
		IntentType:  "statement",
		Punctuation: ".",
		Slots: []GrammarSlot{
			{Name: "subject", POS: POS_Noun, Required: true, Modifiable: true, Article: "the"},
			{Name: "verb", POS: POS_Verb, Required: true, Modifiable: true},
			{Name: "object", POS: POS_Noun, Required: true, Modifiable: true, Article: "the"},
		},
	}

	PatternDeclarativeSV = &SentencePattern{
		Name:        "declarative_sv",
		Description: "Subject-Verb intransitive statement",
		IntentType:  "statement",
		Punctuation: ".",
		Slots: []GrammarSlot{
			{Name: "subject", POS: POS_Noun, Required: true, Modifiable: true, Article: "the"},
			{Name: "verb", POS: POS_Verb, Required: true, Modifiable: true},
		},
	}

	PatternDeclarativeAdj = &SentencePattern{
		Name:        "declarative_adj",
		Description: "Subject is adjective",
		IntentType:  "description",
		Punctuation: ".",
		Slots: []GrammarSlot{
			{Name: "subject", POS: POS_Noun, Required: true, Modifiable: true, Article: "the"},
			{Name: "copula", POS: POS_Auxiliary, Required: true, FixedWord: "is"},
			{Name: "complement", POS: POS_Adjective, Required: true, Modifiable: true},
		},
	}

	PatternGreeting = &SentencePattern{
		Name:        "greeting",
		Description: "Greeting response",
		IntentType:  "greeting",
		Punctuation: ".",
		Slots: []GrammarSlot{
			{Name: "greeting", POS: POS_Any, Required: true, FixedWord: "Hello"},
		},
	}

	PatternQuestionWH = &SentencePattern{
		Name:        "question_wh",
		Description: "WH-question (what, how, why)",
		IntentType:  "question",
		Punctuation: "?",
		Slots: []GrammarSlot{
			{Name: "wh_word", POS: POS_Any, Required: true},
			{Name: "auxiliary", POS: POS_Auxiliary, Required: true},
			{Name: "subject", POS: POS_Noun, Required: true, Article: "the"},
			{Name: "verb", POS: POS_Verb, Required: true},
		},
	}

	PatternQuestionYesNo = &SentencePattern{
		Name:        "question_yesno",
		Description: "Yes/No question",
		IntentType:  "question",
		Punctuation: "?",
		Slots: []GrammarSlot{
			{Name: "auxiliary", POS: POS_Auxiliary, Required: true, FixedWord: "Does"},
			{Name: "subject", POS: POS_Noun, Required: true, Article: "the"},
			{Name: "verb", POS: POS_Verb, Required: true},
			{Name: "object", POS: POS_Noun, Required: false, Article: "the"},
		},
	}

	PatternExplanation = &SentencePattern{
		Name:        "explanation",
		Description: "Explanatory statement",
		IntentType:  "explanation",
		Punctuation: ".",
		Slots: []GrammarSlot{
			{Name: "subject", POS: POS_Noun, Required: true, Modifiable: true, Article: "the"},
			{Name: "verb", POS: POS_Verb, Required: true},
			{Name: "object", POS: POS_Noun, Required: true, Modifiable: true, Article: "the"},
			{Name: "conjunction", POS: POS_Conjunction, Required: false, FixedWord: "and"},
			{Name: "object2", POS: POS_Noun, Required: false, Modifiable: true, Article: "the"},
		},
	}

	PatternImperative = &SentencePattern{
		Name:        "imperative",
		Description: "Command/imperative",
		IntentType:  "command",
		Punctuation: ".",
		Slots: []GrammarSlot{
			{Name: "verb", POS: POS_Verb, Required: true},
			{Name: "object", POS: POS_Noun, Required: true, Modifiable: true, Article: "the"},
		},
	}

	PatternDefinition = &SentencePattern{
		Name:        "definition",
		Description: "Definition pattern",
		IntentType:  "definition",
		Punctuation: ".",
		Slots: []GrammarSlot{
			{Name: "subject", POS: POS_Noun, Required: true, Modifiable: true, Article: "a"},
			{Name: "copula", POS: POS_Auxiliary, Required: true, FixedWord: "is"},
			{Name: "category", POS: POS_Noun, Required: true, Modifiable: true, Article: "a"},
			{Name: "relative", POS: POS_Any, Required: false, FixedWord: "that"},
			{Name: "verb", POS: POS_Verb, Required: false},
			{Name: "object", POS: POS_Noun, Required: false, Article: "the"},
		},
	}
)

var AllPatterns = []*SentencePattern{
	PatternDeclarativeSVO,
	PatternDeclarativeSV,
	PatternDeclarativeAdj,
	PatternGreeting,
	PatternQuestionWH,
	PatternQuestionYesNo,
	PatternExplanation,
	PatternImperative,
	PatternDefinition,
}

func GetPatternForIntent(intent string) *SentencePattern {

	for _, p := range AllPatterns {
		if p.IntentType == intent {
			return p
		}
	}

	return PatternDeclarativeSVO
}

func GetPatternsForIntent(intent string) []*SentencePattern {
	patterns := make([]*SentencePattern, 0)
	for _, p := range AllPatterns {
		if p.IntentType == intent {
			patterns = append(patterns, p)
		}
	}
	if len(patterns) == 0 {
		return []*SentencePattern{PatternDeclarativeSVO}
	}
	return patterns
}

func (s *Sentence) Build() string {
	if s.Pattern == nil || len(s.FilledSlots) == 0 {
		return ""
	}

	words := make([]string, 0)

	for _, fs := range s.FilledSlots {

		if fs.Slot.Article != "" && fs.Slot.POS == POS_Noun {
			words = append(words, fs.Slot.Article)
		}

		if fs.Slot.POS == POS_Noun && len(fs.Modifiers) > 0 {
			words = append(words, fs.Modifiers...)
		}

		if fs.MainWord != "" {
			words = append(words, fs.MainWord)
		}

		if fs.Slot.POS == POS_Verb && len(fs.Modifiers) > 0 {
			words = append(words, fs.Modifiers...)
		}
	}

	if len(words) == 0 {
		return ""
	}

	sentence := strings.Join(words, " ")

	if len(sentence) > 0 {
		sentence = strings.ToUpper(string(sentence[0])) + sentence[1:]
	}

	if !strings.HasSuffix(sentence, s.Pattern.Punctuation) {
		sentence += s.Pattern.Punctuation
	}

	return sentence
}

func NewSentence(pattern *SentencePattern) *Sentence {
	return &Sentence{
		Pattern:     pattern,
		FilledSlots: make([]FilledSlot, 0, len(pattern.Slots)),
	}
}

func (s *Sentence) FillSlot(slotName string, word string, modifiers ...string) bool {
	for i, slot := range s.Pattern.Slots {
		if slot.Name == slotName {

			alreadyFilled := false
			for _, fs := range s.FilledSlots {
				if fs.Slot.Name == slotName {
					alreadyFilled = true
					break
				}
			}
			if alreadyFilled {
				return false
			}

			finalWord := word
			if slot.FixedWord != "" {
				finalWord = slot.FixedWord
			}

			insertPos := len(s.FilledSlots)
			for j, fs := range s.FilledSlots {
				for k, ps := range s.Pattern.Slots {
					if ps.Name == fs.Slot.Name && k > i {
						insertPos = j
						break
					}
				}
			}

			filled := FilledSlot{
				Slot:      slot,
				MainWord:  finalWord,
				Modifiers: modifiers,
			}

			if insertPos >= len(s.FilledSlots) {
				s.FilledSlots = append(s.FilledSlots, filled)
			} else {
				s.FilledSlots = append(s.FilledSlots[:insertPos+1], s.FilledSlots[insertPos:]...)
				s.FilledSlots[insertPos] = filled
			}

			return true
		}
	}
	return false
}

func (s *Sentence) GetRequiredPOS() []PartOfSpeech {
	required := make([]PartOfSpeech, 0)
	filledNames := make(map[string]bool)

	for _, fs := range s.FilledSlots {
		filledNames[fs.Slot.Name] = true
	}

	for _, slot := range s.Pattern.Slots {
		if slot.Required && !filledNames[slot.Name] && slot.FixedWord == "" {
			required = append(required, slot.POS)
		}
	}

	return required
}

func (s *Sentence) IsComplete() bool {
	return len(s.GetRequiredPOS()) == 0
}

func (s *Sentence) GetNextSlot() *GrammarSlot {
	filledNames := make(map[string]bool)
	for _, fs := range s.FilledSlots {
		filledNames[fs.Slot.Name] = true
	}

	for i := range s.Pattern.Slots {
		slot := &s.Pattern.Slots[i]
		if !filledNames[slot.Name] {
			return slot
		}
	}
	return nil
}
