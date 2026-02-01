package identity

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type RecoveryState string

const (

	RecoveryInitiated RecoveryState = "initiated"

	RecoveryPending RecoveryState = "pending"

	RecoveryApproved RecoveryState = "approved"

	RecoveryExecuted RecoveryState = "executed"

	RecoveryCancelled RecoveryState = "cancelled"

	RecoveryFailed RecoveryState = "failed"
)

type CrossForkRecovery struct {

	ID [32]byte `json:"id"`

	SourceForkID [32]byte `json:"source_fork_id"`

	TargetForkID [32]byte `json:"target_fork_id"`

	SourceAddress common.Address `json:"source_address"`

	TargetAddress common.Address `json:"target_address"`

	State RecoveryState `json:"state"`

	Method RecoveryMethod `json:"method"`

	InitiatedAt int64 `json:"initiated_at"`

	CompletedAt int64 `json:"completed_at,omitempty"`

	TimeLockExpiry int64 `json:"time_lock_expiry,omitempty"`

	Approvals []RecoveryApproval `json:"approvals,omitempty"`

	RequiredApprovals int `json:"required_approvals,omitempty"`

	RecoveryData *RecoveryData `json:"recovery_data"`

	StreamsToRecover [][32]byte `json:"streams_to_recover,omitempty"`

	TargetSignature []byte `json:"target_signature"`
}

type RecoveryApproval struct {

	Approver common.Address `json:"approver"`

	ApprovedAt int64 `json:"approved_at"`

	Signature []byte `json:"signature"`

	Comment string `json:"comment,omitempty"`
}

type RecoveryData struct {

	PhraseHash []byte `json:"phrase_hash,omitempty"`

	Guardians       []common.Address `json:"guardians,omitempty"`
	GuardianQuorum  int              `json:"guardian_quorum,omitempty"`

	CheckpointHash  [32]byte `json:"checkpoint_hash,omitempty"`
	CheckpointEpoch int64    `json:"checkpoint_epoch,omitempty"`

	SourceNetworkID string `json:"source_network_id,omitempty"`
	TargetNetworkID string `json:"target_network_id,omitempty"`
	MigrationProof  []byte `json:"migration_proof,omitempty"`
}

func (cfr *CrossForkRecovery) GenerateID() {
	data, _ := json.Marshal(struct {
		SourceForkID  [32]byte
		TargetForkID  [32]byte
		SourceAddress common.Address
		TargetAddress common.Address
		InitiatedAt   int64
	}{cfr.SourceForkID, cfr.TargetForkID, cfr.SourceAddress, cfr.TargetAddress, cfr.InitiatedAt})

	cfr.ID = crypto.Keccak256Hash(data)
}

func (cfr *CrossForkRecovery) SignWithTarget(privateKey *ecdsa.PrivateKey) error {
	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	if addr != cfr.TargetAddress {
		return fmt.Errorf("signer %s is not target address %s", addr.Hex(), cfr.TargetAddress.Hex())
	}

	cfr.TargetSignature = nil
	data, err := json.Marshal(cfr)
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	cfr.TargetSignature = sig
	return nil
}

func (cfr *CrossForkRecovery) Verify() bool {
	if len(cfr.TargetSignature) == 0 {
		return false
	}

	sig := cfr.TargetSignature
	cfr.TargetSignature = nil
	data, err := json.Marshal(cfr)
	cfr.TargetSignature = sig
	if err != nil {
		return false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false
	}

	return crypto.PubkeyToAddress(*pubKey) == cfr.TargetAddress
}

func (cfr *CrossForkRecovery) AddApproval(approver common.Address, signature []byte, comment string) error {

	data, _ := json.Marshal(struct {
		RecoveryID [32]byte
		Approver   common.Address
		Approve    bool
	}{cfr.ID, approver, true})

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, signature)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	if crypto.PubkeyToAddress(*pubKey) != approver {
		return fmt.Errorf("signature does not match approver")
	}

	isGuardian := false
	if cfr.RecoveryData != nil {
		for _, g := range cfr.RecoveryData.Guardians {
			if g == approver {
				isGuardian = true
				break
			}
		}
	}
	if !isGuardian {
		return fmt.Errorf("approver is not a guardian")
	}

	for _, a := range cfr.Approvals {
		if a.Approver == approver {
			return fmt.Errorf("already approved by this guardian")
		}
	}

	cfr.Approvals = append(cfr.Approvals, RecoveryApproval{
		Approver:   approver,
		ApprovedAt: time.Now().Unix(),
		Signature:  signature,
		Comment:    comment,
	})

	if len(cfr.Approvals) >= cfr.RequiredApprovals {
		cfr.State = RecoveryApproved
	}

	return nil
}

