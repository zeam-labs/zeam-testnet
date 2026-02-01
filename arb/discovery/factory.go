package discovery

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var (

	TopicPairCreatedV2 = crypto.Keccak256Hash([]byte("PairCreated(address,address,address,uint256)"))

	TopicPoolCreatedV3 = crypto.Keccak256Hash([]byte("PoolCreated(address,address,uint24,int24,address)"))

	TopicPairCreatedVelo = crypto.Keccak256Hash([]byte("PairCreated(address,address,bool,address,uint256)"))
)

func Factories() map[uint64][]FactoryInfo {
	return map[uint64][]FactoryInfo{

		1: {
			{
				Address:    common.HexToAddress("0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f"),
				ChainID:    1,
				DEXType:    "uniswap_v2",
				Version:    2,
				EventTopic: TopicPairCreatedV2,
				EventName:  "PairCreated",
			},
			{
				Address:    common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"),
				ChainID:    1,
				DEXType:    "uniswap_v3",
				Version:    3,
				EventTopic: TopicPoolCreatedV3,
				EventName:  "PoolCreated",
			},
			{
				Address:    common.HexToAddress("0xC0AEe478e3658e2610c5F7A4A2E1777cE9e4f2Ac"),
				ChainID:    1,
				DEXType:    "sushiswap",
				Version:    2,
				EventTopic: TopicPairCreatedV2,
				EventName:  "PairCreated",
			},
			{
				Address:    common.HexToAddress("0xB9fC157394Af804a3578134A6585C0dc9cc990d4"),
				ChainID:    1,
				DEXType:    "pancakeswap_v2",
				Version:    2,
				EventTopic: TopicPairCreatedV2,
				EventName:  "PairCreated",
			},
		},

		8453: {
			{
				Address:    common.HexToAddress("0x33128a8fC17869897dcE68Ed026d694621f6FDfD"),
				ChainID:    8453,
				DEXType:    "uniswap_v3",
				Version:    3,
				EventTopic: TopicPoolCreatedV3,
				EventName:  "PoolCreated",
			},
			{
				Address:    common.HexToAddress("0x8909Dc15e40173Ff4699343b6eB8132c65e18eC6"),
				ChainID:    8453,
				DEXType:    "uniswap_v2",
				Version:    2,
				EventTopic: TopicPairCreatedV2,
				EventName:  "PairCreated",
			},
			{
				Address:    common.HexToAddress("0x420DD381b31aEf6683db6B902084cB0FFECe40Da"),
				ChainID:    8453,
				DEXType:    "aerodrome",
				Version:    2,
				EventTopic: TopicPairCreatedVelo,
				EventName:  "PairCreated",
			},
			{
				Address:    common.HexToAddress("0x02F25e951b16B57c711eeB6BA9E0e5CCDFc9Fd38"),
				ChainID:    8453,
				DEXType:    "sushiswap_v3",
				Version:    3,
				EventTopic: TopicPoolCreatedV3,
				EventName:  "PoolCreated",
			},
		},

		10: {
			{
				Address:    common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"),
				ChainID:    10,
				DEXType:    "uniswap_v3",
				Version:    3,
				EventTopic: TopicPoolCreatedV3,
				EventName:  "PoolCreated",
			},
			{
				Address:    common.HexToAddress("0x0c3c1c532F1e39EdF36BE9Fe0bE1410313E074Bf"),
				ChainID:    10,
				DEXType:    "uniswap_v2",
				Version:    2,
				EventTopic: TopicPairCreatedV2,
				EventName:  "PairCreated",
			},
			{
				Address:    common.HexToAddress("0x25CbdDb98b35ab1FF77413456B31EC81A6B6B746"),
				ChainID:    10,
				DEXType:    "velodrome_v2",
				Version:    2,
				EventTopic: TopicPairCreatedVelo,
				EventName:  "PairCreated",
			},
			{
				Address:    common.HexToAddress("0xF1046053aa5682b4F9a81b5481394DA16BE5FF5a"),
				ChainID:    10,
				DEXType:    "velodrome_cl",
				Version:    3,
				EventTopic: TopicPoolCreatedV3,
				EventName:  "PoolCreated",
			},
		},

		42161: {
			{
				Address:    common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"),
				ChainID:    42161,
				DEXType:    "uniswap_v3",
				Version:    3,
				EventTopic: TopicPoolCreatedV3,
				EventName:  "PoolCreated",
			},
			{
				Address:    common.HexToAddress("0xc35DADB65012eC5796536bD9864eD8773aBc74C4"),
				ChainID:    42161,
				DEXType:    "sushiswap",
				Version:    2,
				EventTopic: TopicPairCreatedV2,
				EventName:  "PairCreated",
			},
			{
				Address:    common.HexToAddress("0x1af415a1EbA07a4986a52B6f2e7dE7003D82231e"),
				ChainID:    42161,
				DEXType:    "camelot",
				Version:    2,
				EventTopic: TopicPairCreatedV2,
				EventName:  "PairCreated",
			},
		},

		11155111: {
			{
				Address:    common.HexToAddress("0x0227628f3F023bb0B980b67D528571c95c6DaC1c"),
				ChainID:    11155111,
				DEXType:    "uniswap_v3",
				Version:    3,
				EventTopic: TopicPoolCreatedV3,
				EventName:  "PoolCreated",
			},
		},
	}
}

