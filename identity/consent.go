package identity

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type ConsentType string

const (

	ConsentRead ConsentType = "read"

	ConsentWrite ConsentType = "write"

	ConsentExport ConsentType = "export"

	ConsentDelegate ConsentType = "delegate"

	ConsentDelete ConsentType = "delete"
)

type Consent struct {

	ID [32]byte `json:"id"`

	Type ConsentType `json:"type"`

	Grantor common.Address `json:"grantor"`

	Grantee common.Address `json:"grantee"`

	StreamID [32]byte `json:"stream_id,omitempty"`

	SourceLayer LayerID `json:"source_layer,omitempty"`

	TargetLayer LayerID `json:"target_layer,omitempty"`

	Created int64 `json:"created"`

	Expiry int64 `json:"expiry,omitempty"`

	MaxUses int `json:"max_uses,omitempty"`

	UsesRemaining int `json:"uses_remaining,omitempty"`

	Conditions *ConsentConditions `json:"conditions,omitempty"`

	Revoked bool `json:"revoked"`

	RevokedAt int64 `json:"revoked_at,omitempty"`

	Signature []byte `json:"signature"`
}

type ConsentConditions struct {

	RequireReason bool `json:"require_reason,omitempty"`

	NotifyOnUse bool `json:"notify_on_use,omitempty"`

	ApprovalRequired bool `json:"approval_required,omitempty"`

	AllowedPurposes []string `json:"allowed_purposes,omitempty"`

	GeoRestriction []string `json:"geo_restriction,omitempty"`
}

func (c *Consent) GenerateID() {
	data, _ := json.Marshal(struct {
		Type     ConsentType
		Grantor  common.Address
		Grantee  common.Address
		StreamID [32]byte
		Created  int64
	}{c.Type, c.Grantor, c.Grantee, c.StreamID, c.Created})

	c.ID = crypto.Keccak256Hash(data)
}

func (c *Consent) Sign(privateKey *ecdsa.PrivateKey) error {
	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	if addr != c.Grantor {
		return fmt.Errorf("signer %s is not grantor %s", addr.Hex(), c.Grantor.Hex())
	}

	c.Signature = nil
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	c.Signature = sig
	return nil
}

func (c *Consent) Verify() bool {
	if len(c.Signature) == 0 {
		return false
	}

	sig := c.Signature
	c.Signature = nil
	data, err := json.Marshal(c)
	c.Signature = sig
	if err != nil {
		return false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false
	}

	return crypto.PubkeyToAddress(*pubKey) == c.Grantor
}

func (c *Consent) IsValid() bool {
	if c.Revoked {
		return false
	}

	now := time.Now().Unix()
	if c.Expiry > 0 && c.Expiry < now {
		return false
	}

	if c.MaxUses > 0 && c.UsesRemaining <= 0 {
		return false
	}

	return c.Verify()
}

func (c *Consent) Use() error {
	if !c.IsValid() {
		return fmt.Errorf("consent is not valid")
	}

	if c.MaxUses > 0 {
		c.UsesRemaining--
	}

	return nil
}

func (c *Consent) Revoke() {
	c.Revoked = true
	c.RevokedAt = time.Now().Unix()
}

type ConsentProof struct {

	ConsentID [32]byte `json:"consent_id"`

	Operation ConsentType `json:"operation"`

	Target [32]byte `json:"target"`

	Timestamp int64 `json:"timestamp"`

	Reason string `json:"reason,omitempty"`

	Signature []byte `json:"signature"`
}

func (cp *ConsentProof) Sign(privateKey *ecdsa.PrivateKey) error {
	cp.Signature = nil
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	cp.Signature = sig
	return nil
}

func (cp *ConsentProof) Verify() (common.Address, bool) {
	if len(cp.Signature) == 0 {
		return common.Address{}, false
	}

	sig := cp.Signature
	cp.Signature = nil
	data, err := json.Marshal(cp)
	cp.Signature = sig
	if err != nil {
		return common.Address{}, false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return common.Address{}, false
	}

	return crypto.PubkeyToAddress(*pubKey), true
}