func (cfr *CrossForkRecovery) CanExecute() (bool, string) {
	if cfr.State == RecoveryExecuted {
		return false, "already executed"
	}

	if cfr.State == RecoveryCancelled {
		return false, "cancelled"
	}

	if cfr.State == RecoveryFailed {
		return false, "failed"
	}

	now := time.Now().Unix()

	switch cfr.Method {
	case RecoveryTimeLock:
		if now < cfr.TimeLockExpiry {
			return false, fmt.Sprintf("time lock expires in %d seconds", cfr.TimeLockExpiry-now)
		}
		return true, ""

	case RecoverySocial:
		if len(cfr.Approvals) < cfr.RequiredApprovals {
			return false, fmt.Sprintf("need %d more approvals", cfr.RequiredApprovals-len(cfr.Approvals))
		}
		return true, ""

	case RecoveryPhrase:

		if cfr.State == RecoveryApproved || cfr.State == RecoveryPending {
			return true, ""
		}
		return false, "not approved"

	default:
		return false, "unknown recovery method"
	}
}

func (cfr *CrossForkRecovery) Execute() error {
	canExecute, reason := cfr.CanExecute()
	if !canExecute {
		return fmt.Errorf("cannot execute: %s", reason)
	}

	cfr.State = RecoveryExecuted
	cfr.CompletedAt = time.Now().Unix()
	return nil
}

func (cfr *CrossForkRecovery) Cancel() {
	cfr.State = RecoveryCancelled
	cfr.CompletedAt = time.Now().Unix()
}

type RecoveryCheckpoint struct {

	ID [32]byte `json:"id"`

	ForkID [32]byte `json:"fork_id"`

	Address common.Address `json:"address"`

	Epoch int64 `json:"epoch"`

	Timestamp int64 `json:"timestamp"`

	StateRoot [32]byte `json:"state_root"`

	StreamHashes map[[32]byte][32]byte `json:"stream_hashes"`

	RightsHash [32]byte `json:"rights_hash"`

	DelegationsHash [32]byte `json:"delegations_hash"`

	Signature []byte `json:"signature"`
}

func (rc *RecoveryCheckpoint) Sign(privateKey *ecdsa.PrivateKey) error {
	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	if addr != rc.Address {
		return fmt.Errorf("signer does not match checkpoint address")
	}

	rc.Signature = nil
	data, err := json.Marshal(rc)
	if err != nil {
		return err
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return err
	}

	rc.Signature = sig
	return nil
}

func (rc *RecoveryCheckpoint) Verify() bool {
	if len(rc.Signature) == 0 {
		return false
	}

	sig := rc.Signature
	rc.Signature = nil
	data, err := json.Marshal(rc)
	rc.Signature = sig
	if err != nil {
		return false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false
	}

	return crypto.PubkeyToAddress(*pubKey) == rc.Address
}

type RecoveryManager struct {
	mu sync.RWMutex

	ForkID [32]byte

	Owner common.Address

	Recoveries map[[32]byte]*CrossForkRecovery

	Checkpoints map[int64]*RecoveryCheckpoint

	Guardians []common.Address

	GuardianQuorum int

	TimeLockDuration int64

	RecoveryPhraseHash []byte
}

func NewRecoveryManager(forkID [32]byte, owner common.Address) *RecoveryManager {
	return &RecoveryManager{
		ForkID:           forkID,
		Owner:            owner,
		Recoveries:       make(map[[32]byte]*CrossForkRecovery),
		Checkpoints:      make(map[int64]*RecoveryCheckpoint),
		Guardians:        make([]common.Address, 0),
		GuardianQuorum:   0,
		TimeLockDuration: 7 * 24 * 60 * 60,
	}
}

