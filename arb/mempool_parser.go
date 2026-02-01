package arb

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var (

	swapExactTokensForTokens     = mustDecodeSelector("0x38ed1739")
	swapTokensForExactTokens     = mustDecodeSelector("0x8803dbee")
	swapExactETHForTokens        = mustDecodeSelector("0x7ff36ab5")
	swapTokensForExactETH        = mustDecodeSelector("0x4a25d94a")
	swapExactTokensForETH        = mustDecodeSelector("0x18cbafe5")
	swapETHForExactTokens        = mustDecodeSelector("0xfb3bdb41")

	swapExactTokensForTokensFee  = mustDecodeSelector("0x5c11d795")
	swapExactTokensForETHFee     = mustDecodeSelector("0x791ac947")
	swapExactETHForTokensFee     = mustDecodeSelector("0xb6f9de95")

	exactInputSingle             = mustDecodeSelector("0x414bf389")
	exactInput                   = mustDecodeSelector("0xc04b8d59")
	exactOutputSingle            = mustDecodeSelector("0xdb3e2198")
	exactOutput                  = mustDecodeSelector("0xf28c0498")

	multicall                    = mustDecodeSelector("0xac9650d8")
	multicallV3                  = mustDecodeSelector("0x5ae401dc")
	execute                      = mustDecodeSelector("0x3593564c")

	swapExactTokensForTokensV2   = mustDecodeSelector("0x38ed1739")
	swapExactETHForTokensV2      = mustDecodeSelector("0x7ff36ab5")
	swapExactTokensForETHV2      = mustDecodeSelector("0x18cbafe5")
)

func mustDecodeSelector(s string) []byte {
	b, _ := hex.DecodeString(s[2:])
	return b
}

type PendingSwap struct {
	TxHash      common.Hash
	ChainID     uint64
	From        common.Address
	To          common.Address
	Router      string
	TokenIn     common.Address
	TokenOut    common.Address
	AmountIn    *big.Int
	AmountOut   *big.Int
	IsExactIn   bool
	DetectedAt  time.Time
	GasPrice    *big.Int
	GasLimit    uint64
	Nonce       uint64

	EstimatedPriceImpact float64
	PoolAddress          common.Address
}

type MempoolParser struct {
	mu sync.RWMutex

	routers map[uint64]map[common.Address]string

	pendingSwaps map[common.Hash]*PendingSwap

	txParsed      uint64
	swapsDetected uint64
	parseErrors   uint64

	OnSwapDetected func(swap *PendingSwap)
}

func NewMempoolParser() *MempoolParser {
	mp := &MempoolParser{
		routers:      make(map[uint64]map[common.Address]string),
		pendingSwaps: make(map[common.Hash]*PendingSwap),
	}

	mp.initRouters()

	return mp
}

func (mp *MempoolParser) initRouters() {

	mp.routers[ChainIDEthereum] = map[common.Address]string{
		common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D"): "uniswap_v2",
		common.HexToAddress("0xd9e1cE17f2641f24aE83637ab66a2cca9C378B9F"): "sushiswap",
		common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"): "uniswap_v3",
		common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45"): "uniswap_v3_router02",
		common.HexToAddress("0x3fC91A3afd70395Cd496C647d5a6CC9D4B2b7FAD"): "universal_router",
	}

	mp.routers[ChainIDBase] = map[common.Address]string{
		common.HexToAddress("0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24"): "uniswap_v2",
		common.HexToAddress("0x2626664c2603336E57B271c5C0b26F421741e481"): "uniswap_v3",
		common.HexToAddress("0x3fC91A3afd70395Cd496C647d5a6CC9D4B2b7FAD"): "universal_router",
		common.HexToAddress("0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43"): "aerodrome",
		common.HexToAddress("0x327Df1E6de05895d2ab08513aaDD9313Fe505d86"): "baseswap",
	}

	mp.routers[ChainIDOptimism] = map[common.Address]string{
		common.HexToAddress("0x4A7b5Da61326A6379179b40d00F57E5bbDC962c2"): "uniswap_v2",
		common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45"): "uniswap_v3",
		common.HexToAddress("0xCb1355ff08Ab38bBCE60111F1bb2B784bE25D7e8"): "universal_router",
		common.HexToAddress("0xa062aE8A9c5e11aaA026fc2670B0D65cCc8B2858"): "velodrome",
	}
}

