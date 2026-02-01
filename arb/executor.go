package arb

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"zeam/node"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type Executor struct {
	mu sync.RWMutex

	transport *node.MultiChainTransport

	positions *PositionManager

	detector *OpportunityDetector

	privateKey *ecdsa.PrivateKey
	address    common.Address

	preSignedCache *PreSignedCache

	nonces   map[uint64]uint64
	noncesMu sync.Mutex

	config *ExecutorConfig

	results chan *ExecutionResult

	queue     chan *ArbitrageOpportunity
	queueSize int

	totalExecuted    uint64
	totalSuccess     uint64
	totalFailed      uint64
	preSignedUsed    uint64
	preSignedMissed  uint64

	OnProfit func(profit *big.Int)

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type ExecutorConfig struct {
	MaxGasPrice       *big.Int
	SlippageBPS       int64
	ExecutionTimeout  time.Duration
	MaxConcurrent     int
	UseFlashLoans     bool
	MinProfitAfterGas *big.Int
}

func DefaultExecutorConfig() *ExecutorConfig {
	return &ExecutorConfig{
		MaxGasPrice:       big.NewInt(50e9),
		SlippageBPS:       50,
		ExecutionTimeout:  30 * time.Second,
		MaxConcurrent:     3,
		UseFlashLoans:     true,
		MinProfitAfterGas: big.NewInt(1e16),
	}
}

func NewExecutor(
	transport *node.MultiChainTransport,
	positions *PositionManager,
	detector *OpportunityDetector,
	privateKey *ecdsa.PrivateKey,
	config *ExecutorConfig,
) *Executor {
	if config == nil {
		config = DefaultExecutorConfig()
	}

	preSignedCache := NewPreSignedCache(privateKey, DefaultPreSignedConfig())

	return &Executor{
		transport:      transport,
		positions:      positions,
		detector:       detector,
		privateKey:     privateKey,
		address:        crypto.PubkeyToAddress(privateKey.PublicKey),
		preSignedCache: preSignedCache,
		nonces:         make(map[uint64]uint64),
		config:         config,
		results:        make(chan *ExecutionResult, 100),
		queue:          make(chan *ArbitrageOpportunity, 100),
		queueSize:      100,
		stopCh:         make(chan struct{}),
	}
}

func (e *Executor) Start() error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = true
	e.mu.Unlock()

	if e.preSignedCache != nil {
		e.preSignedCache.Start()
	}

	for i := 0; i < e.config.MaxConcurrent; i++ {
		e.wg.Add(1)
		go e.worker(i)
	}

	return nil
}

func (e *Executor) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	close(e.stopCh)
	e.mu.Unlock()

	if e.preSignedCache != nil {
		e.preSignedCache.Stop()
	}

	e.wg.Wait()
	close(e.results)
}

func (e *Executor) worker(id int) {
	defer e.wg.Done()

	for {
		select {
		case <-e.stopCh:
			return
		case opp := <-e.queue:
			result := e.execute(opp)
			select {
			case e.results <- result:
			default:

			}
		}
	}
}

func (e *Executor) Submit(opp *ArbitrageOpportunity) error {
	e.mu.RLock()
	if !e.running {
		e.mu.RUnlock()
		return fmt.Errorf("executor not running")
	}
	e.mu.RUnlock()

	select {
	case e.queue <- opp:
		if e.detector != nil {
			e.detector.MarkExecuting(opp.ID)
		}
		return nil
	default:
		return fmt.Errorf("execution queue full")
	}
}

func (e *Executor) Results() <-chan *ExecutionResult {
	return e.results
}

func (e *Executor) execute(opp *ArbitrageOpportunity) *ExecutionResult {
	ctx, cancel := context.WithTimeout(context.Background(), e.config.ExecutionTimeout)
	defer cancel()

	result := &ExecutionResult{
		Opportunity: opp,
		TxHashes:    make(map[uint64]common.Hash),
		GasUsed:     make(map[uint64]*big.Int),
		ExecutedAt:  time.Now(),
	}

	e.totalExecuted++

	var err error
	if opp.IsCrossChain {
		err = e.executeCrossChain(ctx, opp, result)
	} else {
		err = e.executeSameChain(ctx, opp, result)
	}

	if err != nil {
		result.Success = false
		result.Error = err
		e.totalFailed++
	} else {
		result.Success = true
		e.totalSuccess++
	}

	result.Latency = time.Since(result.ExecutedAt)

	if e.detector != nil {
		e.detector.MarkCompleted(opp.ID, result.Success)
	}

	if result.Success && result.ActualProfit != nil && e.positions != nil {
		e.positions.RecordProfit(result.ActualProfit)
		if e.OnProfit != nil {
			e.OnProfit(result.ActualProfit)
		}
	}

	return result
}