type GossipFactoryListener struct {
	mu sync.RWMutex

	factories map[uint64]map[common.Address]*FactoryInfo

	OnPoolDiscovered func(pool *DiscoveredPool)

	factoryCallsSeen uint64
	poolsDiscovered  uint64
	lastDiscovery    time.Time
}

func NewGossipFactoryListener() *GossipFactoryListener {
	gfl := &GossipFactoryListener{
		factories: make(map[uint64]map[common.Address]*FactoryInfo),
	}

	for chainID, chainFactories := range Factories() {
		gfl.factories[chainID] = make(map[common.Address]*FactoryInfo)
		for i := range chainFactories {
			gfl.factories[chainID][chainFactories[i].Address] = &chainFactories[i]
		}
	}

	return gfl
}

func (gfl *GossipFactoryListener) ProcessTransaction(chainID uint64, tx *types.Transaction) {
	if tx.To() == nil {
		return
	}

	to := *tx.To()
	data := tx.Data()

	factory, ok := gfl.factories[chainID][to]
	if !ok {
		return
	}

	if len(data) < 4 {
		return
	}

	gfl.mu.Lock()
	gfl.factoryCallsSeen++
	gfl.mu.Unlock()

	pool := gfl.parseFactoryCalldata(factory, data, tx)
	if pool == nil {
		return
	}

	gfl.mu.Lock()
	gfl.poolsDiscovered++
	gfl.lastDiscovery = time.Now()
	gfl.mu.Unlock()

	if gfl.OnPoolDiscovered != nil {
		gfl.OnPoolDiscovered(pool)
	}
}

func (gfl *GossipFactoryListener) parseFactoryCalldata(factory *FactoryInfo, data []byte, tx *types.Transaction) *DiscoveredPool {
	if len(data) < 68 {
		return nil
	}

	selector := data[:4]

	pool := &DiscoveredPool{
		ChainID:      factory.ChainID,
		DEXType:      factory.DEXType,
		DiscoveredAt: time.Now(),
		LastUpdated:  time.Now(),
	}

	if selector[0] == 0xc9 && selector[1] == 0xc6 && selector[2] == 0x53 && selector[3] == 0x96 {
		pool.Token0 = common.BytesToAddress(data[16:36])
		pool.Token1 = common.BytesToAddress(data[48:68])
		pool.Fee = 30

		pool.Address = ComputePairAddressV2(factory.Address, pool.Token0, pool.Token1)

		return pool
	}

	if selector[0] == 0xa1 && selector[1] == 0x67 && selector[2] == 0x12 && selector[3] == 0x95 && len(data) >= 100 {
		pool.Token0 = common.BytesToAddress(data[16:36])
		pool.Token1 = common.BytesToAddress(data[48:68])
		pool.Fee = uint32(binary.BigEndian.Uint32(append([]byte{0}, data[97:100]...)))

		pool.Address = ComputePoolAddressV3(factory.Address, pool.Token0, pool.Token1, pool.Fee)

		return pool
	}

	return nil
}

func ComputePairAddressV2(factory, token0, token1 common.Address) common.Address {

	if token0.Hex() > token1.Hex() {
		token0, token1 = token1, token0
	}

	salt := crypto.Keccak256(append(token0.Bytes(), token1.Bytes()...))

	initCodeHash := common.FromHex("96e8ac4277198ff8b6f785478aa9a39f403cb768dd02cbee326c3e7da348845f")

	data := append([]byte{0xff}, factory.Bytes()...)
	data = append(data, salt...)
	data = append(data, initCodeHash...)

	return common.BytesToAddress(crypto.Keccak256(data)[12:])
}