func (mp *MempoolParser) ParseTransaction(chainID uint64, tx *types.Transaction) *PendingSwap {
	mp.mu.Lock()
	mp.txParsed++
	mp.mu.Unlock()

	to := tx.To()
	if to == nil {
		return nil
	}

	chainRouters := mp.routers[chainID]
	if chainRouters == nil {
		return nil
	}

	routerName, isRouter := chainRouters[*to]
	if !isRouter {
		return nil
	}

	data := tx.Data()
	if len(data) < 4 {
		return nil
	}

	swap := mp.parseCalldata(chainID, routerName, data)
	if swap == nil {
		return nil
	}

	swap.TxHash = tx.Hash()
	swap.ChainID = chainID
	swap.From = getSender(tx, chainID)
	swap.To = *to
	swap.Router = routerName
	swap.DetectedAt = time.Now()
	swap.GasPrice = tx.GasPrice()
	swap.GasLimit = tx.Gas()
	swap.Nonce = tx.Nonce()

	mp.mu.Lock()
	mp.pendingSwaps[swap.TxHash] = swap
	mp.swapsDetected++
	mp.mu.Unlock()

	if mp.OnSwapDetected != nil {
		mp.OnSwapDetected(swap)
	}

	return swap
}

func (mp *MempoolParser) parseCalldata(chainID uint64, router string, data []byte) *PendingSwap {
	if len(data) < 4 {
		return nil
	}

	selector := data[:4]

	if bytes.Equal(selector, swapExactTokensForTokens) ||
		bytes.Equal(selector, swapExactTokensForTokensFee) {
		return mp.parseV2SwapExactTokensForTokens(data)
	}

	if bytes.Equal(selector, swapExactETHForTokens) ||
		bytes.Equal(selector, swapExactETHForTokensFee) {
		return mp.parseV2SwapExactETHForTokens(data, chainID)
	}

	if bytes.Equal(selector, swapExactTokensForETH) ||
		bytes.Equal(selector, swapExactTokensForETHFee) {
		return mp.parseV2SwapExactTokensForETH(data, chainID)
	}

	if bytes.Equal(selector, exactInputSingle) {
		return mp.parseV3ExactInputSingle(data)
	}

	if bytes.Equal(selector, exactInput) {
		return mp.parseV3ExactInput(data)
	}

	if bytes.Equal(selector, multicall) || bytes.Equal(selector, multicallV3) {
		return mp.parseMulticall(data, chainID, router)
	}

	if bytes.Equal(selector, execute) {
		return mp.parseUniversalRouter(data, chainID)
	}

	return nil
}

func (mp *MempoolParser) parseV2SwapExactTokensForTokens(data []byte) *PendingSwap {
	if len(data) < 164 {
		return nil
	}

	amountIn := new(big.Int).SetBytes(data[4:36])
	amountOutMin := new(big.Int).SetBytes(data[36:68])

	pathOffset := new(big.Int).SetBytes(data[68:100]).Uint64()
	if pathOffset+32 > uint64(len(data)) {
		return nil
	}

	pathLen := new(big.Int).SetBytes(data[4+pathOffset : 4+pathOffset+32]).Uint64()
	if pathLen < 2 || 4+pathOffset+32+pathLen*32 > uint64(len(data)) {
		return nil
	}

	tokenIn := common.BytesToAddress(data[4+pathOffset+32 : 4+pathOffset+64])
	tokenOut := common.BytesToAddress(data[4+pathOffset+32+(pathLen-1)*32 : 4+pathOffset+64+(pathLen-1)*32])

	return &PendingSwap{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  amountIn,
		AmountOut: amountOutMin,
		IsExactIn: true,
	}
}

func (mp *MempoolParser) parseV2SwapExactETHForTokens(data []byte, chainID uint64) *PendingSwap {
	if len(data) < 132 {
		return nil
	}

	amountOutMin := new(big.Int).SetBytes(data[4:36])

	pathOffset := new(big.Int).SetBytes(data[36:68]).Uint64()
	if pathOffset+32 > uint64(len(data)) {
		return nil
	}

	pathLen := new(big.Int).SetBytes(data[4+pathOffset : 4+pathOffset+32]).Uint64()
	if pathLen < 2 {
		return nil
	}

	tokenOut := common.BytesToAddress(data[4+pathOffset+32+(pathLen-1)*32 : 4+pathOffset+64+(pathLen-1)*32])

	wethAddrs := WETHAddresses()
	tokenIn := wethAddrs[chainID]

	return &PendingSwap{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  nil,
		AmountOut: amountOutMin,
		IsExactIn: true,
	}
}

func (mp *MempoolParser) parseV2SwapExactTokensForETH(data []byte, chainID uint64) *PendingSwap {
	if len(data) < 164 {
		return nil
	}

	amountIn := new(big.Int).SetBytes(data[4:36])
	amountOutMin := new(big.Int).SetBytes(data[36:68])

	pathOffset := new(big.Int).SetBytes(data[68:100]).Uint64()
	if pathOffset+32 > uint64(len(data)) {
		return nil
	}

	pathLen := new(big.Int).SetBytes(data[4+pathOffset : 4+pathOffset+32]).Uint64()
	if pathLen < 2 {
		return nil
	}

	tokenIn := common.BytesToAddress(data[4+pathOffset+32 : 4+pathOffset+64])

	wethAddrs := WETHAddresses()
	tokenOut := wethAddrs[chainID]

	return &PendingSwap{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  amountIn,
		AmountOut: amountOutMin,
		IsExactIn: true,
	}
}

