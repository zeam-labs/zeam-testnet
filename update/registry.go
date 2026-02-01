package update

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type RegistryClient struct {
	client          *ethclient.Client
	registryAddress common.Address
}

func NewRegistryClient(rpcURL string, registryAddress common.Address) (*RegistryClient, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	return &RegistryClient{
		client:          client,
		registryAddress: registryAddress,
	}, nil
}

func (rc *RegistryClient) GetManifestHash(ctx context.Context) ([32]byte, error) {

	selector := crypto.Keccak256([]byte("manifestHash()"))[:4]

	result, err := rc.client.CallContract(ctx, ethereum.CallMsg{
		To:   &rc.registryAddress,
		Data: selector,
	}, nil)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to call manifestHash: %w", err)
	}

	var hash [32]byte
	if len(result) >= 32 {
		copy(hash[:], result[:32])
	}
	return hash, nil
}

func (rc *RegistryClient) GetLastUpdated(ctx context.Context) (uint64, error) {
	selector := crypto.Keccak256([]byte("lastUpdated()"))[:4]

	result, err := rc.client.CallContract(ctx, ethereum.CallMsg{
		To:   &rc.registryAddress,
		Data: selector,
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to call lastUpdated: %w", err)
	}

	if len(result) < 32 {
		return 0, nil
	}

	return new(big.Int).SetBytes(result).Uint64(), nil
}

func (rc *RegistryClient) GetInfo(ctx context.Context) (manifestHash [32]byte, lastUpdated uint64, owner common.Address, err error) {
	selector := crypto.Keccak256([]byte("getInfo()"))[:4]

	result, err := rc.client.CallContract(ctx, ethereum.CallMsg{
		To:   &rc.registryAddress,
		Data: selector,
	}, nil)
	if err != nil {
		return [32]byte{}, 0, common.Address{}, fmt.Errorf("failed to call getInfo: %w", err)
	}

	if len(result) < 96 {
		return [32]byte{}, 0, common.Address{}, fmt.Errorf("unexpected result length: %d", len(result))
	}

	copy(manifestHash[:], result[0:32])
	lastUpdated = new(big.Int).SetBytes(result[32:64]).Uint64()
	copy(owner[:], result[76:96])

	return manifestHash, lastUpdated, owner, nil
}

func (rc *RegistryClient) Close() {
	if rc.client != nil {
		rc.client.Close()
	}
}