func (rm *RecoveryManager) SetupSocialRecovery(guardians []common.Address, quorum int) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if quorum > len(guardians) {
		return fmt.Errorf("quorum cannot exceed number of guardians")
	}
	if quorum < 1 {
		return fmt.Errorf("quorum must be at least 1")
	}

	rm.Guardians = guardians
	rm.GuardianQuorum = quorum
	return nil
}

func (rm *RecoveryManager) SetRecoveryPhrase(phraseHash []byte) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.RecoveryPhraseHash = phraseHash
}

func (rm *RecoveryManager) InitiateTimeLockRecovery(
	targetAddress common.Address,
	targetKey *ecdsa.PrivateKey,
) (*CrossForkRecovery, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now().Unix()

	recovery := &CrossForkRecovery{
		SourceForkID:      rm.ForkID,
		TargetForkID:      rm.ForkID,
		SourceAddress:     rm.Owner,
		TargetAddress:     targetAddress,
		State:             RecoveryPending,
		Method:            RecoveryTimeLock,
		InitiatedAt:       now,
		TimeLockExpiry:    now + rm.TimeLockDuration,
		RequiredApprovals: 0,
		RecoveryData:      &RecoveryData{},
	}

	recovery.GenerateID()

	if err := recovery.SignWithTarget(targetKey); err != nil {
		return nil, err
	}

	rm.Recoveries[recovery.ID] = recovery
	return recovery, nil
}

func (rm *RecoveryManager) InitiateSocialRecovery(
	targetAddress common.Address,
	targetKey *ecdsa.PrivateKey,
) (*CrossForkRecovery, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if len(rm.Guardians) == 0 {
		return nil, fmt.Errorf("social recovery not configured")
	}

	now := time.Now().Unix()

	recovery := &CrossForkRecovery{
		SourceForkID:      rm.ForkID,
		TargetForkID:      rm.ForkID,
		SourceAddress:     rm.Owner,
		TargetAddress:     targetAddress,
		State:             RecoveryPending,
		Method:            RecoverySocial,
		InitiatedAt:       now,
		RequiredApprovals: rm.GuardianQuorum,
		RecoveryData: &RecoveryData{
			Guardians:      rm.Guardians,
			GuardianQuorum: rm.GuardianQuorum,
		},
	}

	recovery.GenerateID()

	if err := recovery.SignWithTarget(targetKey); err != nil {
		return nil, err
	}

	rm.Recoveries[recovery.ID] = recovery
	return recovery, nil
}

func (rm *RecoveryManager) InitiatePhraseRecovery(
	targetAddress common.Address,
	targetKey *ecdsa.PrivateKey,
	phraseHash []byte,
) (*CrossForkRecovery, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.RecoveryPhraseHash == nil {
		return nil, fmt.Errorf("recovery phrase not configured")
	}

	if len(phraseHash) != len(rm.RecoveryPhraseHash) {
		return nil, fmt.Errorf("invalid recovery phrase")
	}
	for i := range phraseHash {
		if phraseHash[i] != rm.RecoveryPhraseHash[i] {
			return nil, fmt.Errorf("invalid recovery phrase")
		}
	}

	now := time.Now().Unix()

	recovery := &CrossForkRecovery{
		SourceForkID:      rm.ForkID,
		TargetForkID:      rm.ForkID,
		SourceAddress:     rm.Owner,
		TargetAddress:     targetAddress,
		State:             RecoveryApproved,
		Method:            RecoveryPhrase,
		InitiatedAt:       now,
		RequiredApprovals: 0,
		RecoveryData: &RecoveryData{
			PhraseHash: phraseHash,
		},
	}

	recovery.GenerateID()

	if err := recovery.SignWithTarget(targetKey); err != nil {
		return nil, err
	}

	rm.Recoveries[recovery.ID] = recovery
	return recovery, nil
}

