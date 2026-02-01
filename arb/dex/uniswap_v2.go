package dex

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type UniswapV2Router struct {
	routerAddresses map[uint64]common.Address
}

func NewUniswapV2Router() *UniswapV2Router {
	return &UniswapV2Router{
		routerAddresses: map[uint64]common.Address{
			1:     common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D"),
			8453:  common.HexToAddress("0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24"),
			10:    common.HexToAddress("0x4A7b5Da61326A6379179b40d00F57E5bbDC962c2"),
			11155111: common.HexToAddress("0xC532a74256D3Db42D0Bf7a0400fEFDbad7694008"),
		},
	}
}

func (r *UniswapV2Router) GetQuote(pool *PoolInfo, tokenIn common.Address, amountIn *big.Int) (*SwapQuote, error) {

	return nil, fmt.Errorf("GetQuote requires on-chain reserves; use GetAmountOut with known reserves")
}

func (r *UniswapV2Router) GetAmountOut(pool *PoolInfo, tokenIn common.Address, amountIn *big.Int, reserveIn, reserveOut *big.Int) (*big.Int, error) {
	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, fmt.Errorf("invalid amountIn")
	}
	if reserveIn == nil || reserveIn.Sign() <= 0 {
		return nil, fmt.Errorf("invalid reserveIn")
	}
	if reserveOut == nil || reserveOut.Sign() <= 0 {
		return nil, fmt.Errorf("invalid reserveOut")
	}

	amountInWithFee := new(big.Int).Mul(amountIn, Big997)

	numerator := new(big.Int).Mul(amountInWithFee, reserveOut)

	denominator := new(big.Int).Mul(reserveIn, Big1000)
	denominator.Add(denominator, amountInWithFee)

	amountOut := new(big.Int).Div(numerator, denominator)

	return amountOut, nil
}

func (r *UniswapV2Router) GetAmountIn(pool *PoolInfo, tokenOut common.Address, amountOut *big.Int, reserveIn, reserveOut *big.Int) (*big.Int, error) {
	if amountOut == nil || amountOut.Sign() <= 0 {
		return nil, fmt.Errorf("invalid amountOut")
	}
	if reserveIn == nil || reserveIn.Sign() <= 0 {
		return nil, fmt.Errorf("invalid reserveIn")
	}
	if reserveOut == nil || reserveOut.Sign() <= 0 {
		return nil, fmt.Errorf("invalid reserveOut")
	}
	if amountOut.Cmp(reserveOut) >= 0 {
		return nil, fmt.Errorf("amountOut exceeds reserves")
	}

	numerator := new(big.Int).Mul(reserveIn, amountOut)
	numerator.Mul(numerator, Big1000)

	denominator := new(big.Int).Sub(reserveOut, amountOut)
	denominator.Mul(denominator, Big997)

	amountIn := new(big.Int).Div(numerator, denominator)
	amountIn.Add(amountIn, Big1)

	return amountIn, nil
}

func (r *UniswapV2Router) GetPrice(pool *PoolInfo, reserveIn, reserveOut *big.Int) *big.Float {
	if reserveIn == nil || reserveIn.Sign() <= 0 {
		return new(big.Float)
	}

	price := new(big.Float).SetInt(reserveOut)
	price.Quo(price, new(big.Float).SetInt(reserveIn))
	return price
}

func (r *UniswapV2Router) ParseSwapEvent(pool *PoolInfo, data []byte) (*SwapEventData, error) {
	if len(data) < 128 {
		return nil, fmt.Errorf("data too short for V2 swap event")
	}

	event := &SwapEventData{
		Amount0In:  new(big.Int).SetBytes(data[0:32]),
		Amount1In:  new(big.Int).SetBytes(data[32:64]),
		Amount0Out: new(big.Int).SetBytes(data[64:96]),
		Amount1Out: new(big.Int).SetBytes(data[96:128]),
	}

	return event, nil
}

