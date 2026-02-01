package graph

import (
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

const (

	InfiniteWeight = 1e18

	MinReserve = 1e-18

	MaxSlippagePercent = 0.01
)

func CalculateEdgeWeights(edge *PoolEdge) {

	if edge.Reserve0 == nil || edge.Reserve1 == nil ||
		edge.Reserve0.Sign() == 0 || edge.Reserve1.Sign() == 0 {
		edge.Weight01 = InfiniteWeight
		edge.Weight10 = InfiniteWeight
		return
	}

	r0 := new(big.Float).SetInt(edge.Reserve0)
	r1 := new(big.Float).SetInt(edge.Reserve1)

	r0f, _ := r0.Float64()
	r1f, _ := r1.Float64()

	feeDecimal := float64(edge.Fee) / 10000.0
	feeFactor := 1.0 - feeDecimal

	rate01 := r1f / r0f

	rate10 := r0f / r1f

	effectiveRate01 := rate01 * feeFactor
	effectiveRate10 := rate10 * feeFactor

	if effectiveRate01 > MinReserve {
		edge.Weight01 = -math.Log(effectiveRate01)
	} else {
		edge.Weight01 = InfiniteWeight
	}

	if effectiveRate10 > MinReserve {
		edge.Weight10 = -math.Log(effectiveRate10)
	} else {
		edge.Weight10 = InfiniteWeight
	}
}

func CalculateMaxAmounts(edge *PoolEdge) {
	if edge.Reserve0 == nil || edge.Reserve1 == nil {
		edge.MaxAmount01 = big.NewInt(0)
		edge.MaxAmount10 = big.NewInt(0)
		return
	}

	slippageFactor := big.NewFloat(MaxSlippagePercent * 2)

	r0 := new(big.Float).SetInt(edge.Reserve0)
	max01 := new(big.Float).Mul(r0, slippageFactor)
	edge.MaxAmount01, _ = max01.Int(nil)

	r1 := new(big.Float).SetInt(edge.Reserve1)
	max10 := new(big.Float).Mul(r1, slippageFactor)
	edge.MaxAmount10, _ = max10.Int(nil)
}

func GetAmountOut(edge *PoolEdge, tokenIn common.Address, amountIn *big.Int) *big.Int {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return big.NewInt(0)
	}

	reserveIn := edge.GetReserveIn(tokenIn)
	reserveOut := edge.GetReserveOut(tokenIn)

	if reserveIn == nil || reserveOut == nil ||
		reserveIn.Sign() == 0 || reserveOut.Sign() == 0 {
		return big.NewInt(0)
	}

	feeBPS := big.NewInt(int64(edge.Fee))
	feeBase := big.NewInt(10000)
	feeMultiplier := new(big.Int).Sub(feeBase, feeBPS)

	amountInWithFee := new(big.Int).Mul(amountIn, feeMultiplier)

	numerator := new(big.Int).Mul(amountInWithFee, reserveOut)

	denominator := new(big.Int).Mul(reserveIn, feeBase)
	denominator.Add(denominator, amountInWithFee)

	amountOut := new(big.Int).Div(numerator, denominator)

	return amountOut
}

func GetAmountIn(edge *PoolEdge, tokenOut common.Address, amountOut *big.Int) *big.Int {
	if amountOut == nil || amountOut.Sign() <= 0 {
		return big.NewInt(0)
	}

	tokenIn := edge.GetTokenOut(tokenOut)
	reserveIn := edge.GetReserveIn(tokenIn)
	reserveOut := edge.GetReserveOut(tokenIn)

	if reserveIn == nil || reserveOut == nil ||
		reserveIn.Sign() == 0 || reserveOut.Cmp(amountOut) <= 0 {
		return nil
	}

	feeBase := big.NewInt(10000)
	numerator := new(big.Int).Mul(reserveIn, amountOut)
	numerator.Mul(numerator, feeBase)

	feeBPS := big.NewInt(int64(edge.Fee))
	feeMultiplier := new(big.Int).Sub(feeBase, feeBPS)
	reserveDiff := new(big.Int).Sub(reserveOut, amountOut)
	denominator := new(big.Int).Mul(reserveDiff, feeMultiplier)

	if denominator.Sign() == 0 {
		return nil
	}

	amountIn := new(big.Int).Div(numerator, denominator)
	amountIn.Add(amountIn, big.NewInt(1))

	return amountIn
}

func CalculatePriceImpact(edge *PoolEdge, tokenIn common.Address, amountIn *big.Int) int64 {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return 0
	}

	reserveIn := edge.GetReserveIn(tokenIn)
	if reserveIn == nil || reserveIn.Sign() == 0 {
		return 10000
	}

	impact := new(big.Float).Quo(
		new(big.Float).SetInt(amountIn),
		new(big.Float).SetInt(reserveIn),
	)
	impact.Mul(impact, big.NewFloat(10000))

	impactBPS, _ := impact.Int64()
	return impactBPS
}

func SimulatePathExecution(path *GraphPath, startAmount *big.Int) (*big.Int, *big.Int, bool) {
	if len(path.Nodes) < 2 {
		return big.NewInt(0), big.NewInt(0), false
	}

	currentAmount := new(big.Int).Set(startAmount)
	totalFees := big.NewInt(0)

	for i := 1; i < len(path.Nodes); i++ {
		edge := path.Nodes[i].Edge
		if edge == nil {
			return big.NewInt(0), big.NewInt(0), false
		}

		tokenIn := path.Nodes[i-1].Token

		maxAmount := edge.GetMaxAmount(tokenIn)
		if maxAmount != nil && currentAmount.Cmp(maxAmount) > 0 {

			return currentAmount, totalFees, false
		}

		amountOut := GetAmountOut(edge, tokenIn, currentAmount)
		if amountOut.Sign() == 0 {
			return big.NewInt(0), big.NewInt(0), false
		}

		feeAmount := new(big.Int).Mul(currentAmount, big.NewInt(int64(edge.Fee)))
		feeAmount.Div(feeAmount, big.NewInt(10000))
		totalFees.Add(totalFees, feeAmount)

		currentAmount = amountOut
	}

	return currentAmount, totalFees, true
}

func FindOptimalAmount(path *GraphPath, minAmount, maxAmount *big.Int) (*big.Int, *big.Int) {
	if maxAmount.Cmp(minAmount) <= 0 {
		return minAmount, big.NewInt(0)
	}

	low := new(big.Int).Set(minAmount)
	high := new(big.Int).Set(maxAmount)

	optimalAmount := new(big.Int).Set(minAmount)
	optimalProfit := big.NewInt(0)

	iterations := 50
	for i := 0; i < iterations && low.Cmp(high) < 0; i++ {

		mid := new(big.Int).Add(low, high)
		mid.Div(mid, big.NewInt(2))

		amountOut, _, ok := SimulatePathExecution(path, mid)
		if !ok {

			high = new(big.Int).Sub(mid, big.NewInt(1))
			continue
		}

		profit := new(big.Int).Sub(amountOut, mid)

		if profit.Cmp(optimalProfit) > 0 {
			optimalAmount.Set(mid)
			optimalProfit.Set(profit)
			low = new(big.Int).Add(mid, big.NewInt(1))
		} else {
			high = new(big.Int).Sub(mid, big.NewInt(1))
		}
	}

	return optimalAmount, optimalProfit
}
