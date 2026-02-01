package arb

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type LiquidationStrategyConfig struct {

	MinPoolImpactBPS float64

	MinProfitBPS float64

	MinFlowConfidence float64

	ExecutionWindowDuration time.Duration

	EnabledChains []uint64

	OracleHeartbeatByChain map[uint64]time.Duration
}

func DefaultLiquidationStrategyConfig() *LiquidationStrategyConfig {
	return &LiquidationStrategyConfig{
		MinPoolImpactBPS:        30,
		MinProfitBPS:            50,
		MinFlowConfidence:       0.35,
		ExecutionWindowDuration: 6 * time.Second,
		EnabledChains: []uint64{
			ChainIDEthereum, ChainIDBase, ChainIDOptimism, ChainIDArbitrum,
		},
		OracleHeartbeatByChain: map[uint64]time.Duration{
			ChainIDEthereum: 1 * time.Hour,
			ChainIDBase:     20 * time.Minute,
			ChainIDOptimism: 20 * time.Minute,
			ChainIDArbitrum: 20 * time.Minute,
		},
	}
}

type LiquidationStrategy struct {
	config    *LiquidationStrategyConfig
	priceFeed *PriceFeed
}

func NewLiquidationStrategy(priceFeed *PriceFeed, config *LiquidationStrategyConfig) *LiquidationStrategy {
	if config == nil {
		config = DefaultLiquidationStrategyConfig()
	}
	return &LiquidationStrategy{
		config:    config,
		priceFeed: priceFeed,
	}
}

func (s *LiquidationStrategy) Type() StrategyType { return StrategyLiquidation }
func (s *LiquidationStrategy) Name() string       { return "liquidation" }

func (s *LiquidationStrategy) OnSignal(signal FlowSignal, state *FlowState) *ExecutionIntent {
	switch signal.Type {
	case SignalPoolImpact:
		return s.onPoolImpact(signal, state)
	case SignalSwapSequence:
		return s.onSwapSequence(signal, state)
	case SignalPressureSpike:
		return s.onPressureSpike(signal, state)
	default:
		return nil
	}
}

func (s *LiquidationStrategy) OnFlashblock(fb *FlashblockData, state *FlowState) *FlashblockDecision {
	return nil
}

func (s *LiquidationStrategy) onPoolImpact(signal FlowSignal, state *FlowState) *ExecutionIntent {
	for pool, forecast := range signal.PoolImpacts {
		if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS {
			continue
		}
		if !s.isEnabledChain(forecast.ChainID) {
			continue
		}

		intent := s.checkLiquidationOpportunity(pool, forecast, signal, state)
		if intent != nil {
			return intent
		}
	}
	return nil
}

func (s *LiquidationStrategy) onSwapSequence(signal FlowSignal, state *FlowState) *ExecutionIntent {
	if len(signal.AffectedPools) == 0 {
		return nil
	}

	pool := signal.AffectedPools[0]
	forecast := state.PoolForecasts[pool]
	if forecast == nil {
		return nil
	}

	if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS*1.5 {
		return nil
	}

	return s.checkLiquidationOpportunity(pool, forecast, signal, state)
}

func (s *LiquidationStrategy) onPressureSpike(signal FlowSignal, state *FlowState) *ExecutionIntent {

	for pool, forecast := range state.PoolForecasts {
		if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS {
			continue
		}
		if !s.isEnabledChain(forecast.ChainID) {
			continue
		}

		intent := s.checkLiquidationOpportunity(pool, forecast, signal, state)
		if intent != nil {
			return intent
		}
	}
	return nil
}

func (s *LiquidationStrategy) checkLiquidationOpportunity(
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

	opp := &ArbitrageOpportunity{
		Type:       OpTypeFlashblockFront,
		Status:     StatusDetected,
		BuyChain:   forecast.ChainID,
		SellChain:  forecast.ChainID,
		SpreadBPS:  int64(forecast.CumulativeImpactBPS),
		DetectedAt: now,
		ExpiresAt:  now.Add(s.config.ExecutionWindowDuration),
		Score:      confidence,
	}
	opp.ComputeID()

	intent := &ExecutionIntent{
		ID:             opp.ID,
		Strategy:       StrategyLiquidation,
		Opportunity:    opp,
		EarliestTime:   now,
		LatestTime:     now.Add(s.config.ExecutionWindowDuration),
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

func (s *LiquidationStrategy) computeConfidence(state *FlowState, forecast *PoolImpactForecast) float64 {

	impactScore := clampFloat(forecast.CumulativeImpactBPS/100.0, 0, 1)

	gradientScore := 0.0
	if state.Gradient.IsAccelerating() {
		gradientScore = clampFloat(state.Gradient.DPressureDt, 0, 1)
	}

	forecastConfidence := forecast.Confidence

	return 0.4*impactScore + 0.3*forecastConfidence + 0.3*gradientScore
}

func (s *LiquidationStrategy) isEnabledChain(chainID uint64) bool {
	for _, c := range s.config.EnabledChains {
		if c == chainID {
			return true
		}
	}
	return false
}

var _ Strategy = (*LiquidationStrategy)(nil)
