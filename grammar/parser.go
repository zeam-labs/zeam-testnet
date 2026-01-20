

package grammar

import (
	"regexp"
	"strings"
)


type ParsedWord struct {
	Word     string       
	Lemma    string       
	POS      PartOfSpeech 
	Role     string       
	Position int          
}


type ParsedSentence struct {
	Original    string        
	Words       []*ParsedWord 
	Type        SentenceType  
	Intent      string        
	Subject     *ParsedWord   
	Verb        *ParsedWord   
	Object      *ParsedWord   
	Concepts    []string      
	IsComplete  bool          
}


type SentenceType int

const (
	TypeStatement SentenceType = iota
	TypeQuestion
	TypeCommand
	TypeGreeting
	TypeExclamation
)


type SentenceParser struct {
	
	POSLookup func(word string) PartOfSpeech

	
	SynsetLookup func(word string) []string 
}


var (
	questionWords = map[string]bool{
		"what": true, "who": true, "where": true, "when": true,
		"why": true, "how": true, "which": true, "whose": true,
	}

	auxiliaryVerbs = map[string]bool{
		"is": true, "are": true, "was": true, "were": true,
		"do": true, "does": true, "did": true,
		"have": true, "has": true, "had": true,
		"will": true, "would": true, "shall": true, "should": true,
		"can": true, "could": true, "may": true, "might": true,
		"must": true, "be": true, "been": true, "being": true,
	}

	determiners = map[string]bool{
		"the": true, "a": true, "an": true,
		"this": true, "that": true, "these": true, "those": true,
		"my": true, "your": true, "his": true, "her": true, "its": true,
		"our": true, "their": true,
		"some": true, "any": true, "no": true, "every": true,
	}

	pronouns = map[string]bool{
		"i": true, "you": true, "he": true, "she": true, "it": true,
		"we": true, "they": true, "me": true, "him": true, "her": true,
		"us": true, "them": true, "myself": true, "yourself": true,
		"who": true, "what": true, "which": true, "that": true,
	}

	prepositions = map[string]bool{
		"in": true, "on": true, "at": true, "to": true, "for": true,
		"with": true, "by": true, "from": true, "of": true, "about": true,
		"into": true, "through": true, "during": true, "before": true,
		"after": true, "above": true, "below": true, "between": true,
		"under": true, "over": true,
	}

	conjunctions = map[string]bool{
		"and": true, "or": true, "but": true, "nor": true,
		"for": true, "yet": true, "so": true,
		"because": true, "although": true, "while": true,
		"if": true, "unless": true, "until": true,
	}

	greetingWords = map[string]bool{
		"hello": true, "hi": true, "hey": true, "greetings": true,
		"welcome": true, "goodbye": true, "bye": true, "farewell": true,
		"thanks": true, "thank": true,
	}

	commandVerbs = map[string]bool{
		"tell": true, "show": true, "explain": true, "describe": true,
		"give": true, "find": true, "get": true, "make": true,
		"help": true, "let": true, "please": true,
	}

	
	stopWords = map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"into": true, "through": true, "during": true, "before": true,
		"after": true, "above": true, "below": true, "between": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"it": true, "its": true, "this": true, "that": true, "these": true,
		"i": true, "me": true, "my": true, "you": true, "your": true,
		"we": true, "our": true, "they": true, "their": true,
		"what": true, "which": true, "who": true, "how": true, "when": true,
		"where": true, "why": true,
	}
)


func NewSentenceParser(posLookup func(string) PartOfSpeech, synsetLookup func(string) []string) *SentenceParser {
	return &SentenceParser{
		POSLookup:    posLookup,
		SynsetLookup: synsetLookup,
	}
}


func (sp *SentenceParser) Parse(text string) *ParsedSentence {
	result := &ParsedSentence{
		Original: text,
		Words:    make([]*ParsedWord, 0),
		Concepts: make([]string, 0),
	}

	
	text = strings.TrimSpace(text)
	if text == "" {
		return result
	}

	
	result.Type = sp.detectSentenceType(text)

	
	tokens := sp.tokenize(text)

	
	for i, token := range tokens {
		word := sp.analyzeWord(token, i, result.Type)
		result.Words = append(result.Words, word)

		
		if sp.isContentWord(word) {
			result.Concepts = append(result.Concepts, strings.ToLower(word.Lemma))
		}
	}

	
	sp.identifyRoles(result)

	
	result.Intent = sp.detectIntent(result)

	
	result.IsComplete = result.Subject != nil && result.Verb != nil

	return result
}


