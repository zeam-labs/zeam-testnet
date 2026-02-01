package discovery

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var (

	SelectorCreatePairV2     = crypto.Keccak256([]byte("createPair(address,address)"))[:4]
	SelectorCreatePoolV3     = crypto.Keccak256([]byte("createPool(address,address,uint24)"))[:4]
	SelectorCreatePoolVelo   = crypto.Keccak256([]byte("createPool(address,address,bool)"))[:4]

	SelectorSwapExactTokensForTokens     = crypto.Keccak256([]byte("swapExactTokensForTokens(uint256,uint256,address[],address,uint256)"))[:4]
	SelectorSwapTokensForExactTokens     = crypto.Keccak256([]byte("swapTokensForExactTokens(uint256,uint256,address[],address,uint256)"))[:4]
	SelectorSwapExactETHForTokens        = crypto.Keccak256([]byte("swapExactETHForTokens(uint256,address[],address,uint256)"))[:4]
	SelectorSwapTokensForExactETH        = crypto.Keccak256([]byte("swapTokensForExactETH(uint256,uint256,address[],address,uint256)"))[:4]
	SelectorSwapExactTokensForETH        = crypto.Keccak256([]byte("swapExactTokensForETH(uint256,uint256,address[],address,uint256)"))[:4]
	SelectorSwapETHForExactTokens        = crypto.Keccak256([]byte("swapETHForExactTokens(uint256,address[],address,uint256)"))[:4]

	SelectorSwapV2 = crypto.Keccak256([]byte("swap(uint256,uint256,address,bytes)"))[:4]

	SelectorExactInputSingle  = crypto.Keccak256([]byte("exactInputSingle((address,address,uint24,address,uint256,uint256,uint256,uint160))"))[:4]
	SelectorExactInput        = crypto.Keccak256([]byte("exactInput((bytes,address,uint256,uint256,uint256))"))[:4]
	SelectorExactOutputSingle = crypto.Keccak256([]byte("exactOutputSingle((address,address,uint24,address,uint256,uint256,uint256,uint160))"))[:4]
	SelectorExactOutput       = crypto.Keccak256([]byte("exactOutput((bytes,address,uint256,uint256,uint256))"))[:4]

	SelectorSwapV3 = crypto.Keccak256([]byte("swap(address,bool,int256,uint160,bytes)"))[:4]

	SelectorMulticall = crypto.Keccak256([]byte("multicall(bytes[])"))[:4]
	SelectorMulticallDeadline = crypto.Keccak256([]byte("multicall(uint256,bytes[])"))[:4]
)

type ParsedFactoryCall struct {
	Token0   common.Address
	Token1   common.Address
	Fee      uint32
	Stable   bool
	PoolType string
}

type ParsedSwapCall struct {
	Pool       common.Address
	TokenIn    common.Address
	TokenOut   common.Address
	AmountIn   *big.Int
	AmountOut  *big.Int
	Recipient  common.Address
	IsV3       bool
}

type CalldataParser struct {
	mu sync.RWMutex

	factories map[uint64]map[common.Address]*FactoryInfo

	routers map[uint64]map[common.Address]string

	OnFactoryCall func(chainID uint64, call *ParsedFactoryCall, tx *types.Transaction)
	OnSwapCall    func(chainID uint64, call *ParsedSwapCall, tx *types.Transaction)
}

func NewCalldataParser() *CalldataParser {
	cp := &CalldataParser{
		factories: make(map[uint64]map[common.Address]*FactoryInfo),
		routers:   make(map[uint64]map[common.Address]string),
	}

	for chainID, chainFactories := range Factories() {
		cp.factories[chainID] = make(map[common.Address]*FactoryInfo)
		for i := range chainFactories {
			cp.factories[chainID][chainFactories[i].Address] = &chainFactories[i]
		}
	}

	cp.initRouters()

	return cp
}

func (cp *CalldataParser) initRouters() {

	cp.routers[1] = map[common.Address]string{
		common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D"): "uniswap_v2_router",
		common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"): "uniswap_v3_router",
		common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45"): "uniswap_v3_router2",
		common.HexToAddress("0xd9e1cE17f2641f24aE83637ab66a2cca9C378B9F"): "sushiswap_router",
	}

	cp.routers[8453] = map[common.Address]string{
		common.HexToAddress("0x2626664c2603336E57B271c5C0b26F421741e481"): "uniswap_v3_router",
		common.HexToAddress("0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43"): "aerodrome_router",
		common.HexToAddress("0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24"): "uniswap_v2_router",
	}

	cp.routers[10] = map[common.Address]string{
		common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"): "uniswap_v3_router",
		common.HexToAddress("0xa062aE8A9c5e11aaA026fc2670B0D65cCc8B2858"): "velodrome_router",
	}

	cp.routers[42161] = map[common.Address]string{
		common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"): "uniswap_v3_router",
		common.HexToAddress("0x1b02dA8Cb0d097eB8D57A175b88c7D8b47997506"): "sushiswap_router",
		common.HexToAddress("0xc873fEcbd354f5A56E00E710B90EF4201db2448d"): "camelot_router",
	}
}

