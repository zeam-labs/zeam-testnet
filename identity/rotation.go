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

type KeyRotationReason string

const (

	RotationScheduled KeyRotationReason = "scheduled"

	RotationCompromise KeyRotationReason = "compromise"

	RotationUpgrade KeyRotationReason = "upgrade"

	RotationRecovery KeyRotationReason = "recovery"

	RotationMigration KeyRotationReason = "migration"
)

type ContinuityProof struct {

	Version int `json:"version"`

	OldAddress common.Address `json:"old_address"`

	NewAddress common.Address `json:"new_address"`

	ForkID [32]byte `json:"fork_id"`

	RotationNumber int `json:"rotation_number"`

	Reason KeyRotationReason `json:"reason"`

	Timestamp int64 `json:"timestamp"`

	EffectiveAt int64 `json:"effective_at"`

	GracePeriodEnd int64 `json:"grace_period_end"`

	OldKeySignature []byte `json:"old_key_signature"`

	NewKeySignature []byte `json:"new_key_signature"`

	RecoveryProof *RecoveryProof `json:"recovery_proof,omitempty"`
}

type RecoveryProof struct {

	Method RecoveryMethod `json:"method"`

	RecoveryPhraseHash []byte `json:"recovery_phrase_hash,omitempty"`

	SocialRecoverySignatures []SocialRecoverySignature `json:"social_recovery_signatures,omitempty"`

	TimeLockProof *TimeLockProof `json:"time_lock_proof,omitempty"`
}

type RecoveryMethod string

const (

	RecoveryPhrase RecoveryMethod = "phrase"

	RecoverySocial RecoveryMethod = "social"

	RecoveryTimeLock RecoveryMethod = "timelock"
)

type SocialRecoverySignature struct {
	Guardian  common.Address `json:"guardian"`
	Signature []byte         `json:"signature"`
	Timestamp int64          `json:"timestamp"`
}

type TimeLockProof struct {

	InitiatedAt int64 `json:"initiated_at"`

	LockDuration int64 `json:"lock_duration"`

	InitiationTxHash [32]byte `json:"initiation_tx_hash"`
}

func (cp *ContinuityProof) SignWithOldKey(oldKey *ecdsa.PrivateKey) error {
	addr := crypto.PubkeyToAddress(oldKey.PublicKey)
	if addr != cp.OldAddress {
		return fmt.Errorf("key address %s does not match old address %s", addr.Hex(), cp.OldAddress.Hex())
	}

	cp.OldKeySignature = nil
	cp.NewKeySignature = nil
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, oldKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	cp.OldKeySignature = sig
	return nil
}

func (cp *ContinuityProof) SignWithNewKey(newKey *ecdsa.PrivateKey) error {
	addr := crypto.PubkeyToAddress(newKey.PublicKey)
	if addr != cp.NewAddress {
		return fmt.Errorf("key address %s does not match new address %s", addr.Hex(), cp.NewAddress.Hex())
	}

	oldSig := cp.OldKeySignature
	cp.NewKeySignature = nil
	cp.OldKeySignature = nil
	data, err := json.Marshal(cp)
	cp.OldKeySignature = oldSig
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(append(data, oldSig...))
	sig, err := crypto.Sign(hash, newKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	cp.NewKeySignature = sig
	return nil
}

func (cp *ContinuityProof) Verify() error {

	if cp.Reason == RotationRecovery {
		return cp.verifyNewKeyOnly()
	}

	if len(cp.OldKeySignature) == 0 {
		return fmt.Errorf("missing old key signature")
	}
	if len(cp.NewKeySignature) == 0 {
		return fmt.Errorf("missing new key signature")
	}

	oldSig := cp.OldKeySignature
	newSig := cp.NewKeySignature
	cp.OldKeySignature = nil
	cp.NewKeySignature = nil

	data, err := json.Marshal(cp)
	if err != nil {
		cp.OldKeySignature = oldSig
		cp.NewKeySignature = newSig
		return fmt.Errorf("failed to marshal: %w", err)
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, oldSig)
	if err != nil {
		cp.OldKeySignature = oldSig
		cp.NewKeySignature = newSig
		return fmt.Errorf("invalid old key signature: %w", err)
	}

	if crypto.PubkeyToAddress(*pubKey) != cp.OldAddress {
		cp.OldKeySignature = oldSig
		cp.NewKeySignature = newSig
		return fmt.Errorf("old key signature does not match old address")
	}

	hash = crypto.Keccak256(append(data, oldSig...))
	pubKey, err = crypto.SigToPub(hash, newSig)
	if err != nil {
		cp.OldKeySignature = oldSig
		cp.NewKeySignature = newSig
		return fmt.Errorf("invalid new key signature: %w", err)
	}

	if crypto.PubkeyToAddress(*pubKey) != cp.NewAddress {
		cp.OldKeySignature = oldSig
		cp.NewKeySignature = newSig
		return fmt.Errorf("new key signature does not match new address")
	}

	cp.OldKeySignature = oldSig
	cp.NewKeySignature = newSig
	return nil
}

func (cp *ContinuityProof) verifyNewKeyOnly() error {
	if len(cp.NewKeySignature) == 0 {
		return fmt.Errorf("missing new key signature")
	}

	if cp.RecoveryProof == nil {
		return fmt.Errorf("recovery rotation requires recovery proof")
	}

	newSig := cp.NewKeySignature
	cp.NewKeySignature = nil

	data, err := json.Marshal(cp)
	cp.NewKeySignature = newSig
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, newSig)
	if err != nil {
		return fmt.Errorf("invalid new key signature: %w", err)
	}

	if crypto.PubkeyToAddress(*pubKey) != cp.NewAddress {
		return fmt.Errorf("new key signature does not match new address")
	}

	return nil
}