func (sp *SentenceParser) tokenize(text string) []string {
	
	text = strings.TrimRight(text, ".!?")

	
	rawTokens := strings.Fields(text)

	tokens := make([]string, 0, len(rawTokens))
	for _, t := range rawTokens {
		
		t = strings.Trim(t, ",;:\"'")
		if t != "" {
			tokens = append(tokens, t)
		}
	}

	return tokens
}


func (sp *SentenceParser) detectSentenceType(text string) SentenceType {
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)

	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return TypeStatement
	}

	firstWord := strings.Trim(fields[0], ",.!?")

	
	if greetingWords[firstWord] {
		return TypeGreeting
	}

	
	if strings.HasSuffix(text, "?") {
		return TypeQuestion
	}
	if strings.HasSuffix(text, "!") {
		return TypeExclamation
	}

	
	if questionWords[firstWord] {
		return TypeQuestion
	}

	
	if auxiliaryVerbs[firstWord] {
		return TypeQuestion
	}

	
	if commandVerbs[firstWord] {
		return TypeCommand
	}

	return TypeStatement
}


func (sp *SentenceParser) analyzeWord(word string, position int, sentType SentenceType) *ParsedWord {
	lower := strings.ToLower(word)

	pw := &ParsedWord{
		Word:     word,
		Lemma:    lower,
		Position: position,
	}

	
	if determiners[lower] {
		pw.POS = POS_Determiner
		return pw
	}
	if pronouns[lower] {
		pw.POS = POS_Pronoun
		return pw
	}
	if auxiliaryVerbs[lower] {
		pw.POS = POS_Auxiliary
		return pw
	}
	if prepositions[lower] {
		pw.POS = POS_Preposition
		return pw
	}
	if conjunctions[lower] {
		pw.POS = POS_Conjunction
		return pw
	}
	if questionWords[lower] {
		pw.POS = POS_Pronoun 
		return pw
	}

	
	if sp.POSLookup != nil {
		pos := sp.POSLookup(lower)
		if pos != "" {
			pw.POS = pos
			return pw
		}
	}

	
	pw.POS = sp.guessPOS(lower, position, sentType)

	return pw
}


func (sp *SentenceParser) guessPOS(word string, position int, sentType SentenceType) PartOfSpeech {
	
	if strings.HasSuffix(word, "ly") {
		return POS_Adverb
	}
	if strings.HasSuffix(word, "ing") {
		return POS_Verb
	}
	if strings.HasSuffix(word, "ed") {
		return POS_Verb
	}
	if strings.HasSuffix(word, "tion") || strings.HasSuffix(word, "sion") {
		return POS_Noun
	}
	if strings.HasSuffix(word, "ness") || strings.HasSuffix(word, "ment") {
		return POS_Noun
	}
	if strings.HasSuffix(word, "ive") || strings.HasSuffix(word, "ous") {
		return POS_Adjective
	}
	if strings.HasSuffix(word, "able") || strings.HasSuffix(word, "ible") {
		return POS_Adjective
	}
	if strings.HasSuffix(word, "er") || strings.HasSuffix(word, "est") {
		return POS_Adjective
	}

	
	if position == 0 && sentType == TypeCommand {
		return POS_Verb
	}

	
	return POS_Noun
}


func (sp *SentenceParser) identifyRoles(ps *ParsedSentence) {
	if len(ps.Words) == 0 {
		return
	}

	
	switch ps.Type {
	case TypeQuestion:
		sp.identifyQuestionRoles(ps)
	case TypeCommand:
		sp.identifyCommandRoles(ps)
	case TypeGreeting:
		
	default:
		sp.identifyStatementRoles(ps)
	}
}


