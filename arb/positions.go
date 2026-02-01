package arb

import (
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type PositionManager struct {
	mu sync.RWMutex

	positions map[uint64]map[common.Address]*Position

	targets map[uint64]map[common.Address]*big.Int

	config *PositionConfig

	totalTrades        uint64
	totalProfitWei     *big.Int
	lastRebalanceCheck time.Time

	pendingRebalances []RebalanceTrade
}

type PositionConfig struct {
	RebalanceThreshold float64
	MinRebalanceAmount *big.Int
	RebalanceCheckFreq time.Duration
}

func DefaultPositionConfig() *PositionConfig {
	return &PositionConfig{
		RebalanceThreshold: 0.2,
		MinRebalanceAmount: big.NewInt(1e17),
		RebalanceCheckFreq: 5 * time.Minute,
	}
}

func NewPositionManager(initialPositions map[uint64]map[common.Address]*big.Int, config *PositionConfig) *PositionManager {
	if config == nil {
		config = DefaultPositionConfig()
	}

	pm := &PositionManager{
		positions:      make(map[uint64]map[common.Address]*Position),
		targets:        make(map[uint64]map[common.Address]*big.Int),
		config:         config,
		totalProfitWei: big.NewInt(0),
	}

	for chainID, tokens := range initialPositions {
		pm.positions[chainID] = make(map[common.Address]*Position)
		pm.targets[chainID] = make(map[common.Address]*big.Int)

		for token, amount := range tokens {
			pm.positions[chainID][token] = &Position{
				ChainID:       chainID,
				Token:         token,
				Amount:        new(big.Int).Set(amount),
				Available:     new(big.Int).Set(amount),
				Locked:        big.NewInt(0),
				LastRebalance: time.Now(),
			}
			pm.targets[chainID][token] = new(big.Int).Set(amount)
		}
	}

	return pm
}

func (pm *PositionManager) GetPosition(chainID uint64, token common.Address) *Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if chain, ok := pm.positions[chainID]; ok {
		if pos, ok := chain[token]; ok {
			return pos
		}
	}
	return nil
}

func (pm *PositionManager) GetChainPositions(chainID uint64) map[common.Address]*Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[common.Address]*Position)
	if chain, ok := pm.positions[chainID]; ok {
		for token, pos := range chain {
			result[token] = pos
		}
	}
	return result
}

func (pm *PositionManager) GetAllPositions() map[uint64]map[common.Address]*Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[uint64]map[common.Address]*Position)
	for chainID, tokens := range pm.positions {
		result[chainID] = make(map[common.Address]*Position)
		for token, pos := range tokens {
			result[chainID][token] = pos
		}
	}
	return result
}

func (pm *PositionManager) GetTotalValue() *big.Int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	total := big.NewInt(0)
	for _, tokens := range pm.positions {
		for _, pos := range tokens {
			total.Add(total, pos.Amount)
		}
	}
	return total
}

func (pm *PositionManager) Lock(chainID uint64, token common.Address, amount *big.Int) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos := pm.getPositionUnsafe(chainID, token)
	if pos == nil {
		return fmt.Errorf("no position for chain %d token %s", chainID, token.Hex())
	}

	if pos.Available.Cmp(amount) < 0 {
		return fmt.Errorf("insufficient available balance: have %s, need %s",
			pos.Available.String(), amount.String())
	}

	pos.Available.Sub(pos.Available, amount)
	pos.Locked.Add(pos.Locked, amount)
	return nil
}

func (pm *PositionManager) Unlock(chainID uint64, token common.Address, amount *big.Int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos := pm.getPositionUnsafe(chainID, token)
	if pos == nil {
		return
	}

	pos.Locked.Sub(pos.Locked, amount)
	pos.Available.Add(pos.Available, amount)
}

func (pm *PositionManager) getPositionUnsafe(chainID uint64, token common.Address) *Position {
	if chain, ok := pm.positions[chainID]; ok {
		if pos, ok := chain[token]; ok {
			return pos
		}
	}
	return nil
}

func (pm *PositionManager) AdjustAfterTrade(chainID uint64, tokenIn, tokenOut common.Address, amountIn, amountOut *big.Int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pos := pm.getPositionUnsafe(chainID, tokenIn); pos != nil {
		pos.Amount.Sub(pos.Amount, amountIn)

		pos.Locked.Sub(pos.Locked, amountIn)
	}

	if pos := pm.getPositionUnsafe(chainID, tokenOut); pos != nil {
		pos.Amount.Add(pos.Amount, amountOut)
		pos.Available.Add(pos.Available, amountOut)
	} else {

		if pm.positions[chainID] == nil {
			pm.positions[chainID] = make(map[common.Address]*Position)
		}
		pm.positions[chainID][tokenOut] = &Position{
			ChainID:   chainID,
			Token:     tokenOut,
			Amount:    new(big.Int).Set(amountOut),
			Available: new(big.Int).Set(amountOut),
			Locked:    big.NewInt(0),
		}
	}

	pm.totalTrades++
}

