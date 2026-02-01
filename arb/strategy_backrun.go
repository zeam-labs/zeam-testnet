package arb

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type BackrunStrategyConfig struct {

	MinPoolImpactBPS float64

	MinFlowConfidence float64

	PostSettleDelay     time.Duration
	ExecutionWindowDuration time.Duration

	EnabledChains []uint64
}

func DefaultBackrunStrategyConfig() *BackrunStrategyConfig {
	return &BackrunStrategyConfig{
		MinPoolImpactBPS:        25,
		MinFlowConfidence:       0.3,
		PostSettleDelay:         2 * time.Second,
		ExecutionWindowDuration: 6 * time.Second,
		EnabledChains: []uint64{
			ChainIDEthereum, ChainIDBase, ChainIDOptimism,
			ChainIDArbitrum, ChainIDZora, ChainIDMode,
		},
	}
}

type BackrunStrategy struct {
	config    *BackrunStrategyConfig
	priceFeed *PriceFeed
}

func NewBackrunStrategy(priceFeed *PriceFeed, config *BackrunStrategyConfig) *BackrunStrategy {
	if config == nil {
		config = DefaultBackrunStrategyConfig()
	}
	return &BackrunStrategy{
		config:    config,
		priceFeed: priceFeed,
	}
}

func (s *BackrunStrategy) Type() StrategyType { return StrategyBackrun }
func (s *BackrunStrategy) Name() string       { return "backrun" }

func (s *BackrunStrategy) OnSignal(signal FlowSignal, state *FlowState) *ExecutionIntent {
	switch signal.Type {
	case SignalSwapSequence:
		return s.onSwapSequence(signal, state)
	case SignalPoolImpact:
		return s.onPoolImpact(signal, state)
	case SignalFlashblockConfirm:
		return s.onFlashblockConfirm(signal, state)
	default:
		return nil
	}
}

func (s *BackrunStrategy) OnFlashblock(fb *FlashblockData, state *FlowState) *FlashblockDecision {
	return nil
}

func (s *BackrunStrategy) onSwapSequence(signal FlowSignal, state *FlowState) *ExecutionIntent {
	if len(signal.AffectedPools) == 0 {
		return nil
	}

	pool := signal.AffectedPools[0]
	forecast := state.PoolForecasts[pool]
	if forecast == nil || forecast.PredictedPrice == nil {
		return nil
	}

	if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS {
		return nil
	}

	if !s.isEnabledChain(forecast.ChainID) {
		return nil
	}

	return s.buildBackrunIntent(pool, forecast, signal, state)
}

func (s *BackrunStrategy) onPoolImpact(signal FlowSignal, state *FlowState) *ExecutionIntent {
	for pool, forecast := range signal.PoolImpacts {
		if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS {
			continue
		}
		if forecast.PredictedPrice == nil {
			continue
		}
		if !s.isEnabledChain(forecast.ChainID) {
			continue
		}

		intent := s.buildBackrunIntent(pool, forecast, signal, state)
		if intent != nil {
			return intent
		}
	}
	return nil
}

func (s *BackrunStrategy) onFlashblockConfirm(signal FlowSignal, state *FlowState) *ExecutionIntent {

	for pool, forecast := range state.PoolForecasts {
		if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS {
			continue
		}
		if forecast.ChainID != ChainIDBase {
			continue
		}

		intent := s.buildBackrunIntent(pool, forecast, signal, state)
		if intent != nil {

			intent.EarliestTime = time.Now().Add(200 * time.Millisecond)
			return intent
		}
	}
	return nil
}

func (s *BackrunStrategy) buildBackrunIntent(
	pool common.Address,
	forecast *PoolImpactForecast,
	signal FlowSignal,
	state *FlowState,
) *ExecutionIntent {
	confidence := s.computeConfidence(state, forecast)
	if confidence < s.config.MinFlowConfidence {
		return nil
	}

	now := time.Now()

	earliest := now.Add(s.config.PostSettleDelay)
	latest := earliest.Add(s.config.ExecutionWindowDuration)

	opp := &ArbitrageOpportunity{
		Type:       OpTypeSameChainDEX,
		Status:     StatusDetected,
		BuyChain:   forecast.ChainID,
		SellChain:  forecast.ChainID,
		SpreadBPS:  int64(forecast.CumulativeImpactBPS),
		DetectedAt: now,
		ExpiresAt:  latest,
		Score:      confidence,
	}
	opp.ComputeID()

	intent := &ExecutionIntent{
		ID:             opp.ID,
		Strategy:       StrategyBackrun,
		Opportunity:    opp,
		EarliestTime:   earliest,
		LatestTime:     latest,
		FlowConfidence: confidence,
		SignalSource:   signal.Type,
		TargetChainID:  forecast.ChainID,
		TargetPools:    []common.Address{pool},
		PressureAtEmit: state.Gradient,
	}

	if forecast.ChainID == ChainIDBase {
		intent.NeedsFlashblockValidation = true
		intent.FlashblockPrediction = forecast
	}

	return intent
}

func (s *BackrunStrategy) computeConfidence(state *FlowState, forecast *PoolImpactForecast) float64 {

	impactScore := clampFloat(forecast.CumulativeImpactBPS/80.0, 0, 1)
	forecastConf := forecast.Confidence

	gradientScore := 0.0
	if state.Gradient.IsAccelerating() {
		gradientScore = clampFloat(state.Gradient.DPressureDt, 0, 1)
	}

	return 0.35*impactScore + 0.4*forecastConf + 0.25*gradientScore
}

func (s *BackrunStrategy) isEnabledChain(chainID uint64) bool {
	for _, c := range s.config.EnabledChains {
		if c == chainID {
			return true
		}
	}
	return false
}

var _ Strategy = (*BackrunStrategy)(nil)
