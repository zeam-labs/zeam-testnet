package node

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const (

	ChainIDEthereum uint64 = 1
	ChainIDSepolia  uint64 = 11155111

	ChainIDOptimism  uint64 = 10
	ChainIDBase      uint64 = 8453
	ChainIDZora      uint64 = 7777777
	ChainIDMode      uint64 = 34443
	ChainIDFraxtal   uint64 = 252
	ChainIDWorldcoin uint64 = 480
	ChainIDCyber     uint64 = 7560
	ChainIDMint      uint64 = 185
	ChainIDRedstone  uint64 = 690
	ChainIDLisk      uint64 = 1135

	ChainIDBaseSep  uint64 = 84532
	ChainIDOptimSep uint64 = 11155420
	ChainIDZoraSep  uint64 = 999999999
	ChainIDModeSep  uint64 = 919

	ChainIDArbitrum uint64 = 42161
)

type ChainConfig struct {
	ChainID        uint64
	NetworkID      uint64
	Name           string
	RPCURL         string
	WSURL          string
	GenesisHash    common.Hash
	FlashLoanPool  common.Address
	Bootnodes      []string
	BlockTime      time.Duration
	FlashblocksURL string
	IsL2           bool
}

func IsOPStackL2(chainID uint64) bool {
	switch chainID {

	case ChainIDOptimism, ChainIDBase, ChainIDZora, ChainIDMode,
		ChainIDFraxtal, ChainIDWorldcoin, ChainIDCyber, ChainIDMint,
		ChainIDRedstone, ChainIDLisk:
		return true

	case ChainIDBaseSep, ChainIDOptimSep, ChainIDZoraSep, ChainIDModeSep:
		return true
	default:
		return false
	}
}

func DefaultChainConfigs() map[uint64]*ChainConfig {
	return map[uint64]*ChainConfig{
		ChainIDEthereum: {
			ChainID:       ChainIDEthereum,
			NetworkID:     1,
			Name:          "Ethereum",
			GenesisHash:   common.HexToHash("0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3"),
			FlashLoanPool: common.HexToAddress("0x87870Bca3F3fD6335C3F4ce8392D69350B4fA4E2"),
			BlockTime:     12 * time.Second,
			IsL2:          false,
		},
		ChainIDBase: {
			ChainID:        ChainIDBase,
			NetworkID:      8453,
			Name:           "Base",
			GenesisHash:    common.HexToHash("0xf712aa9241cc24369b143cf6dce85f0902a9731e70d66818a3a5845b296c73dd"),
			FlashLoanPool:  common.HexToAddress("0xA238Dd80C259a72e81d7e4664a9801593F98d1c5"),
			BlockTime:      2 * time.Second,
			FlashblocksURL: "wss://flashblocks.base.org/ws",
			IsL2:           true,
		},
		ChainIDOptimism: {
			ChainID:       ChainIDOptimism,
			NetworkID:     10,
			Name:          "Optimism",
			GenesisHash:   common.HexToHash("0x7ca38a1916c42007829c55e69d3e9a73265554b586a499015373241b8a3fa48b"),
			FlashLoanPool: common.HexToAddress("0x794a61358D6845594F94dc1DB02A252b5b4814aD"),
			BlockTime:     2 * time.Second,
			IsL2:          true,
		},
		ChainIDZora: {
			ChainID:   ChainIDZora,
			NetworkID: 7777777,
			Name:      "Zora",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
		ChainIDMode: {
			ChainID:   ChainIDMode,
			NetworkID: 34443,
			Name:      "Mode",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
		ChainIDFraxtal: {
			ChainID:   ChainIDFraxtal,
			NetworkID: 252,
			Name:      "Fraxtal",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
		ChainIDWorldcoin: {
			ChainID:   ChainIDWorldcoin,
			NetworkID: 480,
			Name:      "Worldchain",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
		ChainIDCyber: {
			ChainID:   ChainIDCyber,
			NetworkID: 7560,
			Name:      "Cyber",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
		ChainIDMint: {
			ChainID:   ChainIDMint,
			NetworkID: 185,
			Name:      "Mint",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
		ChainIDRedstone: {
			ChainID:   ChainIDRedstone,
			NetworkID: 690,
			Name:      "Redstone",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
		ChainIDLisk: {
			ChainID:   ChainIDLisk,
			NetworkID: 1135,
			Name:      "Lisk",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
	}
}

func TestnetChainConfigs() map[uint64]*ChainConfig {
	return map[uint64]*ChainConfig{
		ChainIDSepolia: {
			ChainID:       ChainIDSepolia,
			NetworkID:     11155111,
			Name:          "Sepolia",
			GenesisHash:   common.HexToHash("0x25a5cc106eea7138acab33231d7160d69cb777ee0c2c553fcddf5138993e6dd9"),
			FlashLoanPool: common.HexToAddress("0x6Ae43d3271ff6888e7Fc43Fd7321a503ff738951"),
			BlockTime:     12 * time.Second,
			IsL2:          false,
		},
		ChainIDBaseSep: {
			ChainID:        ChainIDBaseSep,
			NetworkID:      84532,
			Name:           "Base Sepolia",
			GenesisHash:    common.HexToHash("0x0dcc9e089e30b90ddfc55be9a37dd15bc551aeee999d2e2b51571f5961453e88"),
			FlashLoanPool:  common.HexToAddress("0x07eA79F68B2B3df564D0A34F8e19D9B1e339814b"),
			BlockTime:      2 * time.Second,
			FlashblocksURL: "wss://flashblocks.base-sepolia.org/ws",
			IsL2:           true,
		},
		ChainIDOptimSep: {
			ChainID:       ChainIDOptimSep,
			NetworkID:     11155420,
			Name:          "Optimism Sepolia",
			GenesisHash:   common.HexToHash("0x102de6ffb001480cc9b8b548fd05c34cd4f46ae4aa91759393db90ea0409887d"),
			FlashLoanPool: common.HexToAddress("0xb50201558B00496A145fE76f7424749556E326D8"),
			BlockTime:     2 * time.Second,
			IsL2:          true,
		},
		ChainIDZoraSep: {
			ChainID:   ChainIDZoraSep,
			NetworkID: 999999999,
			Name:      "Zora Sepolia",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
		ChainIDModeSep: {
			ChainID:   ChainIDModeSep,
			NetworkID: 919,
			Name:      "Mode Sepolia",
			BlockTime: 2 * time.Second,
			IsL2:      true,
		},
	}
}
