package graph

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type TokenNode struct {
	Address  common.Address
	ChainID  uint64
	Symbol   string
	Decimals uint8

	IsStable    bool
	IsNative    bool
	IsBluechip  bool

	BridgedTo map[uint64]common.Address

	EdgeCount  int
	Importance float64
}

type PoolEdge struct {

	Pool    common.Address
	ChainID uint64
	DEX     string

	Token0 common.Address
	Token1 common.Address

	Weight01 float64
	Weight10 float64

	Reserve0    *big.Int
	Reserve1    *big.Int
	MaxAmount01 *big.Int
	MaxAmount10 *big.Int

	Fee         uint32
	TickSpacing int
	Liquidity   *big.Int

	LastUpdate time.Time
	TotalScore float64
}

func (e *PoolEdge) GetWeight(tokenIn common.Address) float64 {
	if tokenIn == e.Token0 {
		return e.Weight01
	}
	return e.Weight10
}

func (e *PoolEdge) GetMaxAmount(tokenIn common.Address) *big.Int {
	if tokenIn == e.Token0 {
		return e.MaxAmount01
	}
	return e.MaxAmount10
}

func (e *PoolEdge) GetTokenOut(tokenIn common.Address) common.Address {
	if tokenIn == e.Token0 {
		return e.Token1
	}
	return e.Token0
}

func (e *PoolEdge) GetReserveIn(tokenIn common.Address) *big.Int {
	if tokenIn == e.Token0 {
		return e.Reserve0
	}
	return e.Reserve1
}

func (e *PoolEdge) GetReserveOut(tokenIn common.Address) *big.Int {
	if tokenIn == e.Token0 {
		return e.Reserve1
	}
	return e.Reserve0
}

type CrossChainEdge struct {
	Token          common.Address
	SourceChain    uint64
	DestChain      uint64
	SourceAddress  common.Address
	DestAddress    common.Address
	BridgeProtocol string
	BridgeFee      float64
	BridgeTime     time.Duration
	IsEnabled      bool
}

type PathNode struct {
	Token   common.Address
	ChainID uint64
	Edge    *PoolEdge
}

type GraphPath struct {
	Nodes     []PathNode
	TotalWeight float64
	IsCycle   bool
}

func (p *GraphPath) StartToken() common.Address {
	if len(p.Nodes) == 0 {
		return common.Address{}
	}
	return p.Nodes[0].Token
}

func (p *GraphPath) EndToken() common.Address {
	if len(p.Nodes) == 0 {
		return common.Address{}
	}
	return p.Nodes[len(p.Nodes)-1].Token
}

func (p *GraphPath) Length() int {
	if len(p.Nodes) == 0 {
		return 0
	}
	return len(p.Nodes) - 1
}

func (p *GraphPath) IsArbitrage() bool {
	return p.IsCycle && p.TotalWeight < 0
}

func (p *GraphPath) GetEdges() []*PoolEdge {
	edges := make([]*PoolEdge, 0, len(p.Nodes)-1)
	for i := 1; i < len(p.Nodes); i++ {
		if p.Nodes[i].Edge != nil {
			edges = append(edges, p.Nodes[i].Edge)
		}
	}
	return edges
}

type GraphStats struct {
	NodeCount         int
	EdgeCount         int
	NodesByChain      map[uint64]int
	EdgesByChain      map[uint64]int
	EdgesByDEX        map[string]int
	BluechipTokens    int
	CrossChainEdges   int
	LastRebuild       time.Time
	RebuildDuration   time.Duration
}

type GraphConfig struct {

	EnabledChains []uint64

	MinPoolScore      float64
	MinLiquidityUSD   float64
	MaxEdgesPerNode   int

	EnableCrossChain  bool
	CrossChainBridges []CrossChainEdge

	RebuildInterval   time.Duration
}

func DefaultGraphConfig() *GraphConfig {
	return &GraphConfig{
		EnabledChains:    []uint64{1, 8453, 10, 42161},
		MinPoolScore:     0.01,
		MinLiquidityUSD:  1000,
		MaxEdgesPerNode:  100,
		EnableCrossChain: true,
		RebuildInterval:  30 * time.Second,
	}
}

func BluechipTokens() map[uint64][]common.Address {
	return map[uint64][]common.Address{

		1: {
			common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
			common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
			common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
			common.HexToAddress("0x6B175474E89094C44Da98b954EescdeCB5BE3830"),
			common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"),
		},

		8453: {
			common.HexToAddress("0x4200000000000000000000000000000000000006"),
			common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
			common.HexToAddress("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA"),
			common.HexToAddress("0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb"),
		},

		10: {
			common.HexToAddress("0x4200000000000000000000000000000000000006"),
			common.HexToAddress("0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85"),
			common.HexToAddress("0x94b008aA00579c1307B0EF2c499aD98a8ce58e58"),
			common.HexToAddress("0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1"),
		},

		42161: {
			common.HexToAddress("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1"),
			common.HexToAddress("0xaf88d065e77c8cC2239327C5EDb3A432268e5831"),
			common.HexToAddress("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"),
			common.HexToAddress("0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1"),
			common.HexToAddress("0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f"),
		},
	}
}

func StableTokens() map[uint64][]common.Address {
	return map[uint64][]common.Address{

		1: {
			common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"),
			common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
			common.HexToAddress("0x6B175474E89094C44Da98b954EedescdeCB5BE3830"),
			common.HexToAddress("0x4Fabb145d64652a948d72533023f6E7A623C7C53"),
			common.HexToAddress("0x853d955aCEf822Db058eb8505911ED77F175b99e"),
		},

		8453: {
			common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"),
			common.HexToAddress("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA"),
			common.HexToAddress("0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb"),
		},

		10: {
			common.HexToAddress("0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85"),
			common.HexToAddress("0x94b008aA00579c1307B0EF2c499aD98a8ce58e58"),
			common.HexToAddress("0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1"),
		},

		42161: {
			common.HexToAddress("0xaf88d065e77c8cC2239327C5EDb3A432268e5831"),
			common.HexToAddress("0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"),
			common.HexToAddress("0xDA10009cBd5D07dd0CeCc66161FC93D7c9000da1"),
		},
	}
}

func IsBluechip(chainID uint64, token common.Address) bool {
	bluechips := BluechipTokens()[chainID]
	for _, bc := range bluechips {
		if bc == token {
			return true
		}
	}
	return false
}

func IsStable(chainID uint64, token common.Address) bool {
	stables := StableTokens()[chainID]
	for _, s := range stables {
		if s == token {
			return true
		}
	}
	return false
}