func (cp *ContinuityProof) IsEffective() bool {
	return time.Now().Unix() >= cp.EffectiveAt
}

func (cp *ContinuityProof) IsInGracePeriod() bool {
	now := time.Now().Unix()
	return now < cp.GracePeriodEnd
}

type KeyRotationManager struct {
	mu sync.RWMutex

	ForkID [32]byte

	CurrentAddress common.Address

	RotationHistory []*ContinuityProof

	SocialRecoveryGuardians []common.Address

	SocialRecoveryThreshold int

	TimeLockDuration int64

	PendingRecovery *PendingRecovery
}

type PendingRecovery struct {
	NewAddress  common.Address `json:"new_address"`
	InitiatedAt int64          `json:"initiated_at"`
	CanComplete int64          `json:"can_complete"`
}

func NewKeyRotationManager(identity *Identity) *KeyRotationManager {
	return &KeyRotationManager{
		ForkID:                  identity.ForkID,
		CurrentAddress:          identity.Address,
		RotationHistory:         make([]*ContinuityProof, 0),
		SocialRecoveryGuardians: make([]common.Address, 0),
		SocialRecoveryThreshold: 0,
		TimeLockDuration:        7 * 24 * 60 * 60,
	}
}

func (krm *KeyRotationManager) Rotate(oldKey, newKey *ecdsa.PrivateKey, reason KeyRotationReason, gracePeriodDays int) (*ContinuityProof, error) {
	krm.mu.Lock()
	defer krm.mu.Unlock()

	oldAddr := crypto.PubkeyToAddress(oldKey.PublicKey)
	newAddr := crypto.PubkeyToAddress(newKey.PublicKey)

	if oldAddr != krm.CurrentAddress {
		return nil, fmt.Errorf("old key does not match current address")
	}

	now := time.Now().Unix()
	gracePeriod := int64(gracePeriodDays * 24 * 60 * 60)

	proof := &ContinuityProof{
		Version:        1,
		OldAddress:     oldAddr,
		NewAddress:     newAddr,
		ForkID:         krm.ForkID,
		RotationNumber: len(krm.RotationHistory) + 1,
		Reason:         reason,
		Timestamp:      now,
		EffectiveAt:    now,
		GracePeriodEnd: now + gracePeriod,
	}

	if err := proof.SignWithOldKey(oldKey); err != nil {
		return nil, fmt.Errorf("failed to sign with old key: %w", err)
	}
	if err := proof.SignWithNewKey(newKey); err != nil {
		return nil, fmt.Errorf("failed to sign with new key: %w", err)
	}

	if err := proof.Verify(); err != nil {
		return nil, fmt.Errorf("proof verification failed: %w", err)
	}

	krm.CurrentAddress = newAddr
	krm.RotationHistory = append(krm.RotationHistory, proof)

	return proof, nil
}