func ComputePoolAddressV3(factory, token0, token1 common.Address, fee uint32) common.Address {

	if token0.Hex() > token1.Hex() {
		token0, token1 = token1, token0
	}

	feeBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(feeBytes, fee)

	saltInput := append(token0.Bytes(), token1.Bytes()...)
	saltInput = append(saltInput, make([]byte, 28)...)
	saltInput = append(saltInput, feeBytes...)

	salt := crypto.Keccak256(saltInput)

	initCodeHash := common.FromHex("e34f199b19b2b4f47f68442619d555527d244f78a3297ea89325f843f87b8b54")

	data := append([]byte{0xff}, factory.Bytes()...)
	data = append(data, salt...)
	data = append(data, initCodeHash...)

	return common.BytesToAddress(crypto.Keccak256(data)[12:])
}

func (gfl *GossipFactoryListener) Stats() (calls, pools uint64, lastDiscovery time.Time) {
	gfl.mu.RLock()
	defer gfl.mu.RUnlock()
	return gfl.factoryCallsSeen, gfl.poolsDiscovered, gfl.lastDiscovery
}

func (gfl *GossipFactoryListener) GetFactoryForAddress(chainID uint64, addr common.Address) *FactoryInfo {
	gfl.mu.RLock()
	defer gfl.mu.RUnlock()

	if chainFactories, ok := gfl.factories[chainID]; ok {
		return chainFactories[addr]
	}
	return nil
}

func (gfl *GossipFactoryListener) IsFactory(chainID uint64, addr common.Address) bool {
	return gfl.GetFactoryForAddress(chainID, addr) != nil
}

func parseV2EventFromLog(pool *DiscoveredPool, factory *FactoryInfo, log *types.Log) (*DiscoveredPool, error) {
	if len(log.Topics) < 3 {
		return nil, fmt.Errorf("insufficient topics for V2 event")
	}

	pool.Token0 = common.HexToAddress(log.Topics[1].Hex())
	pool.Token1 = common.HexToAddress(log.Topics[2].Hex())

	if len(log.Data) < 64 {
		return nil, fmt.Errorf("insufficient data for V2 event")
	}

	if factory.DEXType == "aerodrome" || factory.DEXType == "velodrome_v2" {

		if len(log.Data) >= 96 {
			pool.Stable = log.Data[31] == 1
			pool.Address = common.BytesToAddress(log.Data[44:64])
		}
	} else {

		pool.Address = common.BytesToAddress(log.Data[12:32])
	}

	pool.Fee = 30

	return pool, nil
}

func parseV3EventFromLog(pool *DiscoveredPool, factory *FactoryInfo, log *types.Log) (*DiscoveredPool, error) {
	if len(log.Topics) < 4 {
		return nil, fmt.Errorf("insufficient topics for V3 event")
	}

	pool.Token0 = common.HexToAddress(log.Topics[1].Hex())
	pool.Token1 = common.HexToAddress(log.Topics[2].Hex())

	feeBytes := log.Topics[3].Bytes()
	pool.Fee = uint32(binary.BigEndian.Uint32(append([]byte{0}, feeBytes[29:32]...)))

	if len(log.Data) < 64 {
		return nil, fmt.Errorf("insufficient data for V3 event")
	}

	tickBytes := log.Data[0:32]
	tickSpacing := int(int32(binary.BigEndian.Uint32(append([]byte{0}, tickBytes[29:32]...))))
	if tickBytes[28]&0x80 != 0 {
		tickSpacing = -tickSpacing
	}
	pool.TickSpacing = tickSpacing

	pool.Address = common.BytesToAddress(log.Data[44:64])

	return pool, nil
}

func ParsePoolCreatedLog(chainID uint64, log *types.Log) (*DiscoveredPool, error) {
	factories := Factories()
	chainFactories, ok := factories[chainID]
	if !ok {
		return nil, fmt.Errorf("no factories for chain %d", chainID)
	}

	var factory *FactoryInfo
	for i := range chainFactories {
		if chainFactories[i].Address == log.Address {
			factory = &chainFactories[i]
			break
		}
	}
	if factory == nil {
		return nil, fmt.Errorf("unknown factory address: %s", log.Address.Hex())
	}

	pool := &DiscoveredPool{
		ChainID:      chainID,
		DEXType:      factory.DEXType,
		DiscoveredAt: time.Now(),
		LastUpdated:  time.Now(),
		CreatedBlock: log.BlockNumber,
	}

	switch factory.Version {
	case 2:
		return parseV2EventFromLog(pool, factory, log)
	case 3:
		return parseV3EventFromLog(pool, factory, log)
	default:
		return nil, fmt.Errorf("unknown factory version: %d", factory.Version)
	}
}
