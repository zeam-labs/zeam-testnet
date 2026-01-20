

package ngac

import (
	"math/big"
	"math/cmplx"
	"sort"
	"strings"

	"zeam/quantum"
)


type Language struct {
	Name         string
	Alphabet     map[rune]*big.Int         
	sortedRunes  []rune                    
	Grammar      map[string]GrammarRule    
	Automata     *SyntaxAutomata           
	Phonotactics *Phonotactics             
	Morphology   map[string]MorphologyRule 
}


type GrammarRule struct {
	Name      string
	Transform func(*quantum.SubstrateChain, ...*big.Int) *big.Int
	Arity     int
}


type SyntaxAutomata struct {
	States       map[string]*State
	CurrentState string
}


type State struct {
	Name        string
	Transitions map[string]string 
	IsFinal     bool
}


type MorphologyRule func(*quantum.SubstrateChain, *big.Int) *big.Int


type Phonotactics struct {
	ValidOnsets      map[string]bool 
	ValidCodas       map[string]bool 
	Vowels           map[rune]bool   
	Consonants       map[rune]bool   
	ForbiddenBigrams map[string]bool 
}


func NewEnglishLanguage(sc *quantum.SubstrateChain) *Language {
	lang := &Language{
		Name:       "English",
		Alphabet:   make(map[rune]*big.Int),
		Grammar:    make(map[string]GrammarRule),
		Morphology: make(map[string]MorphologyRule),
	}

	lang.buildAlphabet(sc)
	lang.indexAlphabet()  
	lang.buildGrammar(sc) 
	lang.Automata = lang.buildAutomata()
	lang.Phonotactics = NewEnglishPhonotactics()
	lang.buildMorphology(sc) 

	return lang
}


func (l *Language) buildAlphabet(sc *quantum.SubstrateChain) {
	
	for ch := 'a'; ch <= 'z'; ch++ {
		l.Alphabet[ch] = quantum.UTF8_ENCODE(sc, string(ch))
	}
	
	for ch := 'A'; ch <= 'Z'; ch++ {
		lower := ch + 32
		l.Alphabet[ch] = l.Alphabet[rune(lower)]
	}
	
	for ch := '0'; ch <= '9'; ch++ {
		l.Alphabet[ch] = quantum.UTF8_ENCODE(sc, string(ch))
	}
	
	syms := []rune{' ', '.', '?', '!', ',', '-', '\'', ';', ':'}
	for _, ch := range syms {
		l.Alphabet[ch] = quantum.UTF8_ENCODE(sc, string(ch))
	}
}


func (l *Language) indexAlphabet() {
	l.sortedRunes = make([]rune, 0, len(l.Alphabet))
	for r := range l.Alphabet {
		l.sortedRunes = append(l.sortedRunes, r)
	}
	sort.Slice(l.sortedRunes, func(i, j int) bool { return l.sortedRunes[i] < l.sortedRunes[j] })
}


func (l *Language) buildGrammar(sc *quantum.SubstrateChain) {
	
	l.Grammar["noun_phrase"] = GrammarRule{
		Name:  "noun_phrase",
		Arity: 2,
		Transform: func(sc *quantum.SubstrateChain, inputs ...*big.Int) *big.Int {
			if len(inputs) != 2 {
				return big.NewInt(0)
			}
			det := inputs[0]
			noun := inputs[1]
			res := new(big.Int).Lsh(new(big.Int).Set(det), 64)
			res.Add(res, noun)
			return res
		},
	}

	
	l.Grammar["verb_phrase"] = GrammarRule{
		Name:  "verb_phrase",
		Arity: 2,
		Transform: func(sc *quantum.SubstrateChain, inputs ...*big.Int) *big.Int {
			if len(inputs) != 2 {
				return big.NewInt(0)
			}
			verb := inputs[0]
			np := inputs[1]
			res := new(big.Int).Lsh(new(big.Int).Set(verb), 128)
			res.Add(res, np)
			return res
		},
	}

	
	l.Grammar["sentence"] = GrammarRule{
		Name:  "sentence",
		Arity: 2,
		Transform: func(sc *quantum.SubstrateChain, inputs ...*big.Int) *big.Int {
			if len(inputs) != 2 {
				return big.NewInt(0)
			}
			np := inputs[0]
			vp := inputs[1]
			res := new(big.Int).Lsh(new(big.Int).Set(np), 256)
			res.Add(res, vp)
			return res
		},
	}
}