func (cp *CalldataParser) ProcessTransaction(chainID uint64, tx *types.Transaction) {
	if tx.To() == nil {
		return
	}

	data := tx.Data()
	if len(data) < 4 {
		return
	}

	to := *tx.To()
	selector := data[:4]

	if factory, ok := cp.factories[chainID][to]; ok {
		call := cp.parseFactoryCalldata(factory, data)
		if call != nil && cp.OnFactoryCall != nil {
			cp.OnFactoryCall(chainID, call, tx)
		}
		return
	}

	if routerType, ok := cp.routers[chainID][to]; ok {
		calls := cp.parseRouterCalldata(routerType, selector, data)
		for _, call := range calls {
			if cp.OnSwapCall != nil {
				cp.OnSwapCall(chainID, call, tx)
			}
		}
		return
	}

}

func (cp *CalldataParser) parseFactoryCalldata(factory *FactoryInfo, data []byte) *ParsedFactoryCall {
	if len(data) < 68 {
		return nil
	}

	selector := data[:4]

	if bytesEqual(selector, SelectorCreatePairV2) {
		return &ParsedFactoryCall{
			Token0:   common.BytesToAddress(data[16:36]),
			Token1:   common.BytesToAddress(data[48:68]),
			PoolType: "v2",
		}
	}

	if bytesEqual(selector, SelectorCreatePoolV3) && len(data) >= 100 {
		return &ParsedFactoryCall{
			Token0:   common.BytesToAddress(data[16:36]),
			Token1:   common.BytesToAddress(data[48:68]),
			Fee:      uint32(binary.BigEndian.Uint32(append([]byte{0}, data[97:100]...))),
			PoolType: "v3",
		}
	}

	if bytesEqual(selector, SelectorCreatePoolVelo) && len(data) >= 100 {
		return &ParsedFactoryCall{
			Token0:   common.BytesToAddress(data[16:36]),
			Token1:   common.BytesToAddress(data[48:68]),
			Stable:   data[99] == 1,
			PoolType: "velo",
		}
	}

	return nil
}

func (cp *CalldataParser) parseRouterCalldata(routerType string, selector, data []byte) []*ParsedSwapCall {
	var calls []*ParsedSwapCall

	if bytesEqual(selector, SelectorMulticall) || bytesEqual(selector, SelectorMulticallDeadline) {
		innerCalls := cp.unwrapMulticall(data)
		for _, innerData := range innerCalls {
			if len(innerData) >= 4 {
				innerCalls := cp.parseRouterCalldata(routerType, innerData[:4], innerData)
				calls = append(calls, innerCalls...)
			}
		}
		return calls
	}

	if bytesEqual(selector, SelectorSwapExactTokensForTokens) ||
		bytesEqual(selector, SelectorSwapTokensForExactTokens) {
		call := cp.parseV2SwapCalldata(data)
		if call != nil {
			calls = append(calls, call)
		}
		return calls
	}

	if bytesEqual(selector, SelectorExactInputSingle) {
		call := cp.parseV3ExactInputSingle(data)
		if call != nil {
			calls = append(calls, call)
		}
		return calls
	}

	if bytesEqual(selector, SelectorExactOutputSingle) {
		call := cp.parseV3ExactOutputSingle(data)
		if call != nil {
			calls = append(calls, call)
		}
		return calls
	}

	return calls
}

func (cp *CalldataParser) parseV2SwapCalldata(data []byte) *ParsedSwapCall {

	if len(data) < 164 {
		return nil
	}

	amountIn := new(big.Int).SetBytes(data[4:36])
	amountOutMin := new(big.Int).SetBytes(data[36:68])
	recipient := common.BytesToAddress(data[100:132])

	pathOffset := int(binary.BigEndian.Uint64(data[60:68])) + 4
	if pathOffset >= len(data) {
		return nil
	}

	pathLen := int(binary.BigEndian.Uint64(data[pathOffset+24 : pathOffset+32]))
	if pathLen < 2 || pathOffset+32+pathLen*32 > len(data) {
		return nil
	}

	tokenIn := common.BytesToAddress(data[pathOffset+32+12 : pathOffset+32+32])
	tokenOut := common.BytesToAddress(data[pathOffset+32+(pathLen-1)*32+12 : pathOffset+32+pathLen*32])

	return &ParsedSwapCall{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  amountIn,
		AmountOut: amountOutMin,
		Recipient: recipient,
		IsV3:      false,
	}
}

