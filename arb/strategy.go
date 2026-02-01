package arb

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type StrategyType int

const (
	StrategyArbitrage    StrategyType = iota
	StrategyLiquidation
	StrategyBackrun
	StrategyCrossChainMEV
)

func (st StrategyType) String() string {
	switch st {
	case StrategyArbitrage:
		return "arbitrage"
	case StrategyLiquidation:
		return "liquidation"
	case StrategyBackrun:
		return "backrun"
	case StrategyCrossChainMEV:
		return "crosschain_mev"
	default:
		return "unknown"
	}
}

type Strategy interface {

	Type() StrategyType

	Name() string

	OnSignal(signal FlowSignal, state *FlowState) *ExecutionIntent

	OnFlashblock(fb *FlashblockData, state *FlowState) *FlashblockDecision
}

type ExecutionIntent struct {

	ID       [32]byte
	Strategy StrategyType

	Opportunity *ArbitrageOpportunity

	EarliestBlock uint64
	LatestBlock   uint64
	EarliestTime  time.Time
	LatestTime    time.Time

	FlowConfidence float64
	SignalSource   FlowSignalType

	NeedsFlashblockValidation bool
	FlashblockPrediction      *PoolImpactForecast

	TargetChainID  uint64
	TargetPools    []common.Address
	PendingSwaps   []*PendingSwap
	PressureAtEmit PressureGradient
}

func (ei *ExecutionIntent) IsExpired(now time.Time, currentBlock uint64) bool {
	if !ei.LatestTime.IsZero() && now.After(ei.LatestTime) {
		return true
	}
	if ei.LatestBlock > 0 && currentBlock > ei.LatestBlock {
		return true
	}
	return false
}

func (ei *ExecutionIntent) IsReady(now time.Time, currentBlock uint64) bool {
	if !ei.EarliestTime.IsZero() && now.Before(ei.EarliestTime) {
		return false
	}
	if ei.EarliestBlock > 0 && currentBlock < ei.EarliestBlock {
		return false
	}
	return !ei.IsExpired(now, currentBlock)
}

type FlashblockAction int

const (
	FlashblockConfirm FlashblockAction = iota
	FlashblockCancel
	FlashblockModify
)

type FlashblockDecision struct {
	IntentID       [32]byte
	Action         FlashblockAction
	ModifiedIntent *ExecutionIntent
	Reason         string
}
