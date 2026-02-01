package grammar

import "math"

var SemanticCategory = map[byte]string{}

func init() {
	for i := 0; i < 256; i++ {
		b := byte(i)
		switch {
		case b < 0x20:
			SemanticCategory[b] = "noun.object"
		case b < 0x40:
			SemanticCategory[b] = "verb.action"
		case b < 0x60:
			SemanticCategory[b] = "adj.property"
		case b < 0x80:
			SemanticCategory[b] = "noun.relation"
		case b < 0xA0:
			SemanticCategory[b] = "noun.quantity"
		case b < 0xC0:
			SemanticCategory[b] = "noun.state"
		case b < 0xE0:
			SemanticCategory[b] = "noun.event"
		default:
			SemanticCategory[b] = "noun.abstract"
		}
	}
}

func HashToSemantics(hashBytes []byte, count int) []string {
	if count > len(hashBytes) {
		count = len(hashBytes)
	}
	semantics := make([]string, count)
	for i := 0; i < count; i++ {
		semantics[i] = SemanticCategory[hashBytes[i]]
	}
	return semantics
}

func HashEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}

	freq := make(map[byte]int)
	for _, b := range data {
		freq[b]++
	}

	var entropy float64
	n := float64(len(data))
	for _, count := range freq {
		if count > 0 {
			p := float64(count) / n
			entropy -= p * math.Log2(p)
		}
	}

	return entropy / 8.0
}