func (e *Executor) executeSameChain(ctx context.Context, opp *ArbitrageOpportunity, result *ExecutionResult) error {
	if len(opp.Path) < 2 {
		return fmt.Errorf("invalid path: need at least 2 steps")
	}

	chainID := opp.Path[0].ChainID

	inputAmount := opp.InputAmount
	if inputAmount == nil || inputAmount.Sign() <= 0 {

		if e.positions != nil {
			inputAmount = e.positions.GetMaxTradeSize(chainID, opp.Path[0].TokenIn, 0.1)
		}
		if inputAmount == nil || inputAmount.Sign() <= 0 {
			return fmt.Errorf("no input amount available")
		}
	}

	if e.positions != nil {
		if err := e.positions.Lock(chainID, opp.Path[0].TokenIn, inputAmount); err != nil {
			return fmt.Errorf("failed to lock position: %w", err)
		}
		defer e.positions.Unlock(chainID, opp.Path[0].TokenIn, inputAmount)
	}

	for i, step := range opp.Path {
		txHash, err := e.executeStep(ctx, step, inputAmount)
		if err != nil {
			return fmt.Errorf("step %d failed: %w", i, err)
		}
		result.TxHashes[chainID] = txHash

	}

	result.ActualProfit = opp.ExpectedProfit

	return nil
}

func (e *Executor) executeCrossChain(ctx context.Context, opp *ArbitrageOpportunity, result *ExecutionResult) error {

	if len(opp.Path) < 2 {
		return fmt.Errorf("invalid path: need at least 2 steps")
	}

	inputAmount := opp.InputAmount
	if inputAmount == nil || inputAmount.Sign() <= 0 {
		if e.positions != nil {
			inputAmount = e.positions.GetMaxTradeSize(opp.BuyChain, opp.Path[0].TokenIn, 0.1)
		}
		if inputAmount == nil || inputAmount.Sign() <= 0 {
			return fmt.Errorf("no input amount available")
		}
	}

	if e.positions != nil {

		if err := e.positions.Lock(opp.BuyChain, opp.Path[0].TokenIn, inputAmount); err != nil {
			return fmt.Errorf("failed to lock buy position: %w", err)
		}
		defer e.positions.Unlock(opp.BuyChain, opp.Path[0].TokenIn, inputAmount)

		sellAmount := inputAmount
		if len(opp.Path) > 1 {
			if err := e.positions.Lock(opp.SellChain, opp.Path[1].TokenIn, sellAmount); err != nil {
				return fmt.Errorf("failed to lock sell position: %w", err)
			}
			defer e.positions.Unlock(opp.SellChain, opp.Path[1].TokenIn, sellAmount)
		}
	}

	var wg sync.WaitGroup
	var buyHash, sellHash common.Hash
	var buyErr, sellErr error

	wg.Add(2)

	go func() {
		defer wg.Done()
		buyHash, buyErr = e.executeStep(ctx, opp.Path[0], inputAmount)
	}()

	go func() {
		defer wg.Done()
		if len(opp.Path) > 1 {
			sellHash, sellErr = e.executeStep(ctx, opp.Path[1], inputAmount)
		}
	}()

	wg.Wait()

	result.TxHashes[opp.BuyChain] = buyHash
	if opp.SellChain != opp.BuyChain {
		result.TxHashes[opp.SellChain] = sellHash
	}

	if buyErr != nil || sellErr != nil {
		return fmt.Errorf("execution failed - buy: %v, sell: %v", buyErr, sellErr)
	}

	result.ActualProfit = opp.ExpectedProfit

	return nil
}

func (e *Executor) executeStep(ctx context.Context, step PathStep, amount *big.Int) (common.Hash, error) {

	if e.preSignedCache != nil {

		weth := WETHAddresses()[step.ChainID]
		direction := "buy"
		if step.TokenIn == weth {
			direction = "sell"
		}

		preSigned, err := e.preSignedCache.GetPreSignedTx(step.ChainID, step.DEX, direction, amount)
		if err == nil {

			hash, err := e.transport.BroadcastRawTxOnChain(step.ChainID, preSigned.SignedTx)
			if err == nil {
				e.mu.Lock()
				e.preSignedUsed++
				e.mu.Unlock()

				e.preSignedCache.MarkNonceUsed(step.ChainID, preSigned.Nonce)
				log.Printf("[PRE-SIGNED] INSTANT! Chain %d, DEX %s, nonce %d - direct DevP2P broadcast",
					step.ChainID, step.DEX, preSigned.Nonce)
				return hash, nil
			}
			log.Printf("[PRE-SIGNED] Broadcast failed, falling back: %v", err)
		} else {
			log.Printf("[PRE-SIGNED] Cache miss for %d/%s/%s: %v", step.ChainID, step.DEX, direction, err)
		}
		e.mu.Lock()
		e.preSignedMissed++
		e.mu.Unlock()
	}

	tx, err := e.buildSwapTx(step, amount)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to build tx: %w", err)
	}

	signedTx, err := e.signTx(step.ChainID, tx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to sign tx: %w", err)
	}

	hash, err := e.transport.BroadcastRawTxOnChain(step.ChainID, signedTx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to broadcast: %w", err)
	}

	return hash, nil
}

