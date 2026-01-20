

package ngac

import (
	"encoding/json"
	"fmt"
	"math/big"

	"zeam/quantum"
)


type SemanticAtom struct {
	Concept   string  
	Intensity float64 
	Valence   float64 
}


type SemanticState struct {
	QuantumValue *big.Int 
	Intent       string   
	Message      string   
	Confidence   float64  
	Atoms        []SemanticAtom
}


type SemanticField struct {
	Domain  string                   
	States  []SemanticState          
	Decoder map[string]SemanticState 
	Genesis [32]byte                 
}


type Example struct {
	Concept   string   
	Intensity float64  
	Valence   float64  
	Intent    string   
	Message   string   
	Contexts  []string 
}


func BuildSemanticField(sc *quantum.SubstrateChain, domain string, examples []Example) *SemanticField {
	field := &SemanticField{
		Domain:  domain,
		States:  make([]SemanticState, 0, len(examples)),
		Decoder: make(map[string]SemanticState),
	}

	if sc != nil {
		field.Genesis = sc.State.Genesis
	}

	fmt.Printf("[NGAC] Building semantic field: %s\n", domain)
	fmt.Printf("[NGAC] Training examples: %d\n", len(examples))

	
	for i, ex := range examples {
		
		contexts := buildContexts(ex)

		
		contract := quantum.QuantumContract{
			Contexts:  contexts,
			InitState: fmt.Sprintf("%d", i*1000000), 
			Transform: "affine",
			Branch:    "affine4",
			T:         8, 
			K:         128,
			Phase:     "alt",
		}

		contractID := quantum.DeployContract(contract)
		result, err := quantum.ExecuteContract(sc, contractID)
		if err != nil {
			continue
		}

		
		state := SemanticState{
			QuantumValue: result.Result,
			Intent:       ex.Intent,
			Message:      ex.Message,
			Confidence:   result.Pressure.Coherence,
			Atoms: []SemanticAtom{
				{Concept: ex.Concept, Intensity: ex.Intensity, Valence: ex.Valence},
			},
		}

		
		stateKey := result.Result.String()
		field.States = append(field.States, state)
		field.Decoder[stateKey] = state

		if (i+1)%10 == 0 || i == len(examples)-1 {
			fmt.Printf("[NGAC] Mapped: %d/%d states\n", i+1, len(examples))
		}
	}

	
	if sc != nil {
		fieldBytes, _ := json.Marshal(field)
		sc.MintEvent("SEMANTIC_FIELD_BUILT", fmt.Sprintf("domain=%s,states=%d", domain, len(field.States)))
		sc.Mint(fieldBytes)
	}

	fmt.Printf("[NGAC] Semantic field ready: %d states mapped\n\n", len(field.Decoder))
	return field
}


func buildContexts(ex Example) []string {
	contexts := make([]string, 0, 5)

	
	contexts = append(contexts, ex.Contexts...)

	
	if ex.Intensity > 0 {
		contexts = append(contexts, fmt.Sprintf("mul:%d", int(ex.Intensity*10)+1))
	}

	
	if ex.Valence != 0 {
		if ex.Valence > 0 {
			contexts = append(contexts, fmt.Sprintf("add:%d", int(ex.Valence*1000)))
		} else {
			contexts = append(contexts, fmt.Sprintf("sub:%d", int(-ex.Valence*1000)))
		}
	}

	
	contexts = append(contexts, "dom:"+ex.Concept)

	return contexts
}


func (field *SemanticField) MeasureSemantics(sc *quantum.SubstrateChain, pressure quantum.PressureMetrics, seed []byte) SemanticState {
	
	seedVal := int64(0)
	for i, b := range seed {
		if i >= 8 {
			break
		}
		seedVal = seedVal*256 + int64(b)
	}

	
	contract := quantum.QuantumContract{
		Contexts:  encodePressureContext(pressure, field.Domain),
		InitState: encodePressureStateWithSeed(pressure, seedVal),
		Transform: "affine",
		Branch:    "affine4",
		T:         12, 
		K:         256,
		Phase:     "alt",
	}

	
	contractID := quantum.DeployContract(contract)
	result, _ := quantum.ExecuteContract(sc, contractID)

	
	collapsedValue := result.Result.String()

	
	if state, exists := field.Decoder[collapsedValue]; exists {
		
		return state
	}

	
	return field.FindNearestMessage(result.Result, pressure, seed)
}


func encodePressureContext(p quantum.PressureMetrics, domain string) []string {
	contexts := make([]string, 0, 5)

	
	mag := int(p.Magnitude * 10)
	if mag < 1 {
		mag = 1
	}
	contexts = append(contexts, fmt.Sprintf("mul:%d", mag))

	
	coh := int(p.Coherence * 1000)
	contexts = append(contexts, fmt.Sprintf("add:%d", coh))

	
	if p.Tension > 0.01 {
		ten := int(p.Tension * 100)
		contexts = append(contexts, fmt.Sprintf("scale:%d", ten))
	}

	
	contexts = append(contexts, fmt.Sprintf("dom:%s-%.2f", domain, p.Density))

	return contexts
}


