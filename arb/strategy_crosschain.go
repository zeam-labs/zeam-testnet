package arb

import (
	"time"
)

type CrossChainStrategyConfig struct {

	MinCoherenceDrift float64

	MinCrossChainSpreadBPS float64

	MinFlowConfidence float64

	ExecutionWindowDuration time.Duration

	SourceChains []uint64

	TargetChains []uint64
}

func DefaultCrossChainStrategyConfig() *CrossChainStrategyConfig {
	return &CrossChainStrategyConfig{
		MinCoherenceDrift:      0.15,
		MinCrossChainSpreadBPS: 15,
		MinFlowConfidence:      0.25,
		ExecutionWindowDuration: 3 * time.Second,
		SourceChains: []uint64{
			ChainIDEthereum, ChainIDBase, ChainIDArbitrum,
		},
		TargetChains: []uint64{
			ChainIDBase, ChainIDOptimism, ChainIDArbitrum,
			ChainIDZora, ChainIDMode,
		},
	}
}

type CrossChainStrategy struct {
	config    *CrossChainStrategyConfig
	priceFeed *PriceFeed
}

func NewCrossChainStrategy(priceFeed *PriceFeed, config *CrossChainStrategyConfig) *CrossChainStrategy {
	if config == nil {
		config = DefaultCrossChainStrategyConfig()
	}
	return &CrossChainStrategy{
		config:    config,
		priceFeed: priceFeed,
	}
}

func (s *CrossChainStrategy) Type() StrategyType { return StrategyCrossChainMEV }
func (s *CrossChainStrategy) Name() string       { return "cross_chain_mev" }

func (s *CrossChainStrategy) OnSignal(signal FlowSignal, state *FlowState) *ExecutionIntent {
	switch signal.Type {
	case SignalCoherenceDrift:
		return s.onCoherenceDrift(signal, state)
	case SignalPoolImpact:
		return s.onPoolImpact(signal, state)
	case SignalSwapSequence:
		return s.onSwapSequence(signal, state)
	default:
		return nil
	}
}

func (s *CrossChainStrategy) OnFlashblock(fb *FlashblockData, state *FlowState) *FlashblockDecision {
	return nil
}

func (s *CrossChainStrategy) onCoherenceDrift(signal FlowSignal, state *FlowState) *ExecutionIntent {
	if s.priceFeed == nil {
		return nil
	}

	for chainID, coherence := range signal.ChainCoherence {
		drift := 1.0 - coherence
		if drift < s.config.MinCoherenceDrift {
			continue
		}

		if !s.isTargetChain(chainID) {
			continue
		}

		return s.findCrossChainSpread(chainID, signal, state)
	}

	return nil
}

func (s *CrossChainStrategy) onPoolImpact(signal FlowSignal, state *FlowState) *ExecutionIntent {
	if s.priceFeed == nil {
		return nil
	}

	for _, forecast := range signal.PoolImpacts {
		if forecast.CumulativeImpactBPS < s.config.MinCrossChainSpreadBPS {
			continue
		}
		if !s.isSourceChain(forecast.ChainID) {
			continue
		}

		for _, targetChain := range s.config.TargetChains {
			if targetChain == forecast.ChainID {
				continue
			}
			intent := s.buildCrossChainIntent(forecast.ChainID, targetChain, forecast, signal, state)
			if intent != nil {
				return intent
			}
		}
	}

	return nil
}

func (s *CrossChainStrategy) onSwapSequence(signal FlowSignal, state *FlowState) *ExecutionIntent {
	if s.priceFeed == nil || len(signal.AffectedPools) == 0 {
		return nil
	}

	pool := signal.AffectedPools[0]
	forecast := state.PoolForecasts[pool]
	if forecast == nil || forecast.PredictedPrice == nil {
		return nil
	}

	if !s.isSourceChain(forecast.ChainID) {
		return nil
	}

	if forecast.CumulativeImpactBPS < s.config.MinCrossChainSpreadBPS*2 {
		return nil
	}

	for _, targetChain := range s.config.TargetChains {
		if targetChain == forecast.ChainID {
			continue
		}
		intent := s.buildCrossChainIntent(forecast.ChainID, targetChain, forecast, signal, state)
		if intent != nil {
			return intent
		}
	}

	return nil
}

