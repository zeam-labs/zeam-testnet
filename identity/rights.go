package identity

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type RightType string

const (

	RightAccess RightType = "access"

	RightControl RightType = "control"

	RightPreservation RightType = "preservation"

	RightReturn RightType = "return"
)

type Right struct {

	Type RightType `json:"type"`

	Grantee common.Address `json:"grantee"`

	Expiry int64 `json:"expiry,omitempty"`

	Conditions *RightConditions `json:"conditions,omitempty"`

	Delegatable bool `json:"delegatable"`

	MaxDelegationDepth int `json:"max_delegation_depth,omitempty"`
}

type RightConditions struct {

	RequireConsent bool `json:"require_consent,omitempty"`

	RequireMultiSig bool `json:"require_multi_sig,omitempty"`

	MultiSigThreshold int `json:"multi_sig_threshold,omitempty"`

	MultiSigParties []common.Address `json:"multi_sig_parties,omitempty"`

	TimeWindowStart int `json:"time_window_start,omitempty"`
	TimeWindowEnd   int `json:"time_window_end,omitempty"`

	RateLimitPerEpoch int `json:"rate_limit_per_epoch,omitempty"`
}

type RightsMetadata struct {

	StreamID [32]byte `json:"stream_id"`

	Owner common.Address `json:"owner"`

	Created int64 `json:"created"`

	Rights []Right `json:"rights"`

	PrivacyLevel PrivacyLevel `json:"privacy_level"`

	Signature []byte `json:"signature"`
}

type PrivacyLevel string

const (

	PrivacyPrivate PrivacyLevel = "private"

	PrivacyRestricted PrivacyLevel = "restricted"

	PrivacyPublic PrivacyLevel = "public"
)

func NewRightsMetadata(streamID [32]byte, owner common.Address, privacy PrivacyLevel) *RightsMetadata {
	return &RightsMetadata{
		StreamID:     streamID,
		Owner:        owner,
		Created:      time.Now().Unix(),
		Rights:       make([]Right, 0),
		PrivacyLevel: privacy,
	}
}

func (rm *RightsMetadata) GrantRight(right Right) {
	rm.Rights = append(rm.Rights, right)
}

func (rm *RightsMetadata) RevokeRight(rightType RightType, grantee common.Address) bool {
	for i, r := range rm.Rights {
		if r.Type == rightType && r.Grantee == grantee {
			rm.Rights = append(rm.Rights[:i], rm.Rights[i+1:]...)
			return true
		}
	}
	return false
}

func (rm *RightsMetadata) HasRight(addr common.Address, rightType RightType) bool {

	if addr == rm.Owner {
		return true
	}

	now := time.Now().Unix()
	for _, r := range rm.Rights {
		if r.Type == rightType && r.Grantee == addr {

			if r.Expiry > 0 && r.Expiry < now {
				continue
			}
			return true
		}
	}
	return false
}

func (rm *RightsMetadata) CanDelegate(addr common.Address, rightType RightType) (bool, int) {

	if addr == rm.Owner {
		return true, 255
	}

	for _, r := range rm.Rights {
		if r.Type == rightType && r.Grantee == addr && r.Delegatable {
			return true, r.MaxDelegationDepth
		}
	}
	return false, 0
}

func (rm *RightsMetadata) Sign(privateKey *ecdsa.PrivateKey) error {

	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	if addr != rm.Owner {
		return fmt.Errorf("signer %s is not owner %s", addr.Hex(), rm.Owner.Hex())
	}

	rm.Signature = nil
	data, err := json.Marshal(rm)
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	rm.Signature = sig
	return nil
}

func (rm *RightsMetadata) Verify() bool {
	if len(rm.Signature) == 0 {
		return false
	}

	sig := rm.Signature
	rm.Signature = nil

	data, err := json.Marshal(rm)
	rm.Signature = sig
	if err != nil {
		return false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	return recoveredAddr == rm.Owner
}

func (rm *RightsMetadata) ToJSON() ([]byte, error) {
	return json.Marshal(rm)
}

func RightsMetadataFromJSON(data []byte) (*RightsMetadata, error) {
	var rm RightsMetadata
	if err := json.Unmarshal(data, &rm); err != nil {
		return nil, err
	}
	return &rm, nil
}

type RightsRegistry struct {

	Owner common.Address

	Streams map[[32]byte]*RightsMetadata

	GrantedToMe map[RightType][][32]byte
}

func NewRightsRegistry(owner common.Address) *RightsRegistry {
	return &RightsRegistry{
		Owner:       owner,
		Streams:     make(map[[32]byte]*RightsMetadata),
		GrantedToMe: make(map[RightType][][32]byte),
	}
}

func (rr *RightsRegistry) Register(rm *RightsMetadata) error {
	if !rm.Verify() {
		return fmt.Errorf("invalid signature on rights metadata")
	}
	rr.Streams[rm.StreamID] = rm

	for _, r := range rm.Rights {
		if r.Grantee == rr.Owner {
			rr.GrantedToMe[r.Type] = append(rr.GrantedToMe[r.Type], rm.StreamID)
		}
	}
	return nil
}

func (rr *RightsRegistry) GetRights(streamID [32]byte) *RightsMetadata {
	return rr.Streams[streamID]
}

func (rr *RightsRegistry) ListStreamsWithRight(addr common.Address, rightType RightType) [][32]byte {
	var result [][32]byte
	for streamID, rm := range rr.Streams {
		if rm.HasRight(addr, rightType) {
			result = append(result, streamID)
		}
	}
	return result
}