type ConsentManager struct {
	mu sync.RWMutex

	Owner common.Address

	Consents map[[32]byte]*Consent

	ByGrantee map[common.Address][][32]byte

	UsageLog []ConsentUsageEntry
}

type ConsentUsageEntry struct {
	ConsentID [32]byte       `json:"consent_id"`
	Grantee   common.Address `json:"grantee"`
	Operation ConsentType    `json:"operation"`
	Target    [32]byte       `json:"target"`
	Timestamp int64          `json:"timestamp"`
	Reason    string         `json:"reason,omitempty"`
}

func NewConsentManager(owner common.Address) *ConsentManager {
	return &ConsentManager{
		Owner:     owner,
		Consents:  make(map[[32]byte]*Consent),
		ByGrantee: make(map[common.Address][][32]byte),
		UsageLog:  make([]ConsentUsageEntry, 0),
	}
}

func (cm *ConsentManager) Grant(consent *Consent, privateKey *ecdsa.PrivateKey) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if consent.Grantor != cm.Owner {
		return fmt.Errorf("grantor must be consent manager owner")
	}

	if consent.ID == [32]byte{} {
		consent.GenerateID()
	}

	if consent.MaxUses > 0 {
		consent.UsesRemaining = consent.MaxUses
	}

	if err := consent.Sign(privateKey); err != nil {
		return err
	}

	cm.Consents[consent.ID] = consent
	cm.ByGrantee[consent.Grantee] = append(cm.ByGrantee[consent.Grantee], consent.ID)

	return nil
}

func (cm *ConsentManager) Revoke(consentID [32]byte) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	consent, exists := cm.Consents[consentID]
	if !exists {
		return fmt.Errorf("consent not found")
	}

	consent.Revoke()
	return nil
}

func (cm *ConsentManager) ValidateAndUse(proof *ConsentProof) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	consent, exists := cm.Consents[proof.ConsentID]
	if !exists {
		return fmt.Errorf("consent not found")
	}

	grantee, valid := proof.Verify()
	if !valid {
		return fmt.Errorf("invalid proof signature")
	}

	if grantee != consent.Grantee {
		return fmt.Errorf("proof signer is not the grantee")
	}

	if !consent.IsValid() {
		return fmt.Errorf("consent is not valid")
	}

	if proof.Operation != consent.Type {
		return fmt.Errorf("operation type mismatch")
	}

	if consent.Conditions != nil {
		if consent.Conditions.RequireReason && proof.Reason == "" {
			return fmt.Errorf("reason required but not provided")
		}
	}

	if err := consent.Use(); err != nil {
		return err
	}

	cm.UsageLog = append(cm.UsageLog, ConsentUsageEntry{
		ConsentID: proof.ConsentID,
		Grantee:   grantee,
		Operation: proof.Operation,
		Target:    proof.Target,
		Timestamp: proof.Timestamp,
		Reason:    proof.Reason,
	})

	return nil
}

func (cm *ConsentManager) GetConsentsFor(grantee common.Address) []*Consent {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var result []*Consent
	for _, id := range cm.ByGrantee[grantee] {
		if consent := cm.Consents[id]; consent != nil && consent.IsValid() {
			result = append(result, consent)
		}
	}
	return result
}

func (cm *ConsentManager) HasConsent(grantee common.Address, operation ConsentType, streamID [32]byte) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for _, id := range cm.ByGrantee[grantee] {
		consent := cm.Consents[id]
		if consent == nil || !consent.IsValid() {
			continue
		}

		if consent.Type != operation {
			continue
		}

		if consent.StreamID != [32]byte{} && consent.StreamID != streamID {
			continue
		}

		return true
	}
	return false
}

func (cm *ConsentManager) ExportConsents() ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return json.Marshal(cm.Consents)
}

func (cm *ConsentManager) GetUsageLog() []ConsentUsageEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]ConsentUsageEntry, len(cm.UsageLog))
	copy(result, cm.UsageLog)
	return result
}

func ConsentIDToHex(id [32]byte) string {
	return hex.EncodeToString(id[:])
}

func HexToConsentID(s string) ([32]byte, error) {
	var id [32]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, err
	}
	if len(b) != 32 {
		return id, fmt.Errorf("invalid consent ID length")
	}
	copy(id[:], b)
	return id, nil
}