func (s *CrossChainStrategy) findCrossChainSpread(staleChain uint64, signal FlowSignal, state *FlowState) *ExecutionIntent {
	spreads := s.priceFeed.GetAllSpreads()
	for _, spread := range spreads {
		if spread.SpreadBPS < int64(s.config.MinCrossChainSpreadBPS) {
			continue
		}

		if spread.BuyChain != staleChain {
			continue
		}

		confidence := s.computeConfidence(state)
		if confidence < s.config.MinFlowConfidence {
			return nil
		}

		return s.buildIntent(spread.BuyChain, spread.SellChain, spread.SpreadBPS, confidence, signal, state)
	}
	return nil
}

func (s *CrossChainStrategy) buildCrossChainIntent(
	sourceChain, targetChain uint64,
	forecast *PoolImpactForecast,
	signal FlowSignal,
	state *FlowState,
) *ExecutionIntent {
	spreads := s.priceFeed.GetAllSpreads()
	for _, spread := range spreads {
		if spread.SpreadBPS < int64(s.config.MinCrossChainSpreadBPS) {
			continue
		}

		if (spread.BuyChain == targetChain && spread.SellChain == sourceChain) ||
			(spread.BuyChain == sourceChain && spread.SellChain == targetChain) {

			confidence := s.computeConfidence(state)
			if confidence < s.config.MinFlowConfidence {
				return nil
			}

			return s.buildIntent(spread.BuyChain, spread.SellChain, spread.SpreadBPS, confidence, signal, state)
		}
	}
	return nil
}

func (s *CrossChainStrategy) buildIntent(
	buyChain, sellChain uint64,
	spreadBPS int64,
	confidence float64,
	signal FlowSignal,
	state *FlowState,
) *ExecutionIntent {
	now := time.Now()

	opp := &ArbitrageOpportunity{
		Type:         OpTypeCrossChainSame,
		Status:       StatusDetected,
		BuyChain:     buyChain,
		SellChain:    sellChain,
		SpreadBPS:    spreadBPS,
		IsCrossChain: true,
		DetectedAt:   now,
		ExpiresAt:    now.Add(s.config.ExecutionWindowDuration),
		Score:        confidence,
	}
	opp.ComputeID()

	intent := &ExecutionIntent{
		ID:             opp.ID,
		Strategy:       StrategyCrossChainMEV,
		Opportunity:    opp,
		EarliestTime:   now,
		LatestTime:     now.Add(s.config.ExecutionWindowDuration),
		FlowConfidence: confidence,
		SignalSource:   signal.Type,
		TargetChainID:  buyChain,
		PressureAtEmit: state.Gradient,
	}

	if buyChain == ChainIDBase {
		intent.NeedsFlashblockValidation = true
	}

	return intent
}

func (s *CrossChainStrategy) computeConfidence(state *FlowState) float64 {

	gradientScore := 0.0
	if state.Gradient.IsAccelerating() {
		gradientScore = clampFloat(state.Gradient.DPressureDt, 0, 1)
	}

	driftScore := clampFloat(1.0-state.Pressure.PauliX, 0, 1)

	flowScore := clampFloat(state.FlowRate/50.0, 0, 1)

	return 0.25*gradientScore + 0.5*driftScore + 0.25*flowScore
}

func (s *CrossChainStrategy) isSourceChain(chainID uint64) bool {
	for _, c := range s.config.SourceChains {
		if c == chainID {
			return true
		}
	}
	return false
}

func (s *CrossChainStrategy) isTargetChain(chainID uint64) bool {
	for _, c := range s.config.TargetChains {
		if c == chainID {
			return true
		}
	}
	return false
}

var _ Strategy = (*CrossChainStrategy)(nil)
