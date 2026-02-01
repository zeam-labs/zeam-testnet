package dex

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type DEXType string

const (
	DEXUniswapV2  DEXType = "uniswap_v2"
	DEXUniswapV3  DEXType = "uniswap_v3"
	DEXCurve      DEXType = "curve"
	DEXAerodrome  DEXType = "aerodrome"
	DEXVelodrome  DEXType = "velodrome"
	DEXSushiswap  DEXType = "sushiswap"
)

type PoolInfo struct {
	Address     common.Address
	ChainID     uint64
	DEX         DEXType
	Token0      common.Address
	Token1      common.Address
	Fee         uint32
	TickSpacing int
	Stable      bool
}

type SwapQuote struct {
	Pool         *PoolInfo
	TokenIn      common.Address
	TokenOut     common.Address
	AmountIn     *big.Int
	AmountOut    *big.Int
	PriceImpact  *big.Float
	Fee          *big.Int
	GasEstimate  uint64
}

type DEXRouter interface {

	GetQuote(pool *PoolInfo, tokenIn common.Address, amountIn *big.Int) (*SwapQuote, error)

	GetAmountOut(pool *PoolInfo, tokenIn common.Address, amountIn *big.Int, reserveIn, reserveOut *big.Int) (*big.Int, error)

	GetAmountIn(pool *PoolInfo, tokenOut common.Address, amountOut *big.Int, reserveIn, reserveOut *big.Int) (*big.Int, error)

	GetPrice(pool *PoolInfo, reserveIn, reserveOut *big.Int) *big.Float

	ParseSwapEvent(pool *PoolInfo, data []byte) (*SwapEventData, error)

	BuildSwapCalldata(quote *SwapQuote, recipient common.Address, deadline uint64) ([]byte, error)

	GetRouterAddress(chainID uint64) common.Address
}

type SwapEventData struct {
	Sender     common.Address
	Recipient  common.Address
	Amount0In  *big.Int
	Amount1In  *big.Int
	Amount0Out *big.Int
	Amount1Out *big.Int
	SqrtPriceX96 *big.Int
	Liquidity    *big.Int
	Tick         int
}

type Registry struct {
	routers map[DEXType]DEXRouter
	pools   map[common.Address]*PoolInfo
}

func NewRegistry() *Registry {
	return &Registry{
		routers: make(map[DEXType]DEXRouter),
		pools:   make(map[common.Address]*PoolInfo),
	}
}

func (r *Registry) RegisterRouter(dexType DEXType, router DEXRouter) {
	r.routers[dexType] = router
}

func (r *Registry) RegisterPool(pool *PoolInfo) {
	r.pools[pool.Address] = pool
}

func (r *Registry) GetRouter(dexType DEXType) (DEXRouter, bool) {
	router, ok := r.routers[dexType]
	return router, ok
}

func (r *Registry) GetPool(address common.Address) (*PoolInfo, bool) {
	pool, ok := r.pools[address]
	return pool, ok
}

func (r *Registry) GetQuote(poolAddr common.Address, tokenIn common.Address, amountIn *big.Int) (*SwapQuote, error) {
	pool, ok := r.pools[poolAddr]
	if !ok {
		return nil, fmt.Errorf("pool not found: %s", poolAddr.Hex())
	}

	router, ok := r.routers[pool.DEX]
	if !ok {
		return nil, fmt.Errorf("router not found for DEX: %s", pool.DEX)
	}

	return router.GetQuote(pool, tokenIn, amountIn)
}

var (

	Big0    = big.NewInt(0)
	Big1    = big.NewInt(1)
	Big2    = big.NewInt(2)
	Big10   = big.NewInt(10)
	Big997  = big.NewInt(997)
	Big1000 = big.NewInt(1000)

	Q96  = new(big.Int).Lsh(big.NewInt(1), 96)
	Q192 = new(big.Int).Lsh(big.NewInt(1), 192)
)

func MulDiv(a, b, c *big.Int) *big.Int {
	if c.Sign() == 0 {
		return Big0
	}
	result := new(big.Int).Mul(a, b)
	return result.Div(result, c)
}

func SqrtBigInt(n *big.Int) *big.Int {
	if n.Sign() <= 0 {
		return Big0
	}

	x := new(big.Int).Set(n)
	y := new(big.Int).Add(x, Big1)
	y.Div(y, Big2)

	for y.Cmp(x) < 0 {
		x.Set(y)
		y.Div(n, x)
		y.Add(y, x)
		y.Div(y, Big2)
	}

	return x
}

