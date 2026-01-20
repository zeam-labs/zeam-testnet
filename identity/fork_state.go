

package identity

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)


type ForkState struct {
	mu sync.RWMutex

	
	ForkID          [32]byte       `json:"fork_id"`
	ChainSalt       []byte         `json:"chain_salt"`
	CreatedAt       int64          `json:"created_at"`
	GenesisBlock    uint64         `json:"genesis_block"`
	GenesisChain    string         `json:"genesis_chain"` 
	GenesisTxHash   common.Hash    `json:"genesis_tx_hash"`

	
	CurrentAddress  common.Address `json:"current_address"`
	RecoveryConfig  RecoveryConfig `json:"recovery_config"`
	LastCheckpoint  *Checkpoint    `json:"last_checkpoint,omitempty"`

	
	LastSyncBlock   uint64         `json:"-"`
	PendingRecovery *PendingRecoveryState `json:"-"`
}


type RecoveryConfig struct {
	
	Guardians        []common.Address `json:"guardians,omitempty"`
	GuardianThreshold int             `json:"guardian_threshold,omitempty"`

	
	TimelockEnabled  bool  `json:"timelock_enabled"`
	TimelockDuration int64 `json:"timelock_duration"` 

	
	UpdatedAt        int64          `json:"updated_at"`
	UpdatedBy        common.Address `json:"updated_by"`
}


type Checkpoint struct {
	Epoch           uint64      `json:"epoch"`
	StateRoot       [32]byte    `json:"state_root"`
	Timestamp       int64       `json:"timestamp"`
	BlockNumber     uint64      `json:"block_number"`
	PreviousCheckpoint common.Hash `json:"previous_checkpoint,omitempty"`
	Signature       []byte      `json:"signature"`
}


type PendingRecoveryState struct {
	NewAddress      common.Address   `json:"new_address"`
	NewChainSalt    []byte           `json:"new_chain_salt"`
	InitiatedAt     int64            `json:"initiated_at"`
	InitiatedBy     common.Address   `json:"initiated_by"`
	Method          string           `json:"method"` 
	Approvals       []RecoveryApproval `json:"approvals,omitempty"`
	CanCompleteAt   int64            `json:"can_complete_at"` 
	TxHash          common.Hash      `json:"tx_hash"`
}


type ForkGenesis struct {
	Version         int            `json:"version"`
	Type            string         `json:"type"` 
	ForkID          [32]byte       `json:"fork_id"`
	ChainSalt       []byte         `json:"chain_salt"`
	InitialAddress  common.Address `json:"initial_address"`
	CreatedAt       int64          `json:"created_at"`
	RecoveryConfig  RecoveryConfig `json:"recovery_config"`
	Signature       []byte         `json:"signature"`
}


func NewForkGenesis(identity *StatelessIdentity, config RecoveryConfig) (*ForkGenesis, error) {
	if identity == nil || !identity.IsValid() {
		return nil, fmt.Errorf("valid identity required")
	}

	now := time.Now().Unix()
	config.UpdatedAt = now
	config.UpdatedBy = identity.Address()

	genesis := &ForkGenesis{
		Version:        1,
		Type:           "ZEAM_FORK_GENESIS",
		ForkID:         identity.ForkID(),
		ChainSalt:      identity.ChainSalt(),
		InitialAddress: identity.Address(),
		CreatedAt:      now,
		RecoveryConfig: config,
	}

	
	data, err := json.Marshal(genesis)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal genesis: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := identity.Sign(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to sign genesis: %w", err)
	}

	genesis.Signature = sig
	return genesis, nil
}


func (fg *ForkGenesis) Verify() (common.Address, bool) {
	sig := fg.Signature
	fg.Signature = nil

	data, err := json.Marshal(fg)
	fg.Signature = sig
	if err != nil {
		return common.Address{}, false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return common.Address{}, false
	}

	signer := crypto.PubkeyToAddress(*pubKey)
	return signer, signer == fg.InitialAddress
}


func (fg *ForkGenesis) ToBytes() ([]byte, error) {
	return json.Marshal(fg)
}


func ForkGenesisFromBytes(data []byte) (*ForkGenesis, error) {
	var fg ForkGenesis
	if err := json.Unmarshal(data, &fg); err != nil {
		return nil, err
	}
	return &fg, nil
}