func (l *Language) buildAutomata() *SyntaxAutomata {
	automata := &SyntaxAutomata{
		States:       make(map[string]*State),
		CurrentState: "START",
	}

	start := &State{
		Name:        "START",
		Transitions: map[string]string{"WORD": "AFTER_WORD"},
		IsFinal:     false,
	}
	afterWord := &State{
		Name:        "AFTER_WORD",
		Transitions: map[string]string{"WORD": "AFTER_WORD", "END": "END"},
		IsFinal:     false,
	}
	end := &State{
		Name:        "END",
		Transitions: map[string]string{},
		IsFinal:     true,
	}

	automata.States["START"] = start
	automata.States["AFTER_WORD"] = afterWord
	automata.States["END"] = end

	return automata
}


func NewEnglishPhonotactics() *Phonotactics {
	p := &Phonotactics{
		ValidOnsets:      make(map[string]bool),
		ValidCodas:       make(map[string]bool),
		Vowels:           make(map[rune]bool),
		Consonants:       make(map[rune]bool),
		ForbiddenBigrams: make(map[string]bool),
	}

	
	for _, v := range []rune{'a', 'e', 'i', 'o', 'u'} {
		p.Vowels[v] = true
	}
	
	for _, c := range []rune{
		'b', 'c', 'd', 'f', 'g', 'h', 'j', 'k', 'l', 'm',
		'n', 'p', 'q', 'r', 's', 't', 'v', 'w', 'x', 'y', 'z',
	} {
		p.Consonants[c] = true
	}

	
	onsets := []string{
		"",
		"b", "c", "d", "f", "g", "h", "j", "k", "l", "m", "n", "p", "r", "s", "t", "v", "w", "y", "z",
		"bl", "br", "cl", "cr", "dr", "fl", "fr", "gl", "gr", "pl", "pr",
		"sk", "sl", "sm", "sn", "sp", "st", "sw", "tr", "tw",
		"scr", "spl", "spr", "str", "squ",
	}
	for _, o := range onsets {
		p.ValidOnsets[o] = true
	}

	
	codas := []string{
		"",
		"b", "d", "f", "g", "k", "l", "m", "n", "p", "r", "s", "t", "v", "x", "z",
		"ct", "ft", "ld", "lf", "lk", "lm", "lp", "lt", "mp", "nd", "ng", "nk", "nt", "pt", "sk", "sp", "st",
		"nct", "mpt", "nks", "nts", "sts",
	}
	for _, c := range codas {
		p.ValidCodas[c] = true
	}

	
	for _, seq := range []string{"tl", "dl", "sr", "vl", "bz", "dg", "gk"} {
		p.ForbiddenBigrams[seq] = true
	}

	return p
}


func (l *Language) buildMorphology(sc *quantum.SubstrateChain) {
	
	l.Morphology["pluralize"] = func(sc *quantum.SubstrateChain, coord *big.Int) *big.Int {
		sCoord := l.Alphabet['s']
		res := new(big.Int).Lsh(new(big.Int).Set(coord), 8)
		res.Add(res, sCoord)
		return res
	}
	
	l.Morphology["past_tense"] = func(sc *quantum.SubstrateChain, coord *big.Int) *big.Int {
		ed := quantum.UTF8_ENCODE(sc, "ed")
		res := new(big.Int).Lsh(new(big.Int).Set(coord), 16)
		res.Add(res, ed)
		return res
	}
}


func (p *Phonotactics) IsValidSequence(letters []rune) bool {
	if len(letters) == 0 {
		return false
	}

	
	hasV := false
	for _, ch := range letters {
		if p.Vowels[ch] {
			hasV = true
			break
		}
	}
	if !hasV {
		return false
	}

	
	onset := p.extractOnset(letters)
	if !p.ValidOnsets[onset] {
		return false
	}
	coda := p.extractCoda(letters)
	if !p.ValidCodas[coda] {
		return false
	}

	
	for i := 0; i < len(letters)-1; i++ {
		bg := string([]rune{letters[i], letters[i+1]})
		if p.ForbiddenBigrams[bg] {
			return false
		}
	}
	return true
}


func (p *Phonotactics) extractOnset(letters []rune) string {
	onset := strings.Builder{}
	for _, ch := range letters {
		if p.Consonants[ch] {
			onset.WriteRune(ch)
		} else {
			break
		}
	}
	return onset.String()
}


func (p *Phonotactics) extractCoda(letters []rune) string {
	coda := ""
	for i := len(letters) - 1; i >= 0; i-- {
		if p.Consonants[letters[i]] {
			coda = string(letters[i]) + coda
		} else {
			break
		}
	}
	return coda
}


