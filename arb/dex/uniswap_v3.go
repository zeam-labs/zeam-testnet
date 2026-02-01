package dex

import (
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type UniswapV3Router struct {
	routerAddresses map[uint64]common.Address
}

func NewUniswapV3Router() *UniswapV3Router {
	return &UniswapV3Router{
		routerAddresses: map[uint64]common.Address{
			1:        common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"),
			8453:     common.HexToAddress("0x2626664c2603336E57B271c5C0b26F421741e481"),
			10:       common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"),
			11155111: common.HexToAddress("0x3bFA4769FB09eefC5a80d6E87c3B9C650f7Ae48E"),
		},
	}
}

var (
	MinTick     = -887272
	MaxTick     = 887272
	MinSqrtRatio = new(big.Int).SetUint64(4295128739)
	MaxSqrtRatio, _ = new(big.Int).SetString("1461446703485210103287273052203988822378723970342", 10)
)

func (r *UniswapV3Router) GetQuote(pool *PoolInfo, tokenIn common.Address, amountIn *big.Int) (*SwapQuote, error) {

	return nil, fmt.Errorf("GetQuote requires on-chain state; use simulation or quoter contract")
}

func (r *UniswapV3Router) GetAmountOut(pool *PoolInfo, tokenIn common.Address, amountIn *big.Int, reserveIn, reserveOut *big.Int) (*big.Int, error) {

	if amountIn == nil || amountIn.Sign() <= 0 {
		return nil, fmt.Errorf("invalid amountIn")
	}

	feeBps := pool.Fee
	if feeBps == 0 {
		feeBps = 30
	}

	feeMultiplier := big.NewInt(10000 - int64(feeBps))
	amountInAfterFee := new(big.Int).Mul(amountIn, feeMultiplier)
	amountInAfterFee.Div(amountInAfterFee, big.NewInt(10000))

	numerator := new(big.Int).Mul(amountInAfterFee, reserveOut)
	denominator := new(big.Int).Add(reserveIn, amountInAfterFee)
	amountOut := new(big.Int).Div(numerator, denominator)

	return amountOut, nil
}

func (r *UniswapV3Router) GetAmountIn(pool *PoolInfo, tokenOut common.Address, amountOut *big.Int, reserveIn, reserveOut *big.Int) (*big.Int, error) {
	if amountOut == nil || amountOut.Sign() <= 0 {
		return nil, fmt.Errorf("invalid amountOut")
	}
	if amountOut.Cmp(reserveOut) >= 0 {
		return nil, fmt.Errorf("amountOut exceeds liquidity")
	}

	feeBps := pool.Fee
	if feeBps == 0 {
		feeBps = 30
	}

	numerator := new(big.Int).Mul(reserveIn, amountOut)
	denominator := new(big.Int).Sub(reserveOut, amountOut)
	amountInBeforeFee := new(big.Int).Div(numerator, denominator)
	amountInBeforeFee.Add(amountInBeforeFee, Big1)

	feeMultiplier := big.NewInt(10000)
	feeDivisor := big.NewInt(10000 - int64(feeBps))
	amountIn := new(big.Int).Mul(amountInBeforeFee, feeMultiplier)
	amountIn.Div(amountIn, feeDivisor)
	amountIn.Add(amountIn, Big1)

	return amountIn, nil
}

func (r *UniswapV3Router) GetPrice(pool *PoolInfo, sqrtPriceX96, _ *big.Int) *big.Float {

	if sqrtPriceX96 == nil || sqrtPriceX96.Sign() <= 0 {
		return new(big.Float)
	}

	sqrtPriceSquared := new(big.Int).Mul(sqrtPriceX96, sqrtPriceX96)

	price := new(big.Float).SetInt(sqrtPriceSquared)
	divisor := new(big.Float).SetInt(Q192)
	price.Quo(price, divisor)

	return price
}

func (r *UniswapV3Router) ParseSwapEvent(pool *PoolInfo, data []byte) (*SwapEventData, error) {
	if len(data) < 160 {
		return nil, fmt.Errorf("data too short for V3 swap event")
	}

	event := &SwapEventData{
		Amount0In:    new(big.Int).SetBytes(data[0:32]),
		Amount1In:    new(big.Int).SetBytes(data[32:64]),
		SqrtPriceX96: new(big.Int).SetBytes(data[64:96]),
		Liquidity:    new(big.Int).SetBytes(data[96:128]),
	}

	tickBytes := data[128:160]
	tickBig := new(big.Int).SetBytes(tickBytes)

	if tickBig.Bit(23) == 1 {

		tickBig.Sub(tickBig, new(big.Int).Lsh(Big1, 24))
	}
	event.Tick = int(tickBig.Int64())

	if event.Amount0In.Sign() < 0 {
		event.Amount0Out = new(big.Int).Neg(event.Amount0In)
		event.Amount0In = Big0
	} else {
		event.Amount0Out = Big0
	}
	if event.Amount1In.Sign() < 0 {
		event.Amount1Out = new(big.Int).Neg(event.Amount1In)
		event.Amount1In = Big0
	} else {
		event.Amount1Out = Big0
	}

	return event, nil
}

func (r *UniswapV3Router) BuildSwapCalldata(quote *SwapQuote, recipient common.Address, deadline uint64) ([]byte, error) {

	calldata := make([]byte, 4+32*8)

	copy(calldata[0:4], SelectorExactInputSingle)

	copy(calldata[4+12:4+32], quote.TokenIn.Bytes())

	copy(calldata[4+32+12:4+64], quote.TokenOut.Bytes())

	fee := quote.Pool.Fee
	if fee == 0 {
		fee = 3000
	}
	feeBytes := big.NewInt(int64(fee)).Bytes()
	copy(calldata[4+96-len(feeBytes):4+96], feeBytes)

	copy(calldata[4+96+12:4+128], recipient.Bytes())

	deadlineBytes := new(big.Int).SetUint64(deadline).Bytes()
	copy(calldata[4+160-len(deadlineBytes):4+160], deadlineBytes)

	amountInBytes := quote.AmountIn.Bytes()
	copy(calldata[4+192-len(amountInBytes):4+192], amountInBytes)

	minOut := new(big.Int).Mul(quote.AmountOut, big.NewInt(995))
	minOut.Div(minOut, big.NewInt(1000))
	minOutBytes := minOut.Bytes()
	copy(calldata[4+224-len(minOutBytes):4+224], minOutBytes)

	return calldata, nil
}

func (r *UniswapV3Router) GetRouterAddress(chainID uint64) common.Address {
	if addr, ok := r.routerAddresses[chainID]; ok {
		return addr
	}
	return common.Address{}
}

func TickToSqrtPriceX96(tick int) *big.Int {
	if tick < MinTick || tick > MaxTick {
		return nil
	}

	sqrtRatio := math.Pow(1.0001, float64(tick)/2.0)
	sqrtRatioX96 := sqrtRatio * math.Pow(2, 96)

	result := new(big.Int)
	result.SetString(fmt.Sprintf("%.0f", sqrtRatioX96), 10)

	return result
}

func SqrtPriceX96ToTick(sqrtPriceX96 *big.Int) int {
	if sqrtPriceX96 == nil || sqrtPriceX96.Sign() <= 0 {
		return 0
	}

	sqrtPriceFloat, _ := new(big.Float).SetInt(sqrtPriceX96).Float64()
	q96Float := math.Pow(2, 96)
	sqrtPrice := sqrtPriceFloat / q96Float

	tick := 2.0 * math.Log(sqrtPrice) / math.Log(1.0001)

	return int(tick)
}

func GetNextSqrtPriceFromInput(sqrtPriceX96, liquidity, amountIn *big.Int, zeroForOne bool) *big.Int {
	if sqrtPriceX96.Sign() <= 0 || liquidity.Sign() <= 0 {
		return nil
	}

	if zeroForOne {

		product := new(big.Int).Mul(amountIn, sqrtPriceX96)
		product.Div(product, Q96)

		denominator := new(big.Int).Add(liquidity, product)

		numerator := new(big.Int).Mul(sqrtPriceX96, liquidity)

		sqrtPriceNext := new(big.Int).Div(numerator, denominator)
		return sqrtPriceNext
	} else {

		delta := new(big.Int).Mul(amountIn, Q96)
		delta.Div(delta, liquidity)

		sqrtPriceNext := new(big.Int).Add(sqrtPriceX96, delta)
		return sqrtPriceNext
	}
}

func GetAmount0Delta(sqrtPriceAX96, sqrtPriceBX96, liquidity *big.Int) *big.Int {

	if sqrtPriceAX96.Cmp(sqrtPriceBX96) > 0 {
		sqrtPriceAX96, sqrtPriceBX96 = sqrtPriceBX96, sqrtPriceAX96
	}

	numerator := new(big.Int).Sub(sqrtPriceBX96, sqrtPriceAX96)
	numerator.Mul(numerator, liquidity)
	numerator.Mul(numerator, Q96)

	denominator := new(big.Int).Mul(sqrtPriceAX96, sqrtPriceBX96)

	amount0 := new(big.Int).Div(numerator, denominator)
	return amount0
}

func GetAmount1Delta(sqrtPriceAX96, sqrtPriceBX96, liquidity *big.Int) *big.Int {

	if sqrtPriceAX96.Cmp(sqrtPriceBX96) > 0 {
		sqrtPriceAX96, sqrtPriceBX96 = sqrtPriceBX96, sqrtPriceAX96
	}

	numerator := new(big.Int).Sub(sqrtPriceBX96, sqrtPriceAX96)
	numerator.Mul(numerator, liquidity)

	amount1 := new(big.Int).Div(numerator, Q96)
	return amount1
}

func ParseV3SwapFromCalldata(calldata []byte) (*V3SwapParams, error) {
	if len(calldata) < 4 {
		return nil, fmt.Errorf("calldata too short")
	}

	params := &V3SwapParams{}

	if MatchesSelector(calldata, SelectorExactInputSingle) {

		if len(calldata) < 4+256 {
			return nil, fmt.Errorf("calldata too short for exactInputSingle")
		}
		params.ExactInput = true
		params.TokenIn = common.BytesToAddress(calldata[4+12 : 4+32])
		params.TokenOut = common.BytesToAddress(calldata[4+32+12 : 4+64])
		params.Fee = uint32(new(big.Int).SetBytes(calldata[4+64:4+96]).Uint64())
		params.Recipient = common.BytesToAddress(calldata[4+96+12 : 4+128])
		params.AmountIn = new(big.Int).SetBytes(calldata[4+160 : 4+192])
		params.AmountOutMin = new(big.Int).SetBytes(calldata[4+192 : 4+224])

	} else if MatchesSelector(calldata, SelectorExactOutputSingle) {

		if len(calldata) < 4+256 {
			return nil, fmt.Errorf("calldata too short for exactOutputSingle")
		}
		params.ExactInput = false
		params.TokenIn = common.BytesToAddress(calldata[4+12 : 4+32])
		params.TokenOut = common.BytesToAddress(calldata[4+32+12 : 4+64])
		params.Fee = uint32(new(big.Int).SetBytes(calldata[4+64:4+96]).Uint64())
		params.Recipient = common.BytesToAddress(calldata[4+96+12 : 4+128])
		params.AmountOut = new(big.Int).SetBytes(calldata[4+160 : 4+192])
		params.AmountInMax = new(big.Int).SetBytes(calldata[4+192 : 4+224])
	}

	return params, nil
}

type V3SwapParams struct {
	ExactInput   bool
	TokenIn      common.Address
	TokenOut     common.Address
	Fee          uint32
	Recipient    common.Address
	Deadline     uint64
	AmountIn     *big.Int
	AmountOutMin *big.Int
	AmountOut    *big.Int
	AmountInMax  *big.Int
	SqrtPriceLimitX96 *big.Int
}
