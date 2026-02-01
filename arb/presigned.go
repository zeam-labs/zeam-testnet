package arb

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type PreSignedTx struct {
	ChainID   uint64
	DEX       string
	Direction string
	Amount    *big.Int
	Nonce     uint64
	SignedTx  []byte
	TxHash    common.Hash
	CreatedAt time.Time
	ExpiresAt time.Time
}

type PreSignedCache struct {
	mu sync.RWMutex

	privateKey *ecdsa.PrivateKey
	address    common.Address

	cache map[uint64]map[string]map[string][]*PreSignedTx

	currentNonce map[uint64]uint64
	maxNonce     map[uint64]uint64

	config *PreSignedConfig

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type PreSignedConfig struct {

	CacheDepth int

	Amounts []*big.Int

	TxLifetime time.Duration

	MaxGasPrice   *big.Int
	MaxPriorityFee *big.Int

	Chains []uint64
	DEXs   []string
}

func DefaultPreSignedConfig() *PreSignedConfig {
	return &PreSignedConfig{
		CacheDepth: 3,
		Amounts: []*big.Int{
			big.NewInt(1e17),
			big.NewInt(5e17),
			big.NewInt(1e18),
		},
		TxLifetime:     30 * time.Second,
		MaxGasPrice:    big.NewInt(100e9),
		MaxPriorityFee: big.NewInt(2e9),
		Chains: []uint64{ChainIDEthereum, ChainIDBase, ChainIDOptimism},
		DEXs: []string{

			"uniswap_v3_005", "uniswap_v3_030", "uniswap_v3_100", "uniswap_v2", "sushiswap",

			"aerodrome", "aerodrome_cl", "baseswap",

			"velodrome",
		},
	}
}

func NewPreSignedCache(privateKey *ecdsa.PrivateKey, config *PreSignedConfig) *PreSignedCache {
	if config == nil {
		config = DefaultPreSignedConfig()
	}

	return &PreSignedCache{
		privateKey:   privateKey,
		address:      crypto.PubkeyToAddress(privateKey.PublicKey),
		cache:        make(map[uint64]map[string]map[string][]*PreSignedTx),
		currentNonce: make(map[uint64]uint64),
		maxNonce:     make(map[uint64]uint64),
		config:       config,
		stopCh:       make(chan struct{}),
	}
}

func (psc *PreSignedCache) SetNonce(chainID uint64, nonce uint64) {
	psc.mu.Lock()
	defer psc.mu.Unlock()

	psc.currentNonce[chainID] = nonce
	if psc.maxNonce[chainID] < nonce {
		psc.maxNonce[chainID] = nonce
	}
}

func (psc *PreSignedCache) Start() error {
	psc.mu.Lock()
	if psc.running {
		psc.mu.Unlock()
		return nil
	}
	psc.running = true
	psc.mu.Unlock()

	psc.populateCache()

	psc.wg.Add(1)
	go psc.refreshLoop()

	return nil
}

func (psc *PreSignedCache) Stop() {
	psc.mu.Lock()
	if !psc.running {
		psc.mu.Unlock()
		return
	}
	psc.running = false
	close(psc.stopCh)
	psc.mu.Unlock()

	psc.wg.Wait()
}

func (psc *PreSignedCache) refreshLoop() {
	defer psc.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-psc.stopCh:
			return
		case <-ticker.C:
			psc.pruneExpired()
			psc.populateCache()
		}
	}
}

func (psc *PreSignedCache) populateCache() {
	psc.mu.Lock()
	defer psc.mu.Unlock()

	for _, chainID := range psc.config.Chains {
		if psc.cache[chainID] == nil {
			psc.cache[chainID] = make(map[string]map[string][]*PreSignedTx)
		}

		for _, dex := range psc.config.DEXs {
			if psc.cache[chainID][dex] == nil {
				psc.cache[chainID][dex] = make(map[string][]*PreSignedTx)
			}

			for _, direction := range []string{"buy", "sell"} {
				for _, amount := range psc.config.Amounts {
					psc.ensureCacheDepth(chainID, dex, direction, amount)
				}
			}
		}
	}
}

func (psc *PreSignedCache) ensureCacheDepth(chainID uint64, dex, direction string, amount *big.Int) {
	key := fmt.Sprintf("%s_%s", direction, amount.String())
	existing := psc.cache[chainID][dex][key]

	validCount := 0
	for _, tx := range existing {
		if time.Now().Before(tx.ExpiresAt) {
			validCount++
		}
	}

	for validCount < psc.config.CacheDepth {
		tx, err := psc.preSignTx(chainID, dex, direction, amount)
		if err != nil {
			break
		}
		psc.cache[chainID][dex][key] = append(psc.cache[chainID][dex][key], tx)
		validCount++
	}
}