func encodePressureState(p quantum.PressureMetrics) string {
	
	combined := int(p.Magnitude*1000000 + p.Density*10000 + p.Coherence*100)
	return fmt.Sprintf("%d", combined)
}


func encodePressureStateWithSeed(p quantum.PressureMetrics, seed int64) string {
	
	combined := int64(p.Magnitude*1000000+p.Density*10000+p.Coherence*100) + (seed % 10000000)
	return fmt.Sprintf("%d", combined)
}


func (field *SemanticField) FindNearestMessage(quantumValue *big.Int, pressure quantum.PressureMetrics, seed []byte) SemanticState {
	if len(field.States) == 0 {
		return SemanticState{
			Intent:  "status",
			Message: "System operational.",
		}
	}

	
	type scoredState struct {
		state SemanticState
		score float64
	}

	candidates := make([]scoredState, 0, len(field.States))

	for _, state := range field.States {
		if len(state.Atoms) == 0 {
			continue
		}

		atom := state.Atoms[0]

		
		intensityMatch := 1.0 - abs(pressure.Magnitude-atom.Intensity)

		
		valenceTarget := 0.5 - pressure.Tension
		valenceMatch := 1.0 - abs(valenceTarget-atom.Valence)/2.0

		
		coherenceMatch := 1.0 - abs(pressure.Coherence-state.Confidence)

		
		score := intensityMatch*0.4 + valenceMatch*0.3 + coherenceMatch*0.3

		candidates = append(candidates, scoredState{state: state, score: score})
	}

	if len(candidates) == 0 {
		return SemanticState{
			Intent:  "status",
			Message: "System operational.",
		}
	}

	
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	
	topN := 5
	if len(candidates) < topN {
		topN = len(candidates)
	}

	
	seedHash := 0
	for _, b := range seed {
		seedHash = seedHash*31 + int(b)
	}
	if seedHash < 0 {
		seedHash = -seedHash
	}

	selectedIdx := seedHash % topN
	selected := candidates[selectedIdx].state
	selected.Confidence *= 0.8 

	return selected
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}