func (l *Language) pickRune(seed *big.Int, step int, retry int) rune {
	idxBig := new(big.Int).Add(seed, big.NewInt(int64(step*17+retry))) 
	hashed := quantum.FeistelHash(idxBig)
	i := new(big.Int).Mod(hashed, big.NewInt(int64(len(l.sortedRunes)))).Int64()
	return l.sortedRunes[i]
}


func (p *Phonotactics) GenerateValidWord(sc *quantum.SubstrateChain, lang *Language, seed *big.Int, maxLen int) string {
	if maxLen < 3 {
		maxLen = 3
	}
	if maxLen > 12 {
		maxLen = 12
	}

	
	skeleton := make([]byte, 0, maxLen)
	parity := new(big.Int).And(seed, big.NewInt(1)).Cmp(big.NewInt(0)) == 0
	for i := 0; i < maxLen; i++ {
		if parity {
			if i%2 == 0 {
				skeleton = append(skeleton, 'C')
			} else {
				skeleton = append(skeleton, 'V')
			}
		} else {
			if i%3 == 2 {
				skeleton = append(skeleton, 'C')
			} else {
				skeleton = append(skeleton, 'V')
			}
		}
	}

	letters := make([]rune, 0, maxLen)
	curSeed := new(big.Int).Set(seed)

	for step := 0; step < maxLen; step++ {
		want := skeleton[step]
		chosen := rune(0)
		
		for retry := 0; retry < 6; retry++ {
			r := lang.pickRune(curSeed, step, retry)

			
			if want == 'V' && !p.Vowels[r] {
				continue
			}
			if want == 'C' && !p.Consonants[r] {
				continue
			}

			test := append(append([]rune{}, letters...), r)

			
			if !p.IsValidSequence(test) {
				continue
			}

			chosen = r
			break
		}
		if chosen == 0 {
			
			if len(letters) >= 3 {
				break
			}
			
			curSeed = quantum.FeistelHash(curSeed)
			continue
		}
		letters = append(letters, chosen)
	}

	
	if len(letters) < 3 || !p.IsValidSequence(letters) {
		return ""
	}
	return string(letters)
}


func (a *SyntaxAutomata) Transition(symbol string) bool {
	st := a.States[a.CurrentState]
	if st == nil {
		return false
	}
	if next, ok := st.Transitions[symbol]; ok {
		a.CurrentState = next
		return true
	}
	return false
}


func (a *SyntaxAutomata) IsValid() bool {
	return a.CurrentState != "" && a.States[a.CurrentState] != nil
}


func (a *SyntaxAutomata) IsFinal() bool {
	if st, ok := a.States[a.CurrentState]; ok {
		return st.IsFinal
	}
	return false
}


func (a *SyntaxAutomata) Reset() {
	a.CurrentState = "START"
}


func (a *SyntaxAutomata) GetValidTypes() []string {
	st := a.States[a.CurrentState]
	if st == nil {
		return nil
	}
	out := make([]string, 0, len(st.Transitions))
	for sym := range st.Transitions {
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}


func Amp(sc *quantum.SubstrateChain, coord *big.Int) float64 {
	if sc == nil || coord == nil {
		return 0
	}
	a := sc.GetAmplitude(coord)
	return cmplx.Abs(a)
}


var UTTERANCE_PHONOTACTIC quantum.QuantumCircuit = func(sc *quantum.SubstrateChain, inputs ...*big.Int) *big.Int {
	if len(inputs) == 0 {
		return quantum.UTF8_ENCODE(sc, "")
	}
	seed := new(big.Int).Set(inputs[0])

	lang := NewEnglishLanguage(sc)
	lang.Automata.Reset()

	words := make([]string, 0, 6)
	cur := new(big.Int).Set(seed)
	maxWords := 5

	for len(words) < maxWords {
		w := lang.Phonotactics.GenerateValidWord(sc, lang, cur, 6)
		if w == "" {
			
			cur = quantum.FeistelHash(cur)
			continue
		}
		if lang.Automata.Transition("WORD") {
			words = append(words, w)
		}
		
		cur = quantum.FeistelHash(cur)

		
		if new(big.Int).Mod(cur, big.NewInt(3)).Int64() == 0 && len(words) >= 2 {
			if lang.Automata.Transition("END") && lang.Automata.IsFinal() {
				break
			}
		}
	}

	text := strings.Join(words, " ")
	return quantum.UTF8_ENCODE(sc, text)
}
