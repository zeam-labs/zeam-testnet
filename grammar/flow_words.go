package grammar

import (
	"zeam/node"
)

type FlowWordSelector struct {
	compute *node.FlowCompute
}

func NewFlowWordSelector(compute *node.FlowCompute) *FlowWordSelector {
	return &FlowWordSelector{compute: compute}
}

func (fws *FlowWordSelector) Select(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	idx := fws.compute.SelectIndex(len(candidates))
	return candidates[idx]
}

func (fws *FlowWordSelector) SelectWeighted(candidates []string, weights []float64) string {
	if len(candidates) == 0 {
		return ""
	}
	if len(weights) != len(candidates) {
		return fws.Select(candidates)
	}

	var total float64
	for _, w := range weights {
		total += w
	}
	if total == 0 {
		return fws.Select(candidates)
	}

	target := fws.compute.NextFloat() * total
	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if target <= cumulative {
			return candidates[i]
		}
	}

	return candidates[len(candidates)-1]
}

type FlowGrammar struct {
	selector *FlowWordSelector

	GetWordsByPOS func(pos string) []string
	GetSynonyms   func(word string) []string
	GetDefinition func(word string) string
}

func NewFlowGrammar(compute *node.FlowCompute) *FlowGrammar {
	return &FlowGrammar{
		selector: NewFlowWordSelector(compute),
	}
}

func (fg *FlowGrammar) SelectWord(candidates []string, weights []float64) string {
	if len(weights) > 0 {
		return fg.selector.SelectWeighted(candidates, weights)
	}
	return fg.selector.Select(candidates)
}

func (fg *FlowGrammar) GenerateFromConcepts(concepts []string) string {
	if len(concepts) == 0 {
		return ""
	}

	primary := fg.selector.Select(concepts)

	var verbs, objects []string
	if fg.GetWordsByPOS != nil {
		verbs = fg.GetWordsByPOS("v")
		objects = fg.GetWordsByPOS("n")
	}

	if len(verbs) == 0 {
		verbs = []string{"involves", "concerns", "relates to", "indicates", "represents"}
	}
	if len(objects) == 0 {
		objects = concepts
	}

	verb := fg.selector.Select(verbs)
	object := fg.selector.Select(objects)

	if object == primary && len(objects) > 1 {
		for _, o := range objects {
			if o != primary {
				object = o
				break
			}
		}
	}

	return "The " + primary + " " + verb + " the " + object + "."
}