func (r *UniswapV2Router) BuildSwapCalldata(quote *SwapQuote, recipient common.Address, deadline uint64) ([]byte, error) {

	calldata := make([]byte, 4+32*5+64)

	copy(calldata[0:4], SelectorSwapExactTokensForTokens)

	amountInBytes := quote.AmountIn.Bytes()
	copy(calldata[4+32-len(amountInBytes):4+32], amountInBytes)

	minOut := new(big.Int).Mul(quote.AmountOut, big.NewInt(995))
	minOut.Div(minOut, big.NewInt(1000))
	minOutBytes := minOut.Bytes()
	copy(calldata[4+64-len(minOutBytes):4+64], minOutBytes)

	pathOffset := big.NewInt(160)
	pathOffsetBytes := pathOffset.Bytes()
	copy(calldata[4+96-len(pathOffsetBytes):4+96], pathOffsetBytes)

	copy(calldata[4+96+12:4+128], recipient.Bytes())

	deadlineBytes := new(big.Int).SetUint64(deadline).Bytes()
	copy(calldata[4+160-len(deadlineBytes):4+160], deadlineBytes)

	calldata[4+160+31] = 2

	copy(calldata[4+192+12:4+224], quote.TokenIn.Bytes())

	copy(calldata[4+224+12:4+256], quote.TokenOut.Bytes())

	return calldata[:4+256], nil
}

func (r *UniswapV2Router) GetRouterAddress(chainID uint64) common.Address {
	if addr, ok := r.routerAddresses[chainID]; ok {
		return addr
	}
	return common.Address{}
}

func CalculateOptimalArbitrageV2(
	reserve0A, reserve1A *big.Int,
	reserve0B, reserve1B *big.Int,
) (*big.Int, *big.Int, error) {

	priceA := new(big.Float).Quo(
		new(big.Float).SetInt(reserve1A),
		new(big.Float).SetInt(reserve0A),
	)
	priceB := new(big.Float).Quo(
		new(big.Float).SetInt(reserve0B),
		new(big.Float).SetInt(reserve1B),
	)

	if priceB.Cmp(priceA) <= 0 {
		return nil, nil, fmt.Errorf("no arbitrage opportunity: priceB <= priceA")
	}

	product := new(big.Int).Mul(reserve0A, reserve1B)
	sqrtProduct := SqrtBigInt(product)

	optimalIn := new(big.Int).Sub(sqrtProduct, reserve0A)

	if optimalIn.Sign() <= 0 {
		return nil, nil, fmt.Errorf("no profitable arbitrage")
	}

	maxIn := new(big.Int).Div(reserve0A, big.NewInt(10))
	if optimalIn.Cmp(maxIn) > 0 {
		optimalIn.Set(maxIn)
	}

	router := NewUniswapV2Router()

	amountOutA, err := router.GetAmountOut(nil, common.Address{}, optimalIn, reserve0A, reserve1A)
	if err != nil {
		return nil, nil, err
	}

	amountOutB, err := router.GetAmountOut(nil, common.Address{}, amountOutA, reserve1B, reserve0B)
	if err != nil {
		return nil, nil, err
	}

	profit := new(big.Int).Sub(amountOutB, optimalIn)

	if profit.Sign() <= 0 {
		return nil, nil, fmt.Errorf("no profit after fees")
	}

	return optimalIn, profit, nil
}

func GetReservesFromSync(data []byte) (*big.Int, *big.Int, error) {
	if len(data) < 64 {
		return nil, nil, fmt.Errorf("data too short for Sync event")
	}

	reserve0 := new(big.Int).SetBytes(data[0:32])
	reserve1 := new(big.Int).SetBytes(data[32:64])

	return reserve0, reserve1, nil
}

func ParseV2SwapFromCalldata(calldata []byte) (*V2SwapParams, error) {
	if len(calldata) < 4 {
		return nil, fmt.Errorf("calldata too short")
	}

	params := &V2SwapParams{}

	if MatchesSelector(calldata, SelectorSwapExactTokensForTokens) {
		params.ExactInput = true

		if len(calldata) < 4+160 {
			return nil, fmt.Errorf("calldata too short for swapExactTokensForTokens")
		}
		params.AmountIn = new(big.Int).SetBytes(calldata[4:36])
		params.AmountOutMin = new(big.Int).SetBytes(calldata[36:68])

	} else if MatchesSelector(calldata, SelectorSwapTokensForExactTokens) {
		params.ExactInput = false

		if len(calldata) < 4+160 {
			return nil, fmt.Errorf("calldata too short for swapTokensForExactTokens")
		}
		params.AmountOut = new(big.Int).SetBytes(calldata[4:36])
		params.AmountInMax = new(big.Int).SetBytes(calldata[36:68])
	}

	return params, nil
}

type V2SwapParams struct {
	ExactInput   bool
	AmountIn     *big.Int
	AmountOutMin *big.Int
	AmountOut    *big.Int
	AmountInMax  *big.Int
	Path         []common.Address
	Recipient    common.Address
	Deadline     uint64
}