func (sp *SentenceParser) identifyStatementRoles(ps *ParsedSentence) {
	
	verbIndex := -1
	for i, w := range ps.Words {
		if w.POS == POS_Verb && verbIndex == -1 {
			verbIndex = i
			w.Role = "verb"
			ps.Verb = w
			break
		}
	}

	
	for i, w := range ps.Words {
		if i >= verbIndex && verbIndex >= 0 {
			break
		}
		if w.POS == POS_Noun || w.POS == POS_Pronoun {
			w.Role = "subject"
			ps.Subject = w
			break
		}
	}

	
	if verbIndex >= 0 {
		for i := verbIndex + 1; i < len(ps.Words); i++ {
			w := ps.Words[i]
			if w.POS == POS_Noun || w.POS == POS_Pronoun {
				w.Role = "object"
				ps.Object = w
				break
			}
		}
	}
}


func (sp *SentenceParser) identifyQuestionRoles(ps *ParsedSentence) {
	
	startIndex := 0
	for i, w := range ps.Words {
		if questionWords[strings.ToLower(w.Word)] || auxiliaryVerbs[strings.ToLower(w.Word)] {
			startIndex = i + 1
		} else {
			break
		}
	}

	
	for i := startIndex; i < len(ps.Words); i++ {
		w := ps.Words[i]
		if w.POS == POS_Noun || w.POS == POS_Pronoun {
			w.Role = "subject"
			ps.Subject = w
			break
		}
	}

	
	for _, w := range ps.Words {
		if w.POS == POS_Verb {
			w.Role = "verb"
			ps.Verb = w
			break
		}
	}

	
	foundVerb := false
	for _, w := range ps.Words {
		if w.POS == POS_Verb {
			foundVerb = true
			continue
		}
		if foundVerb && (w.POS == POS_Noun || w.POS == POS_Pronoun) && w.Role == "" {
			w.Role = "object"
			ps.Object = w
			break
		}
	}
}


func (sp *SentenceParser) identifyCommandRoles(ps *ParsedSentence) {
	
	if len(ps.Words) > 0 {
		ps.Words[0].Role = "verb"
		ps.Words[0].POS = POS_Verb
		ps.Verb = ps.Words[0]
	}

	
	for i := 1; i < len(ps.Words); i++ {
		w := ps.Words[i]
		if w.POS == POS_Noun || w.POS == POS_Pronoun {
			w.Role = "object"
			ps.Object = w
			break
		}
	}

	
	implicitSubject := &ParsedWord{
		Word:     "(you)",
		Lemma:    "you",
		POS:      POS_Pronoun,
		Role:     "subject",
		Position: -1,
	}
	ps.Subject = implicitSubject
}


func (sp *SentenceParser) isContentWord(w *ParsedWord) bool {
	lower := strings.ToLower(w.Lemma)
	if stopWords[lower] {
		return false
	}
	if len(lower) < 3 {
		return false
	}
	
	if w.POS == POS_Preposition {
		return false
	}
	
	switch w.POS {
	case POS_Noun, POS_Verb, POS_Adjective, POS_Adverb:
		return true
	}
	return false
}


func (sp *SentenceParser) detectIntent(ps *ParsedSentence) string {
	switch ps.Type {
	case TypeGreeting:
		return "greeting"
	case TypeQuestion:
		
		if len(ps.Words) > 0 {
			firstWord := strings.ToLower(ps.Words[0].Word)
			switch firstWord {
			case "what":
				return "explanation"
			case "how":
				return "explanation"
			case "why":
				return "explanation"
			case "who", "where", "when":
				return "question"
			}
		}
		return "question"
	case TypeCommand:
		return "command"
	case TypeExclamation:
		return "statement"
	default:
		return "statement"
	}
}


func (ps *ParsedSentence) GetContentWords() []*ParsedWord {
	content := make([]*ParsedWord, 0)
	for _, w := range ps.Words {
		lower := strings.ToLower(w.Lemma)
		if !stopWords[lower] && len(lower) >= 3 {
			
			if w.POS == POS_Preposition || w.POS == POS_Determiner || w.POS == POS_Conjunction {
				continue
			}
			content = append(content, w)
		}
	}
	return content
}


