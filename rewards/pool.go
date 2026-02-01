package rewards

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type PoolClient struct {
	client      *ethclient.Client
	poolAddress common.Address
	operatorKey *ecdsa.PrivateKey
	chainID     *big.Int
}

func NewPoolClient(rpcURL string, poolAddress common.Address, operatorKey *ecdsa.PrivateKey) (*PoolClient, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	chainID, err := client.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	return &PoolClient{
		client:      client,
		poolAddress: poolAddress,
		operatorKey: operatorKey,
		chainID:     chainID,
	}, nil
}

func (pc *PoolClient) FinalizeEpoch(ctx context.Context, root [32]byte, totalDistributed *big.Int) (*types.Transaction, error) {
	if pc.operatorKey == nil {
		return nil, fmt.Errorf("no operator key configured")
	}

	selector := crypto.Keccak256([]byte("finalizeEpoch(bytes32,uint256)"))[:4]

	data := make([]byte, 4+32+32)
	copy(data[0:4], selector)
	copy(data[4:36], root[:])
	copy(data[36:68], common.LeftPadBytes(totalDistributed.Bytes(), 32))

	operatorAddr := crypto.PubkeyToAddress(pc.operatorKey.PublicKey)
	nonce, err := pc.client.PendingNonceAt(ctx, operatorAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %w", err)
	}

	gasPrice, err := pc.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %w", err)
	}

	tx := types.NewTransaction(
		nonce,
		pc.poolAddress,
		big.NewInt(0),
		200000,
		gasPrice,
		data,
	)

	signer := types.NewEIP155Signer(pc.chainID)
	signedTx, err := types.SignTx(tx, signer, pc.operatorKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	if err := pc.client.SendTransaction(ctx, signedTx); err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	fmt.Printf("[Pool] Submitted epoch root %x, total %s wei, tx %s\n",
		root[:8], totalDistributed, signedTx.Hash().Hex()[:18])

	return signedTx, nil
}

func (pc *PoolClient) GetPendingRevenue(ctx context.Context) (*big.Int, error) {

	selector := crypto.Keccak256([]byte("pendingRevenue()"))[:4]

	result, err := pc.client.CallContract(ctx, toCallMsg(pc.poolAddress, selector), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call pendingRevenue: %w", err)
	}

	if len(result) < 32 {
		return big.NewInt(0), nil
	}

	return new(big.Int).SetBytes(result), nil
}

func (pc *PoolClient) GetCurrentEpoch(ctx context.Context) (uint64, error) {
	selector := crypto.Keccak256([]byte("currentEpoch()"))[:4]

	result, err := pc.client.CallContract(ctx, toCallMsg(pc.poolAddress, selector), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to call currentEpoch: %w", err)
	}

	if len(result) < 32 {
		return 0, nil
	}

	return new(big.Int).SetBytes(result).Uint64(), nil
}

func (pc *PoolClient) GetEpochRoot(ctx context.Context, epoch uint64) ([32]byte, error) {

	selector := crypto.Keccak256([]byte("epochRoots(uint256)"))[:4]
	data := make([]byte, 4+32)
	copy(data[0:4], selector)
	copy(data[4:36], common.LeftPadBytes(big.NewInt(int64(epoch)).Bytes(), 32))

	result, err := pc.client.CallContract(ctx, toCallMsg(pc.poolAddress, data), nil)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to call epochRoots: %w", err)
	}

	var root [32]byte
	if len(result) >= 32 {
		copy(root[:], result[:32])
	}
	return root, nil
}

func BuildClaimData(epoch uint64, amount *big.Int, proof [][]byte) []byte {

	selector := crypto.Keccak256([]byte("claim(uint256,uint256,bytes32[])"))[:4]

	dataSize := 4 + 32 + 32 + 32 + 32 + 32*len(proof)
	data := make([]byte, dataSize)

	offset := 0
	copy(data[offset:offset+4], selector)
	offset += 4

	copy(data[offset:offset+32], common.LeftPadBytes(big.NewInt(int64(epoch)).Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(amount.Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(big.NewInt(96).Bytes(), 32))
	offset += 32

	copy(data[offset:offset+32], common.LeftPadBytes(big.NewInt(int64(len(proof))).Bytes(), 32))
	offset += 32

	for _, p := range proof {
		copy(data[offset:offset+32], common.LeftPadBytes(p, 32))
		offset += 32
	}

	return data
}

func toCallMsg(to common.Address, data []byte) ethereum.CallMsg {
	return ethereum.CallMsg{
		To:   &to,
		Data: data,
	}
}

func (pc *PoolClient) Close() {
	if pc.client != nil {
		pc.client.Close()
	}
}

func (pc *PoolClient) PoolAddress() common.Address {
	return pc.poolAddress
}

var _ bind.ContractBackend = (*poolBackend)(nil)

type poolBackend struct {
	*ethclient.Client
}