func DefaultSpeechExamples() []Example {
	return []Example{
		
		{
			Concept:   "urgency-critical",
			Intensity: 1.0,
			Valence:   -0.5,
			Intent:    "alert",
			Message:   "Critical threshold exceeded. Immediate action required.",
			Contexts:  []string{"mul:10", "dom:urgent"},
		},
		{
			Concept:   "urgency-high",
			Intensity: 0.8,
			Valence:   -0.2,
			Intent:    "alert",
			Message:   "High pressure detected. Initiating response protocol.",
			Contexts:  []string{"mul:8", "dom:urgent"},
		},
		{
			Concept:   "urgency-medium",
			Intensity: 0.5,
			Valence:   0.0,
			Intent:    "inform",
			Message:   "Attention required. Monitoring situation.",
			Contexts:  []string{"mul:5", "dom:monitor"},
		},
		{
			Concept:   "urgency-low",
			Intensity: 0.2,
			Valence:   0.3,
			Intent:    "status",
			Message:   "System stable. Operating within parameters.",
			Contexts:  []string{"mul:2", "dom:stable"},
		},

		
		{
			Concept:   "analysis-route",
			Intensity: 0.7,
			Valence:   0.5,
			Intent:    "decide",
			Message:   "Route evaluation complete. Optimal path identified.",
			Contexts:  []string{"mul:7", "dom:navigation"},
		},
		{
			Concept:   "analysis-optimize",
			Intensity: 0.6,
			Valence:   0.6,
			Intent:    "inform",
			Message:   "Performance analysis complete. Parameters optimized.",
			Contexts:  []string{"mul:6", "dom:optimization"},
		},
		{
			Concept:   "analysis-pattern",
			Intensity: 0.5,
			Valence:   0.2,
			Intent:    "inform",
			Message:   "Pattern detected in accumulated memory. Analyzing structure.",
			Contexts:  []string{"mul:5", "dom:pattern"},
		},
		{
			Concept:   "analysis-search",
			Intensity: 0.6,
			Valence:   0.3,
			Intent:    "inform",
			Message:   "Autonomous search initiated. Scanning solution space.",
			Contexts:  []string{"mul:6", "dom:search"},
		},

		
		{
			Concept:   "decision-confirmed",
			Intensity: 0.9,
			Valence:   0.7,
			Intent:    "decide",
			Message:   "Decision finalized. Proceeding with selected action.",
			Contexts:  []string{"mul:9", "dom:execute"},
		},
		{
			Concept:   "decision-evaluating",
			Intensity: 0.6,
			Valence:   0.1,
			Intent:    "inform",
			Message:   "Evaluating options. Multiple pathways under consideration.",
			Contexts:  []string{"mul:6", "dom:evaluate"},
		},
		{
			Concept:   "decision-deferred",
			Intensity: 0.3,
			Valence:   -0.1,
			Intent:    "inform",
			Message:   "Decision deferred. Awaiting additional data.",
			Contexts:  []string{"mul:3", "dom:wait"},
		},

		
		{
			Concept:   "status-normal",
			Intensity: 0.3,
			Valence:   0.5,
			Intent:    "status",
			Message:   "All systems nominal. Continuing standard operations.",
			Contexts:  []string{"mul:3", "dom:normal"},
		},
		{
			Concept:   "status-active",
			Intensity: 0.7,
			Valence:   0.4,
			Intent:    "status",
			Message:   "Active processing. Multiple contracts in execution.",
			Contexts:  []string{"mul:7", "dom:active"},
		},
		{
			Concept:   "status-convergence",
			Intensity: 0.6,
			Valence:   0.6,
			Intent:    "status",
			Message:   "Pressure converging. System approaching equilibrium.",
			Contexts:  []string{"mul:6", "dom:converge"},
		},

		
		{
			Concept:   "coord-mesh",
			Intensity: 0.5,
			Valence:   0.3,
			Intent:    "inform",
			Message:   "Mesh coordination active. Cross-chain anchoring enabled.",
			Contexts:  []string{"mul:5", "dom:mesh"},
		},
		{
			Concept:   "coord-interference",
			Intensity: 0.7,
			Valence:   0.2,
			Intent:    "inform",
			Message:   "Pressure interference detected. Adjusting to composite field.",
			Contexts:  []string{"mul:7", "dom:interference"},
		},

		
		{
			Concept:   "reflect-history",
			Intensity: 0.4,
			Valence:   0.1,
			Intent:    "inform",
			Message:   "Reviewing accumulated memory. Historical patterns identified.",
			Contexts:  []string{"mul:4", "dom:history"},
		},
		{
			Concept:   "reflect-performance",
			Intensity: 0.5,
			Valence:   0.4,
			Intent:    "inform",
			Message:   "Performance metrics analyzed. Efficiency within target range.",
			Contexts:  []string{"mul:5", "dom:metrics"},
		},

		
		{
			Concept:   "learn-adaptation",
			Intensity: 0.6,
			Valence:   0.5,
			Intent:    "inform",
			Message:   "Adaptive optimization detected. Updating operational parameters.",
			Contexts:  []string{"mul:6", "dom:adapt"},
		},
		{
			Concept:   "learn-discovery",
			Intensity: 0.8,
			Valence:   0.7,
			Intent:    "inform",
			Message:   "Novel pattern discovered. Expanding semantic field.",
			Contexts:  []string{"mul:8", "dom:discover"},
		},

		
		{
			Concept:   "trade-opportunity",
			Intensity: 0.8,
			Valence:   0.6,
			Intent:    "alert",
			Message:   "Arbitrage opportunity detected. Evaluating execution path.",
			Contexts:  []string{"mul:8", "dom:arbitrage"},
		},
		{
			Concept:   "trade-execute",
			Intensity: 0.9,
			Valence:   0.7,
			Intent:    "decide",
			Message:   "Executing trade. Flash loan sequence initiated.",
			Contexts:  []string{"mul:9", "dom:execute"},
		},
		{
			Concept:   "trade-profit",
			Intensity: 0.7,
			Valence:   0.8,
			Intent:    "status",
			Message:   "Trade completed successfully. Profit captured.",
			Contexts:  []string{"mul:7", "dom:profit"},
		},
		{
			Concept:   "trade-risk",
			Intensity: 0.6,
			Valence:   -0.3,
			Intent:    "alert",
			Message:   "Risk threshold approaching. Monitoring position.",
			Contexts:  []string{"mul:6", "dom:risk"},
		},

		
		{
			Concept:   "market-volatile",
			Intensity: 0.7,
			Valence:   -0.2,
			Intent:    "inform",
			Message:   "Market volatility elevated. Adjusting parameters.",
			Contexts:  []string{"mul:7", "dom:volatility"},
		},
		{
			Concept:   "market-stable",
			Intensity: 0.3,
			Valence:   0.4,
			Intent:    "status",
			Message:   "Market conditions stable. Standard operation mode.",
			Contexts:  []string{"mul:3", "dom:stable"},
		},
		{
			Concept:   "market-opportunity",
			Intensity: 0.6,
			Valence:   0.5,
			Intent:    "inform",
			Message:   "Market inefficiency detected. Analyzing potential.",
			Contexts:  []string{"mul:6", "dom:opportunity"},
		},
	}
}
