package arb

import (
	"math/big"
	"sync"
	"time"

	"zeam/node"
	"zeam/quantum"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	ChainIDEthereum  = node.ChainIDEthereum
	ChainIDSepolia   = node.ChainIDSepolia
	ChainIDOptimism  = node.ChainIDOptimism
	ChainIDBase      = node.ChainIDBase
	ChainIDZora      = node.ChainIDZora
	ChainIDMode      = node.ChainIDMode
	ChainIDFraxtal   = node.ChainIDFraxtal
	ChainIDWorldcoin = node.ChainIDWorldcoin
	ChainIDCyber     = node.ChainIDCyber
	ChainIDMint      = node.ChainIDMint
	ChainIDRedstone  = node.ChainIDRedstone
	ChainIDLisk      = node.ChainIDLisk
	ChainIDBaseSep   = node.ChainIDBaseSep
	ChainIDOptimSep  = node.ChainIDOptimSep
	ChainIDZoraSep   = node.ChainIDZoraSep
	ChainIDModeSep   = node.ChainIDModeSep
	ChainIDArbitrum  = node.ChainIDArbitrum
)

type ChainConfig = node.ChainConfig

var DefaultChainConfigs = node.DefaultChainConfigs
var TestnetChainConfigs = node.TestnetChainConfigs

type TokenInfo struct {
	Address  common.Address
	Symbol   string
	Decimals uint8
	ChainID  uint64
}

type TokenPair struct {
	Token0 TokenInfo
	Token1 TokenInfo
}

func (tp *TokenPair) ID() string {
	return tp.Token0.Symbol + "-" + tp.Token1.Symbol
}