func (ps *ParsedSentence) GetWordsByPOS(pos PartOfSpeech) []*ParsedWord {
	words := make([]*ParsedWord, 0)
	for _, w := range ps.Words {
		if w.POS == pos {
			words = append(words, w)
		}
	}
	return words
}


func (ps *ParsedSentence) GetVerbPhrase() string {
	if ps.Verb == nil {
		return ""
	}

	parts := []string{ps.Verb.Word}
	if ps.Object != nil {
		parts = append(parts, ps.Object.Word)
	}

	return strings.Join(parts, " ")
}


func (ps *ParsedSentence) GetNounPhrase() string {
	if ps.Subject == nil {
		return ""
	}

	
	parts := make([]string, 0)
	for _, w := range ps.Words {
		if w.Position < ps.Subject.Position && w.POS == POS_Adjective {
			parts = append(parts, w.Word)
		}
		if w == ps.Subject {
			parts = append(parts, w.Word)
			break
		}
	}

	return strings.Join(parts, " ")
}


func (ps *ParsedSentence) String() string {
	var sb strings.Builder

	sb.WriteString("Sentence: ")
	sb.WriteString(ps.Original)
	sb.WriteString("\n")
	sb.WriteString("Type: ")

	switch ps.Type {
	case TypeStatement:
		sb.WriteString("Statement")
	case TypeQuestion:
		sb.WriteString("Question")
	case TypeCommand:
		sb.WriteString("Command")
	case TypeGreeting:
		sb.WriteString("Greeting")
	case TypeExclamation:
		sb.WriteString("Exclamation")
	}

	sb.WriteString("\nIntent: ")
	sb.WriteString(ps.Intent)
	sb.WriteString("\n")

	if ps.Subject != nil {
		sb.WriteString("Subject: ")
		sb.WriteString(ps.Subject.Word)
		sb.WriteString("\n")
	}
	if ps.Verb != nil {
		sb.WriteString("Verb: ")
		sb.WriteString(ps.Verb.Word)
		sb.WriteString("\n")
	}
	if ps.Object != nil {
		sb.WriteString("Object: ")
		sb.WriteString(ps.Object.Word)
		sb.WriteString("\n")
	}

	sb.WriteString("Concepts: [")
	sb.WriteString(strings.Join(ps.Concepts, ", "))
	sb.WriteString("]\n")

	return sb.String()
}


func (ps *ParsedSentence) ExtractTopicConcepts() []string {
	topics := make([]string, 0)
	seen := make(map[string]bool)

	
	if ps.Subject != nil && !stopWords[strings.ToLower(ps.Subject.Lemma)] {
		topics = append(topics, ps.Subject.Lemma)
		seen[ps.Subject.Lemma] = true
	}

	
	if ps.Object != nil && !stopWords[strings.ToLower(ps.Object.Lemma)] {
		if !seen[ps.Object.Lemma] {
			topics = append(topics, ps.Object.Lemma)
			seen[ps.Object.Lemma] = true
		}
	}

	
	for _, w := range ps.Words {
		if w.POS == POS_Noun && !seen[w.Lemma] && !stopWords[strings.ToLower(w.Lemma)] {
			topics = append(topics, w.Lemma)
			seen[w.Lemma] = true
		}
	}

	return topics
}


var sentencePatterns = []struct {
	pattern *regexp.Regexp
	intent  string
}{
	{regexp.MustCompile(`(?i)^(what|how) (is|are|does|do)`), "explanation"},
	{regexp.MustCompile(`(?i)^(tell|explain|describe|show) me`), "command"},
	{regexp.MustCompile(`(?i)^(can|could|would) you`), "request"},
	{regexp.MustCompile(`(?i)^(hello|hi|hey|greetings)`), "greeting"},
	{regexp.MustCompile(`(?i)^(thanks|thank you)`), "thanks"},
	{regexp.MustCompile(`(?i)^(yes|no|ok|okay)`), "acknowledgment"},
}


func MatchPattern(text string) string {
	for _, sp := range sentencePatterns {
		if sp.pattern.MatchString(text) {
			return sp.intent
		}
	}
	return ""
}