func (krm *KeyRotationManager) SetupSocialRecovery(guardians []common.Address, threshold int) error {
	krm.mu.Lock()
	defer krm.mu.Unlock()

	if threshold > len(guardians) {
		return fmt.Errorf("threshold cannot exceed number of guardians")
	}
	if threshold < 1 {
		return fmt.Errorf("threshold must be at least 1")
	}

	krm.SocialRecoveryGuardians = guardians
	krm.SocialRecoveryThreshold = threshold
	return nil
}

func (krm *KeyRotationManager) InitiateTimeLockRecovery(newAddr common.Address) error {
	krm.mu.Lock()
	defer krm.mu.Unlock()

	if krm.PendingRecovery != nil {
		return fmt.Errorf("recovery already in progress")
	}

	now := time.Now().Unix()
	krm.PendingRecovery = &PendingRecovery{
		NewAddress:  newAddr,
		InitiatedAt: now,
		CanComplete: now + krm.TimeLockDuration,
	}

	return nil
}

func (krm *KeyRotationManager) CompleteTimeLockRecovery(newKey *ecdsa.PrivateKey) (*ContinuityProof, error) {
	krm.mu.Lock()
	defer krm.mu.Unlock()

	if krm.PendingRecovery == nil {
		return nil, fmt.Errorf("no pending recovery")
	}

	newAddr := crypto.PubkeyToAddress(newKey.PublicKey)
	if newAddr != krm.PendingRecovery.NewAddress {
		return nil, fmt.Errorf("new key does not match pending recovery address")
	}

	now := time.Now().Unix()
	if now < krm.PendingRecovery.CanComplete {
		return nil, fmt.Errorf("time lock has not expired, %d seconds remaining",
			krm.PendingRecovery.CanComplete-now)
	}

	proof := &ContinuityProof{
		Version:        1,
		OldAddress:     krm.CurrentAddress,
		NewAddress:     newAddr,
		ForkID:         krm.ForkID,
		RotationNumber: len(krm.RotationHistory) + 1,
		Reason:         RotationRecovery,
		Timestamp:      now,
		EffectiveAt:    now,
		GracePeriodEnd: now,
		RecoveryProof: &RecoveryProof{
			Method: RecoveryTimeLock,
			TimeLockProof: &TimeLockProof{
				InitiatedAt:  krm.PendingRecovery.InitiatedAt,
				LockDuration: krm.TimeLockDuration,
			},
		},
	}

	if err := proof.SignWithNewKey(newKey); err != nil {
		return nil, fmt.Errorf("failed to sign with new key: %w", err)
	}

	krm.CurrentAddress = newAddr
	krm.RotationHistory = append(krm.RotationHistory, proof)
	krm.PendingRecovery = nil

	return proof, nil
}

func (krm *KeyRotationManager) VerifyChain(genesisAddress common.Address) error {
	krm.mu.RLock()
	defer krm.mu.RUnlock()

	currentAddr := genesisAddress

	for i, proof := range krm.RotationHistory {

		if proof.RotationNumber != i+1 {
			return fmt.Errorf("rotation %d has incorrect number %d", i+1, proof.RotationNumber)
		}

		if proof.OldAddress != currentAddr {
			return fmt.Errorf("rotation %d: old address mismatch", i+1)
		}

		if proof.ForkID != krm.ForkID {
			return fmt.Errorf("rotation %d: fork ID mismatch", i+1)
		}

		if err := proof.Verify(); err != nil {
			return fmt.Errorf("rotation %d: %w", i+1, err)
		}

		currentAddr = proof.NewAddress
	}

	if currentAddr != krm.CurrentAddress {
		return fmt.Errorf("chain does not end at current address")
	}

	return nil
}

func (krm *KeyRotationManager) GetRotationHistory() []*ContinuityProof {
	krm.mu.RLock()
	defer krm.mu.RUnlock()

	result := make([]*ContinuityProof, len(krm.RotationHistory))
	copy(result, krm.RotationHistory)
	return result
}

func (krm *KeyRotationManager) ExportChain() ([]byte, error) {
	krm.mu.RLock()
	defer krm.mu.RUnlock()

	return json.Marshal(struct {
		ForkID          [32]byte           `json:"fork_id"`
		CurrentAddress  common.Address     `json:"current_address"`
		RotationHistory []*ContinuityProof `json:"rotation_history"`
	}{
		ForkID:          krm.ForkID,
		CurrentAddress:  krm.CurrentAddress,
		RotationHistory: krm.RotationHistory,
	})
}