func CommonTokens() map[uint64]map[string]TokenInfo {
	return map[uint64]map[string]TokenInfo{
		ChainIDEthereum: {
			"WETH": {Address: common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"), Symbol: "WETH", Decimals: 18, ChainID: ChainIDEthereum},
			"USDC": {Address: common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"), Symbol: "USDC", Decimals: 6, ChainID: ChainIDEthereum},
			"USDT": {Address: common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"), Symbol: "USDT", Decimals: 6, ChainID: ChainIDEthereum},
			"WBTC": {Address: common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"), Symbol: "WBTC", Decimals: 8, ChainID: ChainIDEthereum},
			"DAI":  {Address: common.HexToAddress("0x6B175474E89094C44Da98b954EescdeCB5BE3830"), Symbol: "DAI", Decimals: 18, ChainID: ChainIDEthereum},
		},
		ChainIDBase: {
			"WETH": {Address: common.HexToAddress("0x4200000000000000000000000000000000000006"), Symbol: "WETH", Decimals: 18, ChainID: ChainIDBase},
			"USDC": {Address: common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"), Symbol: "USDC", Decimals: 6, ChainID: ChainIDBase},
			"USDbC": {Address: common.HexToAddress("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA"), Symbol: "USDbC", Decimals: 6, ChainID: ChainIDBase},
			"DAI":  {Address: common.HexToAddress("0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb"), Symbol: "DAI", Decimals: 18, ChainID: ChainIDBase},
		},
		ChainIDOptimism: {
			"WETH": {Address: common.HexToAddress("0x4200000000000000000000000000000000000006"), Symbol: "WETH", Decimals: 18, ChainID: ChainIDOptimism},
			"USDC": {Address: common.HexToAddress("0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85"), Symbol: "USDC", Decimals: 6, ChainID: ChainIDOptimism},
			"USDT": {Address: common.HexToAddress("0x94b008aA00579c1307B0EF2c499aD98a8ce58e58"), Symbol: "USDT", Decimals: 6, ChainID: ChainIDOptimism},
			"DAI":  {Address: common.HexToAddress("0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1"), Symbol: "DAI", Decimals: 18, ChainID: ChainIDOptimism},
		},
		ChainIDArbitrum: {
			"WETH": {Address: common.HexToAddress("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"), Symbol: "WETH", Decimals: 18, ChainID: ChainIDArbitrum},
			"USDC": {Address: common.HexToAddress("0xaf88d065e77c8cC2239327C5EDb3A432268e5831"), Symbol: "USDC", Decimals: 6, ChainID: ChainIDArbitrum},
			"USDT": {Address: common.HexToAddress("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"), Symbol: "USDT", Decimals: 6, ChainID: ChainIDArbitrum},
			"WBTC": {Address: common.HexToAddress("0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f"), Symbol: "WBTC", Decimals: 8, ChainID: ChainIDArbitrum},
			"DAI":  {Address: common.HexToAddress("0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1"), Symbol: "DAI", Decimals: 18, ChainID: ChainIDArbitrum},
		},
	}
}

func WETHAddresses() map[uint64]common.Address {
	tokens := CommonTokens()
	result := make(map[uint64]common.Address)
	for chainID, chainTokens := range tokens {
		if weth, ok := chainTokens["WETH"]; ok {
			result[chainID] = weth.Address
		}
	}
	return result
}

func USDCAddresses() map[uint64]common.Address {
	tokens := CommonTokens()
	result := make(map[uint64]common.Address)
	for chainID, chainTokens := range tokens {
		if usdc, ok := chainTokens["USDC"]; ok {
			result[chainID] = usdc.Address
		}
	}
	return result
}

type PriceSource uint8

const (
	PriceSourceMempool    PriceSource = iota
	PriceSourceFlashblock
	PriceSourceConfirmed
	PriceSourceRPC
)

func (ps PriceSource) String() string {
	switch ps {
	case PriceSourceMempool:
		return "mempool"
	case PriceSourceFlashblock:
		return "flashblock"
	case PriceSourceConfirmed:
		return "confirmed"
	case PriceSourceRPC:
		return "rpc"
	default:
		return "unknown"
	}
}

type PricePoint struct {
	Pair        TokenPair
	ChainID     uint64
	DEX         string
	PoolAddress common.Address
	Price       *big.Float
	Liquidity   *big.Int
	Timestamp   time.Time
	BlockNumber uint64
	TxHash      common.Hash
	Source      PriceSource
}

type SwapEvent struct {
	ChainID     uint64
	DEX         string
	Pool        common.Address
	Sender      common.Address
	Token0In    *big.Int
	Token1In    *big.Int
	Token0Out   *big.Int
	Token1Out   *big.Int
	TxHash      common.Hash
	BlockNumber uint64
	Timestamp   time.Time
	Source      PriceSource
}

type OpportunityType uint8

const (
	OpTypeSameChainDEX    OpportunityType = iota
	OpTypeCrossChainSame
	OpTypeCrossChainPath
	OpTypeFlashblockFront
	OpTypeTriangular
	OpTypeMultiHop
)

func (ot OpportunityType) String() string {
	switch ot {
	case OpTypeSameChainDEX:
		return "same_chain_dex"
	case OpTypeCrossChainSame:
		return "cross_chain_same"
	case OpTypeCrossChainPath:
		return "cross_chain_path"
	case OpTypeFlashblockFront:
		return "flashblock_front"
	case OpTypeTriangular:
		return "triangular"
	case OpTypeMultiHop:
		return "multi_hop"
	default:
		return "unknown"
	}
}

type OpportunityStatus uint8

const (
	StatusDetected OpportunityStatus = iota
	StatusScored
	StatusValidated
	StatusQueued
	StatusExecuting
	StatusCompleted
	StatusFailed
	StatusExpired
)

type ActionType uint8

const (
	ActionSwap ActionType = iota
	ActionFlashBorrow
	ActionFlashRepay
	ActionTransfer
)

type PathStep struct {
	ChainID      uint64
	Action       ActionType
	DEX          string
	Pool         common.Address
	TokenIn      common.Address
	TokenOut     common.Address
	AmountIn     *big.Int
	MinAmountOut *big.Int
}

type PressureMetrics = quantum.PressureMetrics

type ArbitrageOpportunity struct {
	ID     [32]byte
	Type   OpportunityType
	Status OpportunityStatus
	Path   []PathStep

	BuyPrice    *big.Float
	SellPrice   *big.Float
	SpreadBPS   int64

	BuyChain     uint64
	SellChain    uint64
	IsCrossChain bool

	InputAmount    *big.Int
	ExpectedOutput *big.Int
	ExpectedProfit *big.Int
	GasEstimate    *big.Int
	NetProfit      *big.Int

	Pressure PressureMetrics
	Score    float64

	DetectedAt      time.Time
	ExpiresAt       time.Time
	FlashblockSlot  uint64
	ExecutionWindow time.Duration
}

func (ao *ArbitrageOpportunity) ComputeID() {
	data := make([]byte, 0, 128)
	data = append(data, byte(ao.Type))
	data = append(data, byte(ao.BuyChain))
	data = append(data, byte(ao.SellChain))
	if len(ao.Path) > 0 {
		data = append(data, ao.Path[0].TokenIn.Bytes()...)
		data = append(data, ao.Path[len(ao.Path)-1].TokenOut.Bytes()...)
	}
	if ao.InputAmount != nil {
		data = append(data, ao.InputAmount.Bytes()...)
	}
	data = append(data, byte(ao.DetectedAt.UnixNano()>>32))
	ao.ID = crypto.Keccak256Hash(data)
}

type ExecutionResult struct {
	Opportunity  *ArbitrageOpportunity
	Success      bool
	TxHashes     map[uint64]common.Hash
	ActualProfit *big.Int
	GasUsed      map[uint64]*big.Int
	Error        error
	ExecutedAt   time.Time
	Latency      time.Duration
}

type Position struct {
	ChainID       uint64
	Token         common.Address
	Symbol        string
	Amount        *big.Int
	Available     *big.Int
	Locked        *big.Int
	LastRebalance time.Time
	mu            sync.RWMutex
}

func (p *Position) Lock(amount *big.Int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Available.Cmp(amount) < 0 {
		return false
	}
	p.Available.Sub(p.Available, amount)
	p.Locked.Add(p.Locked, amount)
	return true
}

func (p *Position) Unlock(amount *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Locked.Sub(p.Locked, amount)
	p.Available.Add(p.Available, amount)
}

func (p *Position) UpdateBalance(delta *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Amount.Add(p.Amount, delta)
	p.Available.Add(p.Available, delta)
}

type FlashblockData struct {
	Slot         uint64
	Timestamp    time.Time
	Transactions []common.Hash
	SwapEvents   []SwapEvent
	ValidUntil   time.Time
	BlockBuilder string
}

type ArbConfig struct {

	EnabledChains []uint64

	MinSpreadBPS   int64
	MinProfitWei   *big.Int
	MaxGasGwei     int64
	MaxSlippageBPS int64

	ShotgunEnabled   bool
	ShotgunThreshold float64
	ShotgunPoolSize  int

	InitialPositions   map[uint64]map[common.Address]*big.Int
	RebalanceThreshold float64

	MaxConcurrentTrades int
	ExecutionTimeout    time.Duration
	UseFlashLoans       bool

	MetricsEnabled bool
	MetricsAddr    string
}

func DefaultArbConfig() *ArbConfig {
	return &ArbConfig{
		EnabledChains:       []uint64{ChainIDEthereum, ChainIDBase, ChainIDOptimism},
		MinSpreadBPS:        10,
		MinProfitWei:        big.NewInt(1e16),
		MaxGasGwei:          50,
		MaxSlippageBPS:      50,
		ShotgunEnabled:      true,
		ShotgunThreshold:    0.7,
		ShotgunPoolSize:     1000,
		RebalanceThreshold:  0.2,
		MaxConcurrentTrades: 3,
		ExecutionTimeout:    30 * time.Second,
		UseFlashLoans:       true,
		MetricsEnabled:      true,
		MetricsAddr:         ":9090",
	}
}