func (cp *CalldataParser) parseV3ExactInputSingle(data []byte) *ParsedSwapCall {

	if len(data) < 260 {
		return nil
	}

	offset := 36

	tokenIn := common.BytesToAddress(data[offset+12 : offset+32])
	tokenOut := common.BytesToAddress(data[offset+44 : offset+64])

	recipient := common.BytesToAddress(data[offset+108 : offset+128])

	amountIn := new(big.Int).SetBytes(data[offset+160 : offset+192])
	amountOutMin := new(big.Int).SetBytes(data[offset+192 : offset+224])

	return &ParsedSwapCall{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  amountIn,
		AmountOut: amountOutMin,
		Recipient: recipient,
		IsV3:      true,
	}
}

func (cp *CalldataParser) parseV3ExactOutputSingle(data []byte) *ParsedSwapCall {

	if len(data) < 260 {
		return nil
	}

	offset := 36
	tokenIn := common.BytesToAddress(data[offset+12 : offset+32])
	tokenOut := common.BytesToAddress(data[offset+44 : offset+64])
	recipient := common.BytesToAddress(data[offset+108 : offset+128])
	amountOut := new(big.Int).SetBytes(data[offset+160 : offset+192])
	amountInMax := new(big.Int).SetBytes(data[offset+192 : offset+224])

	return &ParsedSwapCall{
		TokenIn:   tokenIn,
		TokenOut:  tokenOut,
		AmountIn:  amountInMax,
		AmountOut: amountOut,
		Recipient: recipient,
		IsV3:      true,
	}
}

func (cp *CalldataParser) unwrapMulticall(data []byte) [][]byte {
	var calls [][]byte

	if len(data) < 68 {
		return calls
	}

	var arrayOffset int
	if bytesEqual(data[:4], SelectorMulticallDeadline) {

		arrayOffset = int(binary.BigEndian.Uint64(data[60:68])) + 4
	} else {

		arrayOffset = int(binary.BigEndian.Uint64(data[28:36])) + 4
	}

	if arrayOffset >= len(data) {
		return calls
	}

	arrayLen := int(binary.BigEndian.Uint64(data[arrayOffset+24 : arrayOffset+32]))
	if arrayLen > 20 {
		return calls
	}

	for i := 0; i < arrayLen; i++ {

		offsetOffset := arrayOffset + 32 + i*32
		if offsetOffset+32 > len(data) {
			break
		}

		elementOffset := int(binary.BigEndian.Uint64(data[offsetOffset+24:offsetOffset+32])) + arrayOffset + 32
		if elementOffset >= len(data) {
			break
		}

		if elementOffset+32 > len(data) {
			break
		}
		elementLen := int(binary.BigEndian.Uint64(data[elementOffset+24 : elementOffset+32]))
		if elementLen > 10000 {
			break
		}

		elementStart := elementOffset + 32
		if elementStart+elementLen > len(data) {
			break
		}

		calls = append(calls, data[elementStart:elementStart+elementLen])
	}

	return calls
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type GossipPoolDiscovery struct {
	parser   *CalldataParser
	registry *PoolRegistry

	mu             sync.RWMutex
	poolsDiscovered uint64
	swapsSeen       uint64
	lastDiscovery   time.Time
}

func NewGossipPoolDiscovery(registry *PoolRegistry) *GossipPoolDiscovery {
	gpd := &GossipPoolDiscovery{
		parser:   NewCalldataParser(),
		registry: registry,
	}

	gpd.parser.OnFactoryCall = gpd.onFactoryCall
	gpd.parser.OnSwapCall = gpd.onSwapCall

	return gpd
}

func (gpd *GossipPoolDiscovery) ProcessTransaction(chainID uint64, tx *types.Transaction) {
	gpd.parser.ProcessTransaction(chainID, tx)
}

func (gpd *GossipPoolDiscovery) onFactoryCall(chainID uint64, call *ParsedFactoryCall, tx *types.Transaction) {

	gpd.mu.Lock()
	gpd.poolsDiscovered++
	gpd.lastDiscovery = time.Now()
	gpd.mu.Unlock()

	fmt.Printf("[Gossip-Discovery] Factory call detected on chain %d: %s/%s (%s)\n",
		chainID, call.Token0.Hex()[:10], call.Token1.Hex()[:10], call.PoolType)
}

func (gpd *GossipPoolDiscovery) onSwapCall(chainID uint64, call *ParsedSwapCall, tx *types.Transaction) {
	gpd.mu.Lock()
	gpd.swapsSeen++
	gpd.mu.Unlock()

}

func (gpd *GossipPoolDiscovery) Stats() (pools, swaps uint64, lastDiscovery time.Time) {
	gpd.mu.RLock()
	defer gpd.mu.RUnlock()
	return gpd.poolsDiscovered, gpd.swapsSeen, gpd.lastDiscovery
}
