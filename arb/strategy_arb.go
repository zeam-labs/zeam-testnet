package arb

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type ArbStrategyConfig struct {

	MinSpreadBPS float64

	MinFlowConfidence float64

	ExecutionWindowDuration time.Duration

	MinPoolImpactBPS float64

	EnabledChains []uint64

	UseFlashblockValidation bool
}

func DefaultArbStrategyConfig() *ArbStrategyConfig {
	return &ArbStrategyConfig{
		MinSpreadBPS:            10,
		MinFlowConfidence:       0.3,
		ExecutionWindowDuration: 4 * time.Second,
		MinPoolImpactBPS:        20,
		EnabledChains: []uint64{
			ChainIDEthereum, ChainIDBase, ChainIDOptimism,
			ChainIDArbitrum, ChainIDZora, ChainIDMode,
		},
		UseFlashblockValidation: true,
	}
}

type ArbStrategy struct {
	config *ArbStrategyConfig

	priceFeed *PriceFeed
}

func NewArbStrategy(priceFeed *PriceFeed, config *ArbStrategyConfig) *ArbStrategy {
	if config == nil {
		config = DefaultArbStrategyConfig()
	}
	return &ArbStrategy{
		config:    config,
		priceFeed: priceFeed,
	}
}

func (s *ArbStrategy) Type() StrategyType { return StrategyArbitrage }
func (s *ArbStrategy) Name() string       { return "flow_arbitrage" }

func (s *ArbStrategy) OnSignal(signal FlowSignal, state *FlowState) *ExecutionIntent {
	switch signal.Type {
	case SignalPoolImpact:
		return s.onPoolImpact(signal, state)
	case SignalPressureSpike:
		return s.onPressureSpike(signal, state)
	case SignalSwapSequence:
		return s.onSwapSequence(signal, state)
	default:
		return nil
	}
}

func (s *ArbStrategy) OnFlashblock(fb *FlashblockData, state *FlowState) *FlashblockDecision {

	return nil
}

func (s *ArbStrategy) onPoolImpact(signal FlowSignal, state *FlowState) *ExecutionIntent {
	if s.priceFeed == nil {
		return nil
	}

	for pool, forecast := range signal.PoolImpacts {
		if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS {
			continue
		}
		if forecast.PredictedPrice == nil {
			continue
		}

		intent := s.findSpreadOpportunity(pool, forecast, signal, state)
		if intent != nil {
			return intent
		}
	}

	return nil
}

func (s *ArbStrategy) onPressureSpike(signal FlowSignal, state *FlowState) *ExecutionIntent {
	if s.priceFeed == nil {
		return nil
	}

	for pool, forecast := range state.PoolForecasts {
		if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS {
			continue
		}
		if forecast.PredictedPrice == nil {
			continue
		}

		intent := s.findSpreadOpportunity(pool, forecast, signal, state)
		if intent != nil {
			return intent
		}
	}

	return nil
}

func (s *ArbStrategy) onSwapSequence(signal FlowSignal, state *FlowState) *ExecutionIntent {
	if s.priceFeed == nil || len(signal.AffectedPools) == 0 {
		return nil
	}

	pool := signal.AffectedPools[0]
	forecast := state.PoolForecasts[pool]
	if forecast == nil || forecast.PredictedPrice == nil {
		return nil
	}

	if forecast.CumulativeImpactBPS < s.config.MinPoolImpactBPS*1.5 {
		return nil
	}

	return s.findSpreadOpportunity(pool, forecast, signal, state)
}

func (s *ArbStrategy) findSpreadOpportunity(
	pool common.Address,
	forecast *PoolImpactForecast,
	signal FlowSignal,
	state *FlowState,
) *ExecutionIntent {

	spreads := s.priceFeed.GetAllSpreads()
	if len(spreads) == 0 {
		return nil
	}

	var bestSpread *SpreadInfo
	for _, spread := range spreads {
		if spread.SpreadBPS < int64(s.config.MinSpreadBPS) {
			continue
		}

		if spread.BuyChain == forecast.ChainID || spread.SellChain == forecast.ChainID {
			if bestSpread == nil || spread.SpreadBPS > bestSpread.SpreadBPS {
				bestSpread = spread
			}
		}
	}

	if bestSpread == nil {
		return nil
	}

	confidence := s.computeFlowConfidence(state)
	if confidence < s.config.MinFlowConfidence {
		return nil
	}

	now := time.Now()

	opp := &ArbitrageOpportunity{
		Type:       OpTypeCrossChainSame,
		Status:     StatusDetected,
		BuyChain:   bestSpread.BuyChain,
		SellChain:  bestSpread.SellChain,
		SpreadBPS:  bestSpread.SpreadBPS,
		DetectedAt: now,
		ExpiresAt:  now.Add(s.config.ExecutionWindowDuration),
		Score:      confidence,
	}
	if bestSpread.BuyChain != bestSpread.SellChain {
		opp.IsCrossChain = true
	}
	opp.ComputeID()

	intent := &ExecutionIntent{
		ID:             opp.ID,
		Strategy:       StrategyArbitrage,
		Opportunity:    opp,
		EarliestTime:   now,
		LatestTime:     now.Add(s.config.ExecutionWindowDuration),
		FlowConfidence: confidence,
		SignalSource:   signal.Type,
		TargetChainID:  bestSpread.BuyChain,
		TargetPools:    []common.Address{pool},
		PressureAtEmit: state.Gradient,
	}

	if s.config.UseFlashblockValidation && forecast.ChainID == ChainIDBase {
		intent.NeedsFlashblockValidation = true
		intent.FlashblockPrediction = forecast
	}

	return intent
}

func (s *ArbStrategy) computeFlowConfidence(state *FlowState) float64 {

	gradientScore := 0.0
	if state.Gradient.IsAccelerating() {
		gradientScore = clampFloat(state.Gradient.DPressureDt, 0, 1)
	}

	coherenceScore := state.Pressure.PauliX

	flowScore := clampFloat(state.FlowRate/50.0, 0, 1)

	return 0.4*gradientScore + 0.35*coherenceScore + 0.25*flowScore
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func generateIntentID(strategy StrategyType, pool common.Address, chainID uint64, t time.Time) [32]byte {
	data := make([]byte, 0, 64)
	data = append(data, byte(strategy))
	data = append(data, pool.Bytes()...)
	data = append(data, byte(chainID>>8), byte(chainID))
	data = append(data, byte(t.UnixNano()>>32))
	return crypto.Keccak256Hash(data)
}
