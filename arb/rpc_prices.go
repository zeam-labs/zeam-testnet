package arb

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"zeam/arb/dex"
)

type WSPriceFeed struct {
	mu sync.RWMutex

	endpoints map[uint64]string

	clients map[uint64]*rpc.Client
	ethClients map[uint64]*ethclient.Client

	priceFeed *PriceFeed

	dexRegistry *dex.Registry

	routers map[uint64]map[common.Address]dex.DEXType

	txSeen    map[uint64]uint64
	swapsSeen map[uint64]uint64

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func NewWSPriceFeed(endpoints map[uint64]string, priceFeed *PriceFeed) *WSPriceFeed {
	wsf := &WSPriceFeed{
		endpoints:   endpoints,
		clients:     make(map[uint64]*rpc.Client),
		ethClients:  make(map[uint64]*ethclient.Client),
		priceFeed:   priceFeed,
		dexRegistry: dex.NewRegistry(),
		routers:     make(map[uint64]map[common.Address]dex.DEXType),
		txSeen:      make(map[uint64]uint64),
		swapsSeen:   make(map[uint64]uint64),
		stopCh:      make(chan struct{}),
	}

	wsf.dexRegistry.RegisterRouter(dex.DEXUniswapV2, dex.NewUniswapV2Router())
	wsf.dexRegistry.RegisterRouter(dex.DEXUniswapV3, dex.NewUniswapV3Router())

	routerAddrs := dex.RouterAddresses()
	for chainID, dexAddrs := range routerAddrs {
		wsf.routers[chainID] = make(map[common.Address]dex.DEXType)
		for dexType, addr := range dexAddrs {
			wsf.routers[chainID][addr] = dexType
		}
	}

	return wsf
}

func (wsf *WSPriceFeed) Start() error {
	wsf.mu.Lock()
	if wsf.running {
		wsf.mu.Unlock()
		return nil
	}
	wsf.running = true
	wsf.mu.Unlock()

	for chainID, wsEndpoint := range wsf.endpoints {
		client, err := rpc.Dial(wsEndpoint)
		if err != nil {
			log.Printf("[WSFeed] Failed to connect to chain %d: %v", chainID, err)
			continue
		}
		wsf.clients[chainID] = client
		wsf.ethClients[chainID] = ethclient.NewClient(client)

		wsf.wg.Add(1)
		go wsf.subscribeChain(chainID, client)

		log.Printf("[WSFeed] Connected to chain %d via %s", chainID, wsEndpoint)
	}

	return nil
}

func (wsf *WSPriceFeed) Stop() {
	wsf.mu.Lock()
	if !wsf.running {
		wsf.mu.Unlock()
		return
	}
	wsf.running = false
	close(wsf.stopCh)
	wsf.mu.Unlock()

	wsf.wg.Wait()

	for _, client := range wsf.clients {
		client.Close()
	}
}

var (

	swapTopicV3 = common.HexToHash("0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67")

	swapTopicV2 = common.HexToHash("0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822")
)

func (wsf *WSPriceFeed) subscribeChain(chainID uint64, client *rpc.Client) {
	defer wsf.wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pools := defaultPoolMonitors()
	var poolAddrs []common.Address
	poolMap := make(map[common.Address]*PoolMonitor)
	for _, p := range pools {
		if p.ChainID == chainID {
			poolAddrs = append(poolAddrs, p.PoolAddress)
			poolMap[p.PoolAddress] = p
		}
	}

	if len(poolAddrs) == 0 {
		log.Printf("[WSFeed] Chain %d: no pools to monitor", chainID)
		return
	}

	logsCh := make(chan types.Log, 100)
	query := ethereum.FilterQuery{
		Addresses: poolAddrs,
		Topics:    [][]common.Hash{{swapTopicV2, swapTopicV3}},
	}

	sub, err := wsf.ethClients[chainID].SubscribeFilterLogs(ctx, query, logsCh)
	if err != nil {
		log.Printf("[WSFeed] Chain %d Swap subscription failed: %v", chainID, err)
		return
	}
	defer sub.Unsubscribe()

	log.Printf("[WSFeed] Chain %d subscribed to Swap events on %d pools", chainID, len(poolAddrs))

	for {
		select {
		case <-wsf.stopCh:
			return
		case err := <-sub.Err():
			log.Printf("[WSFeed] Chain %d subscription error: %v", chainID, err)
			return
		case swapLog := <-logsCh:
			pool, ok := poolMap[swapLog.Address]
			if !ok {
				continue
			}

			price := wsf.parsePriceFromSwap(swapLog, pool)
			if price != nil {
				wsf.priceFeed.SetPrice(pool.PairID, pool.ChainID, pool.DEX, price)
				priceFloat, _ := price.Float64()
				log.Printf("[SWAP] %d/%s: $%.2f", chainID, pool.DEX, priceFloat)
			}

			wsf.mu.Lock()
			wsf.swapsSeen[chainID]++
			wsf.mu.Unlock()
		}
	}
}

func (wsf *WSPriceFeed) processTx(chainID uint64, txHash common.Hash) {
	client := wsf.ethClients[chainID]
	if client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	tx, _, err := client.TransactionByHash(ctx, txHash)
	if err != nil {
		return
	}

	wsf.parseSwap(chainID, tx)
}

func (wsf *WSPriceFeed) parseSwap(chainID uint64, tx *types.Transaction) {
	if tx.To() == nil {
		return
	}

	to := *tx.To()
	data := tx.Data()

	if len(data) < 4 {
		return
	}

	routers, ok := wsf.routers[chainID]
	if !ok {
		return
	}

	dexType, isRouter := routers[to]
	if !isRouter {
		return
	}

	detectedDex, isSwap := dex.DetectDEXFromCalldata(data)
	if !isSwap {
		return
	}

	if detectedDex != "" {
		dexType = detectedDex
	}

	wsf.mu.Lock()
	wsf.swapsSeen[chainID]++
	wsf.mu.Unlock()

	router, ok := wsf.dexRegistry.GetRouter(dexType)
	if !ok {
		return
	}

	_ = router
	log.Printf("[WSFeed] Chain %d: Swap detected via %s, value: %s",
		chainID, dexType, tx.Value().String())

	go wsf.refreshChainPrices(chainID)
}

func (wsf *WSPriceFeed) refreshChainPrices(chainID uint64) {
	client := wsf.ethClients[chainID]
	if client == nil {
		return
	}

	pools := defaultPoolMonitors()

	var wg sync.WaitGroup
	for _, pool := range pools {
		if pool.ChainID != chainID {
			continue
		}

		wg.Add(1)
		go func(p *PoolMonitor) {
			defer wg.Done()
			price, err := wsf.fetchPoolPrice(client, p)
			if err != nil {
				return
			}
			wsf.priceFeed.SetPrice(p.PairID, p.ChainID, p.DEX, price)
		}(pool)
	}
	wg.Wait()
}

func (wsf *WSPriceFeed) RefreshChain(chainID uint64) {
	wsf.refreshChainPrices(chainID)
}

func (wsf *WSPriceFeed) parsePriceFromSwap(swapLog types.Log, pool *PoolMonitor) *big.Float {
	if len(swapLog.Data) < 32 {
		return nil
	}

	if pool.IsV3 {

		if len(swapLog.Data) < 96 {
			return nil
		}
		sqrtPriceX96 := new(big.Int).SetBytes(swapLog.Data[64:96])

		q96 := new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil)
		sqrtPrice := new(big.Float).Quo(
			new(big.Float).SetInt(sqrtPriceX96),
			new(big.Float).SetInt(q96),
		)
		price := new(big.Float).Mul(sqrtPrice, sqrtPrice)

		result, _ := wsf.convertToUSDCPerETH(price, pool.Inverted)
		return result
	}

	if len(swapLog.Data) < 128 {
		return nil
	}

	amount0In := new(big.Int).SetBytes(swapLog.Data[0:32])
	amount1In := new(big.Int).SetBytes(swapLog.Data[32:64])
	amount0Out := new(big.Int).SetBytes(swapLog.Data[64:96])
	amount1Out := new(big.Int).SetBytes(swapLog.Data[96:128])

	var price *big.Float
	if amount0In.Sign() > 0 && amount1Out.Sign() > 0 {

		price = new(big.Float).Quo(
			new(big.Float).SetInt(amount1Out),
			new(big.Float).SetInt(amount0In),
		)
	} else if amount1In.Sign() > 0 && amount0Out.Sign() > 0 {

		price = new(big.Float).Quo(
			new(big.Float).SetInt(amount1In),
			new(big.Float).SetInt(amount0Out),
		)
	} else {
		return nil
	}

	result, _ := wsf.convertToUSDCPerETH(price, pool.Inverted)
	return result
}