func ForkStateFromGenesis(genesis *ForkGenesis, blockNumber uint64, txHash common.Hash, chainID string) (*ForkState, error) {
	signer, valid := genesis.Verify()
	if !valid {
		return nil, fmt.Errorf("invalid genesis signature")
	}
	if signer != genesis.InitialAddress {
		return nil, fmt.Errorf("genesis signer mismatch")
	}

	return &ForkState{
		ForkID:         genesis.ForkID,
		ChainSalt:      genesis.ChainSalt,
		CreatedAt:      genesis.CreatedAt,
		GenesisBlock:   blockNumber,
		GenesisChain:   chainID,
		GenesisTxHash:  txHash,
		CurrentAddress: genesis.InitialAddress,
		RecoveryConfig: genesis.RecoveryConfig,
		LastSyncBlock:  blockNumber,
	}, nil
}


type StateTransition struct {
	Version   int            `json:"version"`
	Type      TransitionType `json:"type"`
	ForkID    [32]byte       `json:"fork_id"`
	From      common.Address `json:"from"`
	Timestamp int64          `json:"timestamp"`
	Nonce     uint64         `json:"nonce"`
	Data      json.RawMessage `json:"data"`
	Signature []byte         `json:"signature"`
}


type TransitionType string

const (
	TransitionCheckpoint       TransitionType = "checkpoint"
	TransitionRecoveryConfig   TransitionType = "recovery_config"
	TransitionRecoveryInitiate TransitionType = "recovery_initiate"
	TransitionRecoveryApprove  TransitionType = "recovery_approve"
	TransitionRecoveryComplete TransitionType = "recovery_complete"
	TransitionRecoveryCancel   TransitionType = "recovery_cancel"
	TransitionMemoryWrite      TransitionType = "memory_write"
	TransitionConsentGrant     TransitionType = "consent_grant"
	TransitionConsentRevoke    TransitionType = "consent_revoke"
)


func NewStateTransition(
	identity *StatelessIdentity,
	txType TransitionType,
	data interface{},
	nonce uint64,
) (*StateTransition, error) {
	if identity == nil || !identity.IsValid() {
		return nil, fmt.Errorf("valid identity required")
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	tx := &StateTransition{
		Version:   1,
		Type:      txType,
		ForkID:    identity.ForkID(),
		From:      identity.Address(),
		Timestamp: time.Now().Unix(),
		Nonce:     nonce,
		Data:      dataBytes,
	}

	
	sigData, err := json.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(sigData)
	sig, err := identity.Sign(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	tx.Signature = sig
	return tx, nil
}


func (st *StateTransition) Verify() (common.Address, bool) {
	sig := st.Signature
	st.Signature = nil

	data, err := json.Marshal(st)
	st.Signature = sig
	if err != nil {
		return common.Address{}, false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return common.Address{}, false
	}

	signer := crypto.PubkeyToAddress(*pubKey)
	return signer, signer == st.From
}


func (st *StateTransition) ToBytes() ([]byte, error) {
	return json.Marshal(st)
}


func StateTransitionFromBytes(data []byte) (*StateTransition, error) {
	var st StateTransition
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}


func (fs *ForkState) ApplyTransition(tx *StateTransition) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	
	signer, valid := tx.Verify()
	if !valid {
		return fmt.Errorf("invalid transition signature")
	}

	
	if tx.ForkID != fs.ForkID {
		return fmt.Errorf("fork ID mismatch")
	}

	
	if !fs.isAuthorized(signer, tx.Type) {
		return fmt.Errorf("signer %s not authorized for %s", signer.Hex(), tx.Type)
	}

	
	switch tx.Type {
	case TransitionCheckpoint:
		return fs.applyCheckpoint(tx.Data)

	case TransitionRecoveryConfig:
		return fs.applyRecoveryConfig(tx.Data, signer)

	case TransitionRecoveryInitiate:
		return fs.applyRecoveryInitiate(tx.Data, signer)

	case TransitionRecoveryApprove:
		return fs.applyRecoveryApprove(tx.Data, signer)

	case TransitionRecoveryComplete:
		return fs.applyRecoveryComplete(tx.Data, signer)

	case TransitionRecoveryCancel:
		return fs.applyRecoveryCancel(signer)

	default:
		
		return nil
	}
}

func (fs *ForkState) isAuthorized(signer common.Address, txType TransitionType) bool {
	
	if signer == fs.CurrentAddress {
		return true
	}

	
	if txType == TransitionRecoveryApprove {
		for _, g := range fs.RecoveryConfig.Guardians {
			if g == signer {
				return true
			}
		}
	}

	
	if txType == TransitionRecoveryInitiate {
		return fs.RecoveryConfig.TimelockEnabled
	}

	return false
}

func (fs *ForkState) applyCheckpoint(data json.RawMessage) error {
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return err
	}
	fs.LastCheckpoint = &cp
	return nil
}