func (psc *PreSignedCache) preSignTx(chainID uint64, dex, direction string, amount *big.Int) (*PreSignedTx, error) {

	nonce := psc.maxNonce[chainID]
	psc.maxNonce[chainID]++

	calldata, to, err := psc.buildSwapCalldata(chainID, dex, direction, amount)
	if err != nil {
		return nil, err
	}

	gasLimit := uint64(300000)
	if dex == "uniswap_v2" || dex == "sushiswap" {
		gasLimit = 200000
	}

	tx := &types.DynamicFeeTx{
		ChainID:   big.NewInt(int64(chainID)),
		Nonce:     nonce,
		GasTipCap: psc.config.MaxPriorityFee,
		GasFeeCap: psc.config.MaxGasPrice,
		Gas:       gasLimit,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      calldata,
	}

	signer := types.NewLondonSigner(big.NewInt(int64(chainID)))
	signedTx, err := types.SignNewTx(psc.privateKey, signer, tx)
	if err != nil {
		return nil, fmt.Errorf("signing failed: %w", err)
	}

	rawTx, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("encoding failed: %w", err)
	}

	return &PreSignedTx{
		ChainID:   chainID,
		DEX:       dex,
		Direction: direction,
		Amount:    amount,
		Nonce:     nonce,
		SignedTx:  rawTx,
		TxHash:    signedTx.Hash(),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(psc.config.TxLifetime),
	}, nil
}

func (psc *PreSignedCache) buildSwapCalldata(chainID uint64, dex, direction string, amount *big.Int) ([]byte, common.Address, error) {
	routers := RouterAddresses()
	chainRouters, ok := routers[chainID]
	if !ok {
		return nil, common.Address{}, fmt.Errorf("no routers for chain %d", chainID)
	}

	var dexType DEXType
	switch dex {
	case "uniswap_v2", "sushiswap", "baseswap":
		dexType = DEXUniswapV2
	case "uniswap_v3_005", "uniswap_v3_030":
		dexType = DEXUniswapV3
	case "aerodrome", "velodrome":
		dexType = DEXAerodrome
	default:
		return nil, common.Address{}, fmt.Errorf("unknown DEX: %s", dex)
	}

	router, ok := chainRouters[dexType]
	if !ok {
		return nil, common.Address{}, fmt.Errorf("no router for DEX type %s on chain %d", dexType, chainID)
	}

	weth := WETHAddresses()[chainID]
	usdc := USDCAddresses()[chainID]

	var tokenIn, tokenOut common.Address
	if direction == "buy" {
		tokenIn = usdc
		tokenOut = weth
	} else {
		tokenIn = weth
		tokenOut = usdc
	}

	var calldata []byte
	switch dexType {
	case DEXUniswapV2:

		calldata = buildV2SwapCalldata(amount, tokenIn, tokenOut, psc.address)

	case DEXUniswapV3:

		fee := uint32(500)
		if dex == "uniswap_v3_030" {
			fee = 3000
		}
		calldata = buildV3SwapCalldata(amount, tokenIn, tokenOut, fee, psc.address)

	case DEXAerodrome:

		calldata = buildSolidlySwapCalldata(amount, tokenIn, tokenOut, psc.address, false)
	}

	return calldata, router, nil
}

func (psc *PreSignedCache) GetPreSignedTx(chainID uint64, dex, direction string, amount *big.Int) (*PreSignedTx, error) {
	psc.mu.Lock()
	defer psc.mu.Unlock()

	key := fmt.Sprintf("%s_%s", direction, psc.findClosestAmount(amount).String())

	if psc.cache[chainID] == nil || psc.cache[chainID][dex] == nil {
		return nil, fmt.Errorf("no cache for chain %d DEX %s", chainID, dex)
	}

	txs := psc.cache[chainID][dex][key]
	if len(txs) == 0 {
		return nil, fmt.Errorf("no pre-signed tx available")
	}

	currentNonce := psc.currentNonce[chainID]
	for i, tx := range txs {
		if time.Now().Before(tx.ExpiresAt) && tx.Nonce >= currentNonce {

			psc.cache[chainID][dex][key] = append(txs[:i], txs[i+1:]...)
			return tx, nil
		}
	}

	return nil, fmt.Errorf("no valid pre-signed tx (nonce expired)")
}

func (psc *PreSignedCache) findClosestAmount(target *big.Int) *big.Int {
	var closest *big.Int
	var minDiff *big.Int

	for _, amt := range psc.config.Amounts {
		diff := new(big.Int).Sub(amt, target)
		diff.Abs(diff)

		if minDiff == nil || diff.Cmp(minDiff) < 0 {
			minDiff = diff
			closest = amt
		}
	}

	return closest
}