func KnownPools() map[uint64][]*PoolInfo {
	return map[uint64][]*PoolInfo{

		1: {

			{
				Address:     common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640"),
				ChainID:     1,
				DEX:         DEXUniswapV3,
				Token0:      common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
				Token1:      common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
				Fee:         5,
				TickSpacing: 10,
			},

			{
				Address:     common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
				ChainID:     1,
				DEX:         DEXUniswapV3,
				Token0:      common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
				Token1:      common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
				Fee:         30,
				TickSpacing: 60,
			},

			{
				Address: common.HexToAddress("0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc"),
				ChainID: 1,
				DEX:     DEXUniswapV2,
				Token0:  common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
				Token1:  common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
				Fee:     30,
			},
		},

		8453: {

			{
				Address:     common.HexToAddress("0xd0b53D9277642d899DF5C87A3966A349A798F224"),
				ChainID:     8453,
				DEX:         DEXUniswapV3,
				Token0:      common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
				Token1:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
				Fee:         5,
				TickSpacing: 10,
			},

			{
				Address: common.HexToAddress("0xcDAC0d6c6C59727a65F871236188350531885C43"),
				ChainID: 8453,
				DEX:     DEXAerodrome,
				Token0:  common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
				Token1:  common.HexToAddress("0x4200000000000000000000000000000000000006"),
				Fee:     5,
				Stable:  false,
			},
		},

		10: {

			{
				Address:     common.HexToAddress("0x85149247691df622eaF1a8Bd0CaFd40BC45154a9"),
				ChainID:     10,
				DEX:         DEXUniswapV3,
				Token0:      common.HexToAddress("0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85"),
				Token1:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
				Fee:         5,
				TickSpacing: 10,
			},

			{
				Address: common.HexToAddress("0x0493Bf8b6DBB7F8E30BcDCf0e54C9F8C2E5B2DfB"),
				ChainID: 10,
				DEX:     DEXVelodrome,
				Token0:  common.HexToAddress("0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85"),
				Token1:  common.HexToAddress("0x4200000000000000000000000000000000000006"),
				Fee:     5,
				Stable:  false,
			},
		},
	}
}

func RouterAddresses() map[uint64]map[DEXType]common.Address {
	return map[uint64]map[DEXType]common.Address{

		1: {
			DEXUniswapV2: common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D"),
			DEXUniswapV3: common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"),
			DEXCurve:     common.HexToAddress("0x99a58482BD75cbab83b27EC03CA68fF489b5788f"),
		},

		8453: {
			DEXUniswapV2: common.HexToAddress("0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24"),
			DEXUniswapV3: common.HexToAddress("0x2626664c2603336E57B271c5C0b26F421741e481"),
			DEXAerodrome: common.HexToAddress("0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43"),
		},

		10: {
			DEXUniswapV2: common.HexToAddress("0x4A7b5Da61326A6379179b40d00F57E5bbDC962c2"),
			DEXUniswapV3: common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"),
			DEXVelodrome: common.HexToAddress("0xa062aE8A9c5e11aaA026fc2670B0D65cCc8B2858"),
		},
	}
}

var (

	SelectorSwapExactTokensForTokens     = []byte{0x38, 0xed, 0x17, 0x39}
	SelectorSwapTokensForExactTokens     = []byte{0x88, 0x03, 0xdb, 0xee}
	SelectorSwapExactETHForTokens        = []byte{0x7f, 0xf3, 0x6a, 0xb5}
	SelectorSwapTokensForExactETH        = []byte{0x4a, 0x25, 0xd9, 0x4a}
	SelectorSwapExactTokensForETH        = []byte{0x18, 0xcb, 0xaf, 0xe5}
	SelectorSwapETHForExactTokens        = []byte{0xfb, 0x3b, 0xdb, 0x41}

	SelectorExactInputSingle  = []byte{0x41, 0x4b, 0xf3, 0x89}
	SelectorExactInput        = []byte{0xc0, 0x4b, 0x8d, 0x59}
	SelectorExactOutputSingle = []byte{0xdb, 0x3e, 0x21, 0x98}
	SelectorExactOutput       = []byte{0xf2, 0x8c, 0x02, 0x98}

	SelectorExecute = []byte{0x36, 0x93, 0xd8, 0xa4}
)

func MatchesSelector(calldata []byte, selector []byte) bool {
	if len(calldata) < 4 {
		return false
	}
	return calldata[0] == selector[0] && calldata[1] == selector[1] &&
		calldata[2] == selector[2] && calldata[3] == selector[3]
}

func DetectDEXFromCalldata(calldata []byte) (DEXType, bool) {
	if len(calldata) < 4 {
		return "", false
	}

	if MatchesSelector(calldata, SelectorSwapExactTokensForTokens) ||
		MatchesSelector(calldata, SelectorSwapTokensForExactTokens) ||
		MatchesSelector(calldata, SelectorSwapExactETHForTokens) ||
		MatchesSelector(calldata, SelectorSwapTokensForExactETH) ||
		MatchesSelector(calldata, SelectorSwapExactTokensForETH) ||
		MatchesSelector(calldata, SelectorSwapETHForExactTokens) {
		return DEXUniswapV2, true
	}

	if MatchesSelector(calldata, SelectorExactInputSingle) ||
		MatchesSelector(calldata, SelectorExactInput) ||
		MatchesSelector(calldata, SelectorExactOutputSingle) ||
		MatchesSelector(calldata, SelectorExactOutput) {
		return DEXUniswapV3, true
	}

	return "", false
}