func (rm *RecoveryManager) AddGuardianApproval(
	recoveryID [32]byte,
	guardianKey *ecdsa.PrivateKey,
	comment string,
) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	recovery, exists := rm.Recoveries[recoveryID]
	if !exists {
		return fmt.Errorf("recovery not found")
	}

	if recovery.Method != RecoverySocial {
		return fmt.Errorf("not a social recovery")
	}

	guardian := crypto.PubkeyToAddress(guardianKey.PublicKey)

	data, _ := json.Marshal(struct {
		RecoveryID [32]byte
		Approver   common.Address
		Approve    bool
	}{recoveryID, guardian, true})

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, guardianKey)
	if err != nil {
		return err
	}

	return recovery.AddApproval(guardian, sig, comment)
}

func (rm *RecoveryManager) CreateCheckpoint(
	privateKey *ecdsa.PrivateKey,
	epoch int64,
	stateRoot [32]byte,
	streamHashes map[[32]byte][32]byte,
	rightsHash [32]byte,
	delegationsHash [32]byte,
) (*RecoveryCheckpoint, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	if addr != rm.Owner {
		return nil, fmt.Errorf("only owner can create checkpoints")
	}

	checkpoint := &RecoveryCheckpoint{
		ForkID:          rm.ForkID,
		Address:         rm.Owner,
		Epoch:           epoch,
		Timestamp:       time.Now().Unix(),
		StateRoot:       stateRoot,
		StreamHashes:    streamHashes,
		RightsHash:      rightsHash,
		DelegationsHash: delegationsHash,
	}

	idData, _ := json.Marshal(struct {
		ForkID  [32]byte
		Address common.Address
		Epoch   int64
	}{checkpoint.ForkID, checkpoint.Address, checkpoint.Epoch})
	checkpoint.ID = crypto.Keccak256Hash(idData)

	if err := checkpoint.Sign(privateKey); err != nil {
		return nil, err
	}

	rm.Checkpoints[epoch] = checkpoint
	return checkpoint, nil
}

func (rm *RecoveryManager) GetCheckpoint(epoch int64) *RecoveryCheckpoint {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.Checkpoints[epoch]
}

func (rm *RecoveryManager) GetLatestCheckpoint() *RecoveryCheckpoint {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var latest *RecoveryCheckpoint
	var latestEpoch int64 = -1

	for epoch, checkpoint := range rm.Checkpoints {
		if epoch > latestEpoch {
			latestEpoch = epoch
			latest = checkpoint
		}
	}

	return latest
}

func (rm *RecoveryManager) ExecuteRecovery(recoveryID [32]byte) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	recovery, exists := rm.Recoveries[recoveryID]
	if !exists {
		return fmt.Errorf("recovery not found")
	}

	if err := recovery.Execute(); err != nil {
		return err
	}

	rm.Owner = recovery.TargetAddress

	return nil
}

func (rm *RecoveryManager) GetRecovery(recoveryID [32]byte) *CrossForkRecovery {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.Recoveries[recoveryID]
}

func (rm *RecoveryManager) GetActiveRecoveries() []*CrossForkRecovery {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var result []*CrossForkRecovery
	for _, r := range rm.Recoveries {
		if r.State != RecoveryExecuted && r.State != RecoveryCancelled && r.State != RecoveryFailed {
			result = append(result, r)
		}
	}
	return result
}

func (rm *RecoveryManager) ExportRecoveryData() ([]byte, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return json.Marshal(struct {
		ForkID           [32]byte                        `json:"fork_id"`
		Owner            common.Address                  `json:"owner"`
		Guardians        []common.Address                `json:"guardians"`
		GuardianQuorum   int                             `json:"guardian_quorum"`
		TimeLockDuration int64                           `json:"time_lock_duration"`
		Checkpoints      map[int64]*RecoveryCheckpoint   `json:"checkpoints"`
		Recoveries       map[[32]byte]*CrossForkRecovery `json:"recoveries"`
	}{
		ForkID:           rm.ForkID,
		Owner:            rm.Owner,
		Guardians:        rm.Guardians,
		GuardianQuorum:   rm.GuardianQuorum,
		TimeLockDuration: rm.TimeLockDuration,
		Checkpoints:      rm.Checkpoints,
		Recoveries:       rm.Recoveries,
	})
}