func (psc *PreSignedCache) pruneExpired() {
	psc.mu.Lock()
	defer psc.mu.Unlock()

	now := time.Now()
	for chainID := range psc.cache {
		for dex := range psc.cache[chainID] {
			for key := range psc.cache[chainID][dex] {
				var valid []*PreSignedTx
				for _, tx := range psc.cache[chainID][dex][key] {
					if now.Before(tx.ExpiresAt) {
						valid = append(valid, tx)
					}
				}
				psc.cache[chainID][dex][key] = valid
			}
		}
	}
}

func (psc *PreSignedCache) MarkNonceUsed(chainID uint64, nonce uint64) {
	psc.mu.Lock()
	defer psc.mu.Unlock()

	if nonce >= psc.currentNonce[chainID] {
		psc.currentNonce[chainID] = nonce + 1
	}

	for dex := range psc.cache[chainID] {
		for key := range psc.cache[chainID][dex] {
			var valid []*PreSignedTx
			for _, tx := range psc.cache[chainID][dex][key] {
				if tx.Nonce != nonce {
					valid = append(valid, tx)
				}
			}
			psc.cache[chainID][dex][key] = valid
		}
	}
}

func (psc *PreSignedCache) GetStats() map[string]interface{} {
	psc.mu.RLock()
	defer psc.mu.RUnlock()

	total := 0
	byChain := make(map[uint64]int)

	for chainID := range psc.cache {
		for dex := range psc.cache[chainID] {
			for key := range psc.cache[chainID][dex] {
				count := len(psc.cache[chainID][dex][key])
				total += count
				byChain[chainID] += count
			}
		}
	}

	return map[string]interface{}{
		"total":    total,
		"by_chain": byChain,
		"nonces":   psc.currentNonce,
	}
}

func buildV2SwapCalldata(amount *big.Int, tokenIn, tokenOut, recipient common.Address) []byte {

	selector := []byte{0x38, 0xed, 0x17, 0x39}

	amountOutMin := big.NewInt(0)
	deadline := big.NewInt(time.Now().Add(5 * time.Minute).Unix())

	data := make([]byte, 260)
	copy(data[0:4], selector)

	copy(data[4:36], common.LeftPadBytes(amount.Bytes(), 32))

	copy(data[36:68], common.LeftPadBytes(amountOutMin.Bytes(), 32))

	copy(data[68:100], common.LeftPadBytes(big.NewInt(160).Bytes(), 32))

	copy(data[100:132], common.LeftPadBytes(recipient.Bytes(), 32))

	copy(data[132:164], common.LeftPadBytes(deadline.Bytes(), 32))

	copy(data[164:196], common.LeftPadBytes(big.NewInt(2).Bytes(), 32))

	copy(data[196:228], common.LeftPadBytes(tokenIn.Bytes(), 32))

	copy(data[228:260], common.LeftPadBytes(tokenOut.Bytes(), 32))

	return data[:260]
}

func buildV3SwapCalldata(amount *big.Int, tokenIn, tokenOut common.Address, fee uint32, recipient common.Address) []byte {

	selector := []byte{0x41, 0x4b, 0xf3, 0x89}

	deadline := big.NewInt(time.Now().Add(5 * time.Minute).Unix())
	amountOutMin := big.NewInt(0)
	sqrtPriceLimit := big.NewInt(0)

	data := make([]byte, 4+32*8)
	copy(data[0:4], selector)

	offset := 4

	copy(data[offset:offset+32], common.LeftPadBytes(tokenIn.Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(tokenOut.Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(big.NewInt(int64(fee)).Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(recipient.Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(deadline.Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(amount.Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(amountOutMin.Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(sqrtPriceLimit.Bytes(), 32))

	return data
}

func buildSolidlySwapCalldata(amount *big.Int, tokenIn, tokenOut, recipient common.Address, stable bool) []byte {

	selector := []byte{0x38, 0xed, 0x17, 0x39}

	amountOutMin := big.NewInt(0)
	deadline := big.NewInt(time.Now().Add(5 * time.Minute).Unix())

	data := make([]byte, 260)
	copy(data[0:4], selector)

	copy(data[4:36], common.LeftPadBytes(amount.Bytes(), 32))
	copy(data[36:68], common.LeftPadBytes(amountOutMin.Bytes(), 32))
	copy(data[68:100], common.LeftPadBytes(big.NewInt(160).Bytes(), 32))
	copy(data[100:132], common.LeftPadBytes(recipient.Bytes(), 32))
	copy(data[132:164], common.LeftPadBytes(deadline.Bytes(), 32))
	copy(data[164:196], common.LeftPadBytes(big.NewInt(2).Bytes(), 32))
	copy(data[196:228], common.LeftPadBytes(tokenIn.Bytes(), 32))
	copy(data[228:260], common.LeftPadBytes(tokenOut.Bytes(), 32))

	return data
}