func (mp *MempoolParser) parseV3ExactInputSingle(data []byte) *PendingSwap {
	if len(data) < 196 {
		return nil
	}

	tokenIn := common.BytesToAddress(data[16:36])
	tokenOut := common.BytesToAddress(data[48:68])

	amountIn := new(big.Int).SetBytes(data[164:196])
	amountOutMin := new(big.Int).SetBytes(data[196:228])

	return &PendingSwap{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  amountIn,
		AmountOut: amountOutMin,
		IsExactIn: true,
	}
}

func (mp *MempoolParser) parseV3ExactInput(data []byte) *PendingSwap {
	if len(data) < 164 {
		return nil
	}

	pathOffset := new(big.Int).SetBytes(data[4:36]).Uint64()
	amountIn := new(big.Int).SetBytes(data[100:132])
	amountOutMin := new(big.Int).SetBytes(data[132:164])

	if pathOffset+32 > uint64(len(data)) {
		return nil
	}

	pathLen := new(big.Int).SetBytes(data[4+pathOffset : 4+pathOffset+32]).Uint64()
	if pathLen < 43 {
		return nil
	}

	tokenIn := common.BytesToAddress(data[4+pathOffset+32 : 4+pathOffset+52])

	tokenOut := common.BytesToAddress(data[4+pathOffset+32+pathLen-20 : 4+pathOffset+32+pathLen])

	return &PendingSwap{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  amountIn,
		AmountOut: amountOutMin,
		IsExactIn: true,
	}
}

func (mp *MempoolParser) parseMulticall(data []byte, chainID uint64, router string) *PendingSwap {

	for i := 4; i < len(data)-4; i++ {
		chunk := data[i : i+4]
		if bytes.Equal(chunk, exactInputSingle) ||
			bytes.Equal(chunk, exactInput) ||
			bytes.Equal(chunk, swapExactTokensForTokens) {

			return &PendingSwap{
				Router:    router,
				IsExactIn: true,
			}
		}
	}

	return nil
}

func (mp *MempoolParser) parseUniversalRouter(data []byte, chainID uint64) *PendingSwap {

	if len(data) > 100 {
		return &PendingSwap{
			Router:    "universal_router",
			ChainID:   chainID,
			IsExactIn: true,
		}
	}
	return nil
}

func getSender(tx *types.Transaction, chainID uint64) common.Address {
	signer := types.LatestSignerForChainID(big.NewInt(int64(chainID)))
	from, err := types.Sender(signer, tx)
	if err != nil {
		return common.Address{}
	}
	return from
}

func (mp *MempoolParser) GetPendingSwap(hash common.Hash) *PendingSwap {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.pendingSwaps[hash]
}

func (mp *MempoolParser) GetRecentSwaps(maxAge time.Duration) []*PendingSwap {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	cutoff := time.Now().Add(-maxAge)
	result := make([]*PendingSwap, 0)

	for _, swap := range mp.pendingSwaps {
		if swap.DetectedAt.After(cutoff) {
			result = append(result, swap)
		}
	}

	return result
}

func (mp *MempoolParser) CleanOldSwaps(maxAge time.Duration) int {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for hash, swap := range mp.pendingSwaps {
		if swap.DetectedAt.Before(cutoff) {
			delete(mp.pendingSwaps, hash)
			removed++
		}
	}

	return removed
}

func (mp *MempoolParser) Stats() map[string]interface{} {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	return map[string]interface{}{
		"tx_parsed":       mp.txParsed,
		"swaps_detected":  mp.swapsDetected,
		"parse_errors":    mp.parseErrors,
		"pending_swaps":   len(mp.pendingSwaps),
		"detection_rate":  float64(mp.swapsDetected) / float64(mp.txParsed+1),
	}
}

func (mp *MempoolParser) EstimatePriceImpact(swap *PendingSwap, poolReserves0, poolReserves1 *big.Int) float64 {
	if swap.AmountIn == nil || poolReserves0 == nil || poolReserves1 == nil {
		return 0
	}

	amountFloat := new(big.Float).SetInt(swap.AmountIn)
	reserveFloat := new(big.Float).SetInt(poolReserves0)

	if reserveFloat.Sign() == 0 {
		return 0
	}

	impact := new(big.Float).Quo(amountFloat, reserveFloat)
	impactFloat, _ := impact.Float64()

	return impactFloat * 10000
}

func ComputeOpportunityHash(swap *PendingSwap) [32]byte {
	data := make([]byte, 0, 128)
	data = append(data, swap.TxHash.Bytes()...)
	data = append(data, swap.TokenIn.Bytes()...)
	data = append(data, swap.TokenOut.Bytes()...)
	if swap.AmountIn != nil {
		data = append(data, swap.AmountIn.Bytes()...)
	}
	return crypto.Keccak256Hash(data)
}
