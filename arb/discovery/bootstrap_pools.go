package discovery

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

var (

	WETH_MAINNET  = common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	USDC_MAINNET  = common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	USDT_MAINNET  = common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7")
	DAI_MAINNET   = common.HexToAddress("0x6B175474E89094C44Da98b954EescdeCB5cBF40")
	WBTC_MAINNET  = common.HexToAddress("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599")

	WETH_BASE  = common.HexToAddress("0x4200000000000000000000000000000000000006")
	USDC_BASE  = common.HexToAddress("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913")
	USDbC_BASE = common.HexToAddress("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA")

	WETH_OP = common.HexToAddress("0x4200000000000000000000000000000000000006")
	USDC_OP = common.HexToAddress("0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85")
	OP_OP   = common.HexToAddress("0x4200000000000000000000000000000000000042")

	WETH_ARB = common.HexToAddress("0x82aF49447D8a07e3bd95BD0d56f35241523fBab1")
	USDC_ARB = common.HexToAddress("0xaf88d065e77c8cC2239327C5EDb3A432268e5831")
	ARB_ARB  = common.HexToAddress("0x912CE59144191C1204E64559FE8253a0e49E6548")
)

func BootstrapPools() []*DiscoveredPool {
	now := time.Now()

	return []*DiscoveredPool{

		{
			Address:      common.HexToAddress("0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc"),
			ChainID:      1,
			DEXType:      "uniswap_v2",
			Token0:       USDC_MAINNET,
			Token1:       WETH_MAINNET,
			Fee:          30,
			DiscoveredAt: now,
			LastUpdated:  now,
		},
		{
			Address:      common.HexToAddress("0x0d4a11d5EEaaC28EC3F61d100daF4d40471f1852"),
			ChainID:      1,
			DEXType:      "uniswap_v2",
			Token0:       WETH_MAINNET,
			Token1:       USDT_MAINNET,
			Fee:          30,
			DiscoveredAt: now,
			LastUpdated:  now,
		},
		{
			Address:      common.HexToAddress("0xA478c2975Ab1Ea89e8196811F51A7B7Ade33eB11"),
			ChainID:      1,
			DEXType:      "uniswap_v2",
			Token0:       DAI_MAINNET,
			Token1:       WETH_MAINNET,
			Fee:          30,
			DiscoveredAt: now,
			LastUpdated:  now,
		},
		{
			Address:      common.HexToAddress("0xBb2b8038a1640196FbE3e38816F3e67Cba72D940"),
			ChainID:      1,
			DEXType:      "uniswap_v2",
			Token0:       WBTC_MAINNET,
			Token1:       WETH_MAINNET,
			Fee:          30,
			DiscoveredAt: now,
			LastUpdated:  now,
		},

		{
			Address:      common.HexToAddress("0x397FF1542f962076d0BFE58eA045FfA2d347ACa0"),
			ChainID:      1,
			DEXType:      "sushiswap",
			Token0:       USDC_MAINNET,
			Token1:       WETH_MAINNET,
			Fee:          30,
			DiscoveredAt: now,
			LastUpdated:  now,
		},

		{
			Address:      common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640"),
			ChainID:      1,
			DEXType:      "uniswap_v3",
			Token0:       USDC_MAINNET,
			Token1:       WETH_MAINNET,
			Fee:          500,
			TickSpacing:  10,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
		{
			Address:      common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
			ChainID:      1,
			DEXType:      "uniswap_v3",
			Token0:       USDC_MAINNET,
			Token1:       WETH_MAINNET,
			Fee:          3000,
			TickSpacing:  60,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
		{
			Address:      common.HexToAddress("0x4e68Ccd3E89f51C3074ca5072bbAC773960dFa36"),
			ChainID:      1,
			DEXType:      "uniswap_v3",
			Token0:       WETH_MAINNET,
			Token1:       USDT_MAINNET,
			Fee:          3000,
			TickSpacing:  60,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
		{
			Address:      common.HexToAddress("0xCBCdF9626bC03E24f779434178A73a0B4bad62eD"),
			ChainID:      1,
			DEXType:      "uniswap_v3",
			Token0:       WBTC_MAINNET,
			Token1:       WETH_MAINNET,
			Fee:          3000,
			TickSpacing:  60,
			DiscoveredAt: now,
			LastUpdated:  now,
					},

		{
			Address:      common.HexToAddress("0xd0b53D9277642d899DF5C87A3966A349A798F224"),
			ChainID:      8453,
			DEXType:      "uniswap_v3",
			Token0:       WETH_BASE,
			Token1:       USDC_BASE,
			Fee:          500,
			TickSpacing:  10,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
		{
			Address:      common.HexToAddress("0xcDAC0d6c6C59727a65F871236188350531885C43"),
			ChainID:      8453,
			DEXType:      "uniswap_v3",
			Token0:       WETH_BASE,
			Token1:       USDbC_BASE,
			Fee:          500,
			TickSpacing:  10,
			DiscoveredAt: now,
			LastUpdated:  now,
					},

		{
			Address:      common.HexToAddress("0xB4885Bc63399BF5518b994c1d0C153334Ee579D0"),
			ChainID:      8453,
			DEXType:      "aerodrome",
			Token0:       WETH_BASE,
			Token1:       USDC_BASE,
			Fee:          30,
			Stable:       false,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
		{
			Address:      common.HexToAddress("0x6cDcb1C4A4D1C3C6d054b27AC5B77e89eAFb971d"),
			ChainID:      8453,
			DEXType:      "aerodrome",
			Token0:       USDbC_BASE,
			Token1:       USDC_BASE,
			Fee:          5,
			Stable:       true,
			DiscoveredAt: now,
			LastUpdated:  now,
					},

		{
			Address:      common.HexToAddress("0x85149247691df622eaF1a8Bd0CaFd40BC45154a9"),
			ChainID:      10,
			DEXType:      "uniswap_v3",
			Token0:       WETH_OP,
			Token1:       USDC_OP,
			Fee:          500,
			TickSpacing:  10,
			DiscoveredAt: now,
			LastUpdated:  now,
					},

		{
			Address:      common.HexToAddress("0x0493Bf8b6DBB159Ce2Db2E0E8403E753Abd1235b"),
			ChainID:      10,
			DEXType:      "velodrome_v2",
			Token0:       WETH_OP,
			Token1:       USDC_OP,
			Fee:          30,
			Stable:       false,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
		{
			Address:      common.HexToAddress("0x6387765fFA609aB9A1dA1B16C455548Bfed7CbEA"),
			ChainID:      10,
			DEXType:      "velodrome_v2",
			Token0:       WETH_OP,
			Token1:       OP_OP,
			Fee:          30,
			Stable:       false,
			DiscoveredAt: now,
			LastUpdated:  now,
					},

		{
			Address:      common.HexToAddress("0xC31E54c7a869B9FcBEcc14363CF510d1c41fa443"),
			ChainID:      42161,
			DEXType:      "uniswap_v3",
			Token0:       WETH_ARB,
			Token1:       USDC_ARB,
			Fee:          500,
			TickSpacing:  10,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
		{
			Address:      common.HexToAddress("0xC6962004f452bE9203591991D15f6b388e09E8D0"),
			ChainID:      42161,
			DEXType:      "sushiswap",
			Token0:       WETH_ARB,
			Token1:       USDC_ARB,
			Fee:          30,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
		{
			Address:      common.HexToAddress("0xa6c5C7D189fA4eB5Af8ba34E63dCDD3a635D433f"),
			ChainID:      42161,
			DEXType:      "camelot",
			Token0:       WETH_ARB,
			Token1:       ARB_ARB,
			Fee:          30,
			DiscoveredAt: now,
			LastUpdated:  now,
					},
	}
}

func BootstrapPoolsForChain(chainID uint64) []*DiscoveredPool {
	var pools []*DiscoveredPool
	allPools := append(BootstrapPools(), TestnetBootstrapPools()...)
	for _, pool := range allPools {
		if pool.ChainID == chainID {
			pools = append(pools, pool)
		}
	}
	return pools
}

func TestnetBootstrapPools() []*DiscoveredPool {
	now := time.Now()

	WETH_SEPOLIA := common.HexToAddress("0xfFf9976782d46CC05630D1f6eBAb18b2324d6B14")
	USDC_SEPOLIA := common.HexToAddress("0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238")
	DAI_SEPOLIA := common.HexToAddress("0x68194a729C2450ad26072b3D33ADaCbcef39D574")
	LINK_SEPOLIA := common.HexToAddress("0x779877A7B0D9E8603169DdbD7836e478b4624789")

	WETH_BASE_SEP := common.HexToAddress("0x4200000000000000000000000000000000000006")
	USDC_BASE_SEP := common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e")

	WETH_OP_SEP := common.HexToAddress("0x4200000000000000000000000000000000000006")
	USDC_OP_SEP := common.HexToAddress("0x5fd84259d66Cd46123540766Be93DFE6D43130D7")

	return []*DiscoveredPool{

		{
			Address:     common.HexToAddress("0x0227628f3F023bb0B980b67D528571c95c6DaC1c"),
			ChainID:     11155111,
			DEXType:     "uniswap_v3",
			Token0:      USDC_SEPOLIA,
			Token1:      WETH_SEPOLIA,
			Fee:         3000,
			TickSpacing: 60,
			LastUpdated: now,
		},
		{
			Address:     common.HexToAddress("0x6Ce0896eAE6D4BD668fDe41BB784548fb8F59b50"),
			ChainID:     11155111,
			DEXType:     "uniswap_v3",
			Token0:      DAI_SEPOLIA,
			Token1:      WETH_SEPOLIA,
			Fee:         3000,
			TickSpacing: 60,
			LastUpdated: now,
		},
		{
			Address:     common.HexToAddress("0x1F36fC6E26f5A1B9f2AeDe9A3e7f2D4d8C7e5f6A"),
			ChainID:     11155111,
			DEXType:     "uniswap_v3",
			Token0:      USDC_SEPOLIA,
			Token1:      DAI_SEPOLIA,
			Fee:         500,
			TickSpacing: 10,
			LastUpdated: now,
		},
		{
			Address:     common.HexToAddress("0x45c7aBC18daE7Ba3B18FA138D1aBB2Fce10B8c63"),
			ChainID:     11155111,
			DEXType:     "uniswap_v3",
			Token0:      LINK_SEPOLIA,
			Token1:      WETH_SEPOLIA,
			Fee:         3000,
			TickSpacing: 60,
			LastUpdated: now,
		},

		{
			Address:     common.HexToAddress("0x4c36388bE6F416A29C8d8Ae5C1dB8D9F4B5D6A7C"),
			ChainID:     84532,
			DEXType:     "uniswap_v3",
			Token0:      USDC_BASE_SEP,
			Token1:      WETH_BASE_SEP,
			Fee:         3000,
			TickSpacing: 60,
			LastUpdated: now,
		},

		{
			Address:     common.HexToAddress("0x5D6E7F8A9B0C1D2E3F4A5B6C7D8E9F0A1B2C3D4E"),
			ChainID:     11155420,
			DEXType:     "uniswap_v3",
			Token0:      USDC_OP_SEP,
			Token1:      WETH_OP_SEP,
			Fee:         3000,
			TickSpacing: 60,
			LastUpdated: now,
		},
	}
}

func DefaultReserves() map[common.Address]*big.Int {

	defaultLiquidity := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18))

	reserves := make(map[common.Address]*big.Int)

	reserves[common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640")] = defaultLiquidity

	reserves[common.HexToAddress("0xB4e16d0168e52d35CaCD2c6185b44281Ec28C9Dc")] = defaultLiquidity

	for _, pool := range BootstrapPools() {
		reserves[pool.Address] = defaultLiquidity
	}

	return reserves
}

func LoadBootstrapPools(registry *PoolRegistry) int {

	allPools := append(BootstrapPools(), TestnetBootstrapPools()...)
	count := 0

	placeholderReserve := new(big.Int).Mul(big.NewInt(1000), big.NewInt(1e18))

	for _, pool := range allPools {

		pool.Reserve0 = new(big.Int).Set(placeholderReserve)
		pool.Reserve1 = new(big.Int).Set(placeholderReserve)

		pool.TotalScore = 0.8
		pool.DepthScore = 0.7
		pool.ActivityScore = 0.5

		if err := registry.AddPool(pool); err == nil {
			count++
		}
	}

	return count
}