func (pm *PositionManager) RecordProfit(profit *big.Int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.totalProfitWei.Add(pm.totalProfitWei, profit)
}

func (pm *PositionManager) GetStats() PositionStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return PositionStats{
		TotalTrades:    pm.totalTrades,
		TotalProfitWei: new(big.Int).Set(pm.totalProfitWei),
		TotalValue:     pm.GetTotalValue(),
		ChainCount:     len(pm.positions),
	}
}

type PositionStats struct {
	TotalTrades    uint64
	TotalProfitWei *big.Int
	TotalValue     *big.Int
	ChainCount     int
}

func (pm *PositionManager) NeedsRebalance() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for chainID, tokens := range pm.positions {
		for token, pos := range tokens {
			target, ok := pm.targets[chainID][token]
			if !ok {
				continue
			}

			if target.Sign() == 0 {
				continue
			}

			diff := new(big.Int).Sub(pos.Amount, target)
			diff.Abs(diff)

			deviation := new(big.Float).Quo(
				new(big.Float).SetInt(diff),
				new(big.Float).SetInt(target),
			)

			devFloat, _ := deviation.Float64()
			if devFloat > pm.config.RebalanceThreshold {
				return true
			}
		}
	}

	return false
}

func (pm *PositionManager) GetRebalancePlan() []RebalanceTrade {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var trades []RebalanceTrade

	type Imbalance struct {
		ChainID uint64
		Token   common.Address
		Delta   *big.Int
	}

	var imbalances []Imbalance

	for chainID, tokens := range pm.positions {
		for token, pos := range tokens {
			target, ok := pm.targets[chainID][token]
			if !ok {
				continue
			}

			delta := new(big.Int).Sub(pos.Amount, target)
			if delta.CmpAbs(pm.config.MinRebalanceAmount) < 0 {
				continue
			}

			imbalances = append(imbalances, Imbalance{
				ChainID: chainID,
				Token:   token,
				Delta:   delta,
			})
		}
	}

	for i := 0; i < len(imbalances); i++ {
		if imbalances[i].Delta.Sign() <= 0 {
			continue
		}

		for j := 0; j < len(imbalances); j++ {
			if i == j || imbalances[j].Delta.Sign() >= 0 {
				continue
			}

			if imbalances[i].Token != imbalances[j].Token {
				continue
			}

			overAmount := new(big.Int).Set(imbalances[i].Delta)
			underAmount := new(big.Int).Neg(imbalances[j].Delta)

			transferAmount := overAmount
			if underAmount.Cmp(overAmount) < 0 {
				transferAmount = underAmount
			}

			if transferAmount.Cmp(pm.config.MinRebalanceAmount) >= 0 {
				trades = append(trades, RebalanceTrade{
					FromChain: imbalances[i].ChainID,
					ToChain:   imbalances[j].ChainID,
					Token:     imbalances[i].Token,
					Amount:    transferAmount,
				})

				imbalances[i].Delta.Sub(imbalances[i].Delta, transferAmount)
				imbalances[j].Delta.Add(imbalances[j].Delta, transferAmount)
			}
		}
	}

	return trades
}

type RebalanceTrade struct {
	FromChain uint64
	ToChain   uint64
	Token     common.Address
	Amount    *big.Int
}

func (pm *PositionManager) SetTarget(chainID uint64, token common.Address, amount *big.Int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.targets[chainID] == nil {
		pm.targets[chainID] = make(map[common.Address]*big.Int)
	}
	pm.targets[chainID][token] = new(big.Int).Set(amount)
}

func (pm *PositionManager) GetTarget(chainID uint64, token common.Address) *big.Int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if chain, ok := pm.targets[chainID]; ok {
		if target, ok := chain[token]; ok {
			return new(big.Int).Set(target)
		}
	}
	return nil
}

func (pm *PositionManager) CanExecute(chainID uint64, token common.Address, amount *big.Int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pos := pm.getPositionUnsafe(chainID, token)
	if pos == nil {
		return false
	}

	return pos.Available.Cmp(amount) >= 0
}

func (pm *PositionManager) GetAvailable(chainID uint64, token common.Address) *big.Int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pos := pm.getPositionUnsafe(chainID, token)
	if pos == nil {
		return big.NewInt(0)
	}

	return new(big.Int).Set(pos.Available)
}

func (pm *PositionManager) GetMaxTradeSize(chainID uint64, token common.Address, maxPct float64) *big.Int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pos := pm.getPositionUnsafe(chainID, token)
	if pos == nil {
		return big.NewInt(0)
	}

	maxFromPct := new(big.Float).Mul(
		new(big.Float).SetInt(pos.Amount),
		big.NewFloat(maxPct),
	)
	maxFromPctInt, _ := maxFromPct.Int(nil)

	if maxFromPctInt.Cmp(pos.Available) < 0 {
		return maxFromPctInt
	}
	return new(big.Int).Set(pos.Available)
}