func (wsf *WSPriceFeed) FetchNonce(chainID uint64, address common.Address) (uint64, error) {
	client := wsf.ethClients[chainID]
	if client == nil {
		return 0, fmt.Errorf("no client for chain %d", chainID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return client.PendingNonceAt(ctx, address)
}

func (wsf *WSPriceFeed) GetCurrentPrices() map[uint64]map[string]*big.Float {
	if wsf.priceFeed == nil {
		return nil
	}

	result := make(map[uint64]map[string]*big.Float)

	pairs := wsf.priceFeed.GetTrackedPairs()
	for _, pairID := range pairs {
		prices := wsf.priceFeed.GetLatestPrices(pairID)
		for chainID, pp := range prices {
			if result[chainID] == nil {
				result[chainID] = make(map[string]*big.Float)
			}
			if pp != nil && pp.Price != nil {
				result[chainID][pairID] = pp.Price
			}
		}
	}

	return result
}

func (wsf *WSPriceFeed) GetStats() map[string]interface{} {
	wsf.mu.RLock()
	defer wsf.mu.RUnlock()

	chainStats := make(map[string]interface{})
	for chainID := range wsf.endpoints {
		chainStats[fmt.Sprintf("chain_%d", chainID)] = map[string]interface{}{
			"connected": wsf.clients[chainID] != nil,
			"tx_seen":   wsf.txSeen[chainID],
			"swaps":     wsf.swapsSeen[chainID],
		}
	}

	return map[string]interface{}{
		"running": wsf.running,
		"chains":  chainStats,
	}
}

func (wsf *WSPriceFeed) FetchCurrentPrices() error {
	pools := defaultPoolMonitors()

	for _, pool := range pools {
		client := wsf.ethClients[pool.ChainID]
		if client == nil {
			continue
		}

		price, err := wsf.fetchPoolPrice(client, pool)
		if err != nil {
			log.Printf("[WSFeed] Failed to fetch %s/%s on chain %d: %v", pool.DEX, pool.PairID, pool.ChainID, err)
			continue
		}

		wsf.priceFeed.SetPrice(pool.PairID, pool.ChainID, pool.DEX, price)

		priceFloat, _ := price.Float64()
		log.Printf("[WSFeed] %d/%s: $%.2f", pool.ChainID, pool.DEX, priceFloat)
	}

	return nil
}

type PoolMonitor struct {
	ChainID     uint64
	PoolAddress common.Address
	Token0      common.Address
	Token1      common.Address
	DEX         string
	PairID      string
	Fee         uint32
	IsV3        bool
	Inverted    bool
}

func defaultPoolMonitors() []*PoolMonitor {
	return []*PoolMonitor{

		{
			ChainID:     ChainIDEthereum,
			PoolAddress: common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640"),
			Token0:      common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
			Token1:      common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
			DEX:         "uniswap_v3_005",
			PairID:      "ETH-USDC",
			Fee:         5,
			IsV3:        true,
			Inverted:    false,
		},

		{
			ChainID:     ChainIDEthereum,
			PoolAddress: common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
			Token0:      common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
			Token1:      common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
			DEX:         "uniswap_v3_030",
			PairID:      "ETH-USDC",
			Fee:         30,
			IsV3:        true,
			Inverted:    false,
		},

		{
			ChainID:     ChainIDEthereum,
			PoolAddress: common.HexToAddress("0x7BeA39867e4169DBe237d55C8242a8f2fcDcc387"),
			Token0:      common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
			Token1:      common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
			DEX:         "uniswap_v3_100",
			PairID:      "ETH-USDC",
			Fee:         100,
			IsV3:        true,
			Inverted:    false,
		},

		{
			ChainID:     ChainIDEthereum,
			PoolAddress: common.HexToAddress("0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc"),
			Token0:      common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
			Token1:      common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
			DEX:         "uniswap_v2",
			PairID:      "ETH-USDC",
			Fee:         30,
			IsV3:        false,
			Inverted:    false,
		},

		{
			ChainID:     ChainIDEthereum,
			PoolAddress: common.HexToAddress("0x397FF1542f962076d0BFE58eA045FfA2d347ACa0"),
			Token0:      common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
			Token1:      common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
			DEX:         "sushiswap",
			PairID:      "ETH-USDC",
			Fee:         30,
			IsV3:        false,
			Inverted:    false,
		},

		{
			ChainID:     ChainIDBase,
			PoolAddress: common.HexToAddress("0xd0b53D9277642d899DF5C87A3966A349A798F224"),
			Token0:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
			Token1:      common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
			DEX:         "uniswap_v3_005",
			PairID:      "ETH-USDC",
			Fee:         5,
			IsV3:        true,
			Inverted:    true,
		},

		{
			ChainID:     ChainIDBase,
			PoolAddress: common.HexToAddress("0x4C36388bE6F416A29C8d8Eee81C771cE6bE14B18"),
			Token0:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
			Token1:      common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
			DEX:         "uniswap_v3_030",
			PairID:      "ETH-USDC",
			Fee:         30,
			IsV3:        true,
			Inverted:    true,
		},

		{
			ChainID:     ChainIDBase,
			PoolAddress: common.HexToAddress("0xcDAC0d6c6C59727a65F871236188350531885C43"),
			Token0:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
			Token1:      common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
			DEX:         "aerodrome",
			PairID:      "ETH-USDC",
			Fee:         30,
			IsV3:        false,
			Inverted:    true,
		},

		{
			ChainID:     ChainIDBase,
			PoolAddress: common.HexToAddress("0xb2cc224c1c9feE385f8ad6a55b4d94E92359DC59"),
			Token0:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
			Token1:      common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
			DEX:         "aerodrome_cl",
			PairID:      "ETH-USDC",
			Fee:         5,
			IsV3:        true,
			Inverted:    true,
		},

		{
			ChainID:     ChainIDBase,
			PoolAddress: common.HexToAddress("0x41d160033C222E6f3722EC97379867324567d883"),
			Token0:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
			Token1:      common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
			DEX:         "baseswap",
			PairID:      "ETH-USDC",
			Fee:         30,
			IsV3:        false,
			Inverted:    true,
		},

		{
			ChainID:     ChainIDOptimism,
			PoolAddress: common.HexToAddress("0x85149247691df622eaF1a8Bd0CaFd40BC45154a9"),
			Token0:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
			Token1:      common.HexToAddress("0x7F5c764cBc14f9669B88837ca1490cCa17c31607"),
			DEX:         "uniswap_v3_005",
			PairID:      "ETH-USDC",
			Fee:         5,
			IsV3:        true,
			Inverted:    true,
		},

		{
			ChainID:     ChainIDOptimism,
			PoolAddress: common.HexToAddress("0x79c912FEF520be002c2B6e57EC4324e260f38E50"),
			Token0:      common.HexToAddress("0x4200000000000000000000000000000000000006"),
			Token1:      common.HexToAddress("0x7F5c764cBc14f9669B88837ca1490cCa17c31607"),
			DEX:         "velodrome",
			PairID:      "ETH-USDC",
			Fee:         30,
			IsV3:        false,
			Inverted:    true,
		},
	}
}

func (wsf *WSPriceFeed) fetchPoolPrice(client *ethclient.Client, pool *PoolMonitor) (*big.Float, error) {

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if pool.IsV3 {
		return wsf.fetchV3Price(ctx, client, pool)
	}
	return wsf.fetchV2Price(ctx, client, pool)
}

func (wsf *WSPriceFeed) fetchV3Price(ctx context.Context, client *ethclient.Client, pool *PoolMonitor) (*big.Float, error) {

	msg := map[string]interface{}{
		"to":   pool.PoolAddress.Hex(),
		"data": "0x3850c7bd",
	}

	var result string
	err := client.Client().CallContext(ctx, &result, "eth_call", msg, "latest")
	if err != nil {
		return nil, err
	}

	resultBytes := common.FromHex(result)
	if len(resultBytes) < 32 {
		return nil, fmt.Errorf("invalid slot0 response")
	}

	sqrtPriceX96 := new(big.Int).SetBytes(resultBytes[:32])

	q96 := new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil)

	sqrtPrice := new(big.Float).Quo(
		new(big.Float).SetInt(sqrtPriceX96),
		new(big.Float).SetInt(q96),
	)

	price := new(big.Float).Mul(sqrtPrice, sqrtPrice)

	return wsf.convertToUSDCPerETH(price, pool.Inverted)
}

func (wsf *WSPriceFeed) fetchV2Price(ctx context.Context, client *ethclient.Client, pool *PoolMonitor) (*big.Float, error) {

	msg := map[string]interface{}{
		"to":   pool.PoolAddress.Hex(),
		"data": "0x0902f1ac",
	}

	var result string
	err := client.Client().CallContext(ctx, &result, "eth_call", msg, "latest")
	if err != nil {
		return nil, err
	}

	resultBytes := common.FromHex(result)
	if len(resultBytes) < 64 {
		return nil, fmt.Errorf("invalid getReserves response: %d bytes", len(resultBytes))
	}

	reserve0 := new(big.Int).SetBytes(resultBytes[0:32])
	reserve1 := new(big.Int).SetBytes(resultBytes[32:64])

	if reserve0.Sign() == 0 || reserve1.Sign() == 0 {
		return nil, fmt.Errorf("zero reserves")
	}

	price := new(big.Float).Quo(
		new(big.Float).SetInt(reserve1),
		new(big.Float).SetInt(reserve0),
	)

	return wsf.convertToUSDCPerETH(price, pool.Inverted)
}

func (wsf *WSPriceFeed) convertToUSDCPerETH(price *big.Float, inverted bool) (*big.Float, error) {

	decimalAdjust := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil))

	var priceUSDC *big.Float

	if inverted {

		priceUSDC = new(big.Float).Mul(price, decimalAdjust)
	} else {

		priceUSDC = new(big.Float).Quo(decimalAdjust, price)
	}

	return priceUSDC, nil
}