func (e *Executor) buildSwapTx(step PathStep, amount *big.Int) (*types.DynamicFeeTx, error) {

	routers := RouterAddresses()
	chainRouters, ok := routers[step.ChainID]
	if !ok {
		return nil, fmt.Errorf("no routers for chain %d", step.ChainID)
	}

	var dexType DEXType
	switch step.DEX {
	case "uniswap_v2", "sushiswap":
		dexType = DEXUniswapV2
	case "uniswap_v3":
		dexType = DEXUniswapV3
	case "aerodrome":
		dexType = DEXAerodrome
	case "velodrome":
		dexType = DEXVelodrome
	default:
		dexType = DEXUniswapV2
	}

	routerAddr, ok := chainRouters[dexType]
	if !ok {
		return nil, fmt.Errorf("no router for DEX %s on chain %d", step.DEX, step.ChainID)
	}

	calldata := make([]byte, 4+32*5)
	copy(calldata[0:4], []byte{0x38, 0xed, 0x17, 0x39})

	amountBytes := amount.Bytes()
	copy(calldata[4+32-len(amountBytes):4+32], amountBytes)

	return &types.DynamicFeeTx{
		ChainID:   big.NewInt(int64(step.ChainID)),
		Nonce:     e.nextNonce(step.ChainID),
		GasTipCap: big.NewInt(1e9),
		GasFeeCap: e.config.MaxGasPrice,
		Gas:       300000,
		To:        &routerAddr,
		Value:     big.NewInt(0),
		Data:      calldata,
	}, nil
}

func (e *Executor) signTx(chainID uint64, tx *types.DynamicFeeTx) ([]byte, error) {
	signer := types.NewLondonSigner(big.NewInt(int64(chainID)))
	signedTx, err := types.SignNewTx(e.privateKey, signer, tx)
	if err != nil {
		return nil, err
	}

	return signedTx.MarshalBinary()
}

func (e *Executor) nextNonce(chainID uint64) uint64 {
	e.noncesMu.Lock()
	defer e.noncesMu.Unlock()

	nonce := e.nonces[chainID]
	e.nonces[chainID]++
	return nonce
}

func (e *Executor) SetNonce(chainID uint64, nonce uint64) {
	e.noncesMu.Lock()
	defer e.noncesMu.Unlock()
	e.nonces[chainID] = nonce

	if e.preSignedCache != nil {
		e.preSignedCache.SetNonce(chainID, nonce)
	}
}

func (e *Executor) GetStats() ExecutorStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	successRate := float64(0)
	if e.totalExecuted > 0 {
		successRate = float64(e.totalSuccess) / float64(e.totalExecuted)
	}

	return ExecutorStats{
		TotalExecuted: e.totalExecuted,
		TotalSuccess:  e.totalSuccess,
		TotalFailed:   e.totalFailed,
		SuccessRate:   successRate,
		QueueSize:     len(e.queue),
		Running:       e.running,
	}
}

type ExecutorStats struct {
	TotalExecuted uint64
	TotalSuccess  uint64
	TotalFailed   uint64
	SuccessRate   float64
	QueueSize     int
	Running       bool
}

type DEXType = string

const (
	DEXUniswapV2  DEXType = "uniswap_v2"
	DEXUniswapV3  DEXType = "uniswap_v3"
	DEXAerodrome  DEXType = "aerodrome"
	DEXVelodrome  DEXType = "velodrome"
)

func RouterAddresses() map[uint64]map[DEXType]common.Address {
	return map[uint64]map[DEXType]common.Address{
		ChainIDEthereum: {
			DEXUniswapV2: common.HexToAddress("0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D"),
			DEXUniswapV3: common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"),
		},
		ChainIDBase: {
			DEXUniswapV2:  common.HexToAddress("0x4752ba5DBc23f44D87826276BF6Fd6b1C372aD24"),
			DEXUniswapV3:  common.HexToAddress("0x2626664c2603336E57B271c5C0b26F421741e481"),
			DEXAerodrome: common.HexToAddress("0xcF77a3Ba9A5CA399B7c97c74d54e5b1Beb874E43"),
		},
		ChainIDOptimism: {
			DEXUniswapV2:  common.HexToAddress("0x4A7b5Da61326A6379179b40d00F57E5bbDC962c2"),
			DEXUniswapV3:  common.HexToAddress("0xE592427A0AEce92De3Edee1F18E0157C05861564"),
			DEXVelodrome: common.HexToAddress("0xa062aE8A9c5e11aaA026fc2670B0D65cCc8B2858"),
		},
	}
}