func (fs *ForkState) applyRecoveryConfig(data json.RawMessage, signer common.Address) error {
	var config RecoveryConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	config.UpdatedAt = time.Now().Unix()
	config.UpdatedBy = signer
	fs.RecoveryConfig = config
	return nil
}

func (fs *ForkState) applyRecoveryInitiate(data json.RawMessage, signer common.Address) error {
	var pending PendingRecoveryState
	if err := json.Unmarshal(data, &pending); err != nil {
		return err
	}
	pending.InitiatedAt = time.Now().Unix()
	pending.InitiatedBy = signer

	if pending.Method == "timelock" {
		pending.CanCompleteAt = pending.InitiatedAt + fs.RecoveryConfig.TimelockDuration
	}

	fs.PendingRecovery = &pending
	return nil
}

func (fs *ForkState) applyRecoveryApprove(data json.RawMessage, signer common.Address) error {
	if fs.PendingRecovery == nil {
		return fmt.Errorf("no pending recovery")
	}

	var approval RecoveryApproval
	if err := json.Unmarshal(data, &approval); err != nil {
		return err
	}

	
	isGuardian := false
	for _, g := range fs.RecoveryConfig.Guardians {
		if g == signer {
			isGuardian = true
			break
		}
	}
	if !isGuardian {
		return fmt.Errorf("signer is not a guardian")
	}

	
	for _, a := range fs.PendingRecovery.Approvals {
		if a.Approver == signer {
			return fmt.Errorf("already approved")
		}
	}

	approval.Approver = signer
	approval.ApprovedAt = time.Now().Unix()
	fs.PendingRecovery.Approvals = append(fs.PendingRecovery.Approvals, approval)

	
	if len(fs.PendingRecovery.Approvals) >= fs.RecoveryConfig.GuardianThreshold {
		fs.PendingRecovery.Method = "social"
		fs.PendingRecovery.CanCompleteAt = time.Now().Unix() 
	}

	return nil
}

func (fs *ForkState) applyRecoveryComplete(data json.RawMessage, signer common.Address) error {
	if fs.PendingRecovery == nil {
		return fmt.Errorf("no pending recovery")
	}

	now := time.Now().Unix()
	if now < fs.PendingRecovery.CanCompleteAt {
		return fmt.Errorf("recovery not yet completable")
	}

	
	if signer != fs.PendingRecovery.NewAddress {
		return fmt.Errorf("signer must be the new address")
	}

	
	fs.CurrentAddress = fs.PendingRecovery.NewAddress
	if len(fs.PendingRecovery.NewChainSalt) > 0 {
		fs.ChainSalt = fs.PendingRecovery.NewChainSalt
	}
	fs.PendingRecovery = nil

	return nil
}

func (fs *ForkState) applyRecoveryCancel(signer common.Address) error {
	if fs.PendingRecovery == nil {
		return fmt.Errorf("no pending recovery")
	}

	
	if signer != fs.CurrentAddress {
		return fmt.Errorf("only current owner can cancel recovery")
	}

	fs.PendingRecovery = nil
	return nil
}


func (fs *ForkState) CanRecover(method string) (bool, string) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	switch method {
	case "social":
		if len(fs.RecoveryConfig.Guardians) == 0 {
			return false, "no guardians configured"
		}
		if fs.RecoveryConfig.GuardianThreshold == 0 {
			return false, "no guardian threshold set"
		}
		return true, ""

	case "timelock":
		if !fs.RecoveryConfig.TimelockEnabled {
			return false, "timelock recovery not enabled"
		}
		if fs.RecoveryConfig.TimelockDuration == 0 {
			return false, "no timelock duration set"
		}
		return true, ""

	default:
		return false, "unknown recovery method"
	}
}


func (fs *ForkState) HasPendingRecovery() bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.PendingRecovery != nil
}


func (fs *ForkState) GetPendingRecovery() *PendingRecoveryState {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	if fs.PendingRecovery == nil {
		return nil
	}
	
	copy := *fs.PendingRecovery
	return &copy
}


func (fs *ForkState) IsOwner(addr common.Address) bool {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return addr == fs.CurrentAddress
}


func (fs *ForkState) GetCurrentAddress() common.Address {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.CurrentAddress
}


func (fs *ForkState) GetChainSalt() []byte {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	result := make([]byte, len(fs.ChainSalt))
	copy(result, fs.ChainSalt)
	return result
}
