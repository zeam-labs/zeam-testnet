

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


type Delegation struct {
	
	ID [32]byte `json:"id"`

	
	Delegator common.Address `json:"delegator"`

	
	Delegate common.Address `json:"delegate"`

	
	RightType RightType `json:"right_type"`

	
	StreamID [32]byte `json:"stream_id,omitempty"`

	
	Scope DelegationScope `json:"scope"`

	
	Depth int `json:"depth"`

	
	CurrentDepth int `json:"current_depth"`

	
	ParentDelegation [32]byte `json:"parent_delegation,omitempty"`

	
	Created int64 `json:"created"`

	
	ValidFrom int64 `json:"valid_from"`

	
	ValidUntil int64 `json:"valid_until,omitempty"`

	
	Policy DelegationPolicy `json:"policy"`

	
	Revoked bool `json:"revoked"`

	
	RevokedAt int64 `json:"revoked_at,omitempty"`

	
	RevokedBy common.Address `json:"revoked_by,omitempty"`

	
	Signature []byte `json:"signature"`
}


type DelegationScope struct {
	
	AllowedOperations []ConsentType `json:"allowed_operations"`

	
	ReadOnly bool `json:"read_only,omitempty"`

	
	LayerRestriction []LayerID `json:"layer_restriction,omitempty"`

	
	MaxDataSize int64 `json:"max_data_size,omitempty"`
}


type DelegationPolicy struct {
	
	RequireReason bool `json:"require_reason,omitempty"`

	
	NotifyDelegator bool `json:"notify_delegator,omitempty"`

	
	RateLimitPerHour int `json:"rate_limit_per_hour,omitempty"`

	
	RateLimitPerDay int `json:"rate_limit_per_day,omitempty"`

	
	AllowSubDelegation bool `json:"allow_sub_delegation,omitempty"`

	
	SubDelegationRequiresApproval bool `json:"sub_delegation_requires_approval,omitempty"`

	
	InheritPolicyToSubDelegates bool `json:"inherit_policy_to_sub_delegates,omitempty"`

	
	RevokeOnCompromise bool `json:"revoke_on_compromise,omitempty"`
}


func (d *Delegation) GenerateID() {
	data, _ := json.Marshal(struct {
		Delegator common.Address
		Delegate  common.Address
		RightType RightType
		StreamID  [32]byte
		Created   int64
	}{d.Delegator, d.Delegate, d.RightType, d.StreamID, d.Created})

	d.ID = crypto.Keccak256Hash(data)
}


func (d *Delegation) Sign(privateKey *ecdsa.PrivateKey) error {
	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	if addr != d.Delegator {
		return fmt.Errorf("signer %s is not delegator %s", addr.Hex(), d.Delegator.Hex())
	}

	d.Signature = nil
	data, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	d.Signature = sig
	return nil
}


func (d *Delegation) Verify() bool {
	if len(d.Signature) == 0 {
		return false
	}

	sig := d.Signature
	d.Signature = nil
	data, err := json.Marshal(d)
	d.Signature = sig
	if err != nil {
		return false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false
	}

	return crypto.PubkeyToAddress(*pubKey) == d.Delegator
}


func (d *Delegation) IsValid() bool {
	if d.Revoked {
		return false
	}

	now := time.Now().Unix()

	if now < d.ValidFrom {
		return false
	}

	if d.ValidUntil > 0 && now > d.ValidUntil {
		return false
	}

	return d.Verify()
}


func (d *Delegation) CanSubDelegate() bool {
	if !d.IsValid() {
		return false
	}

	if !d.Policy.AllowSubDelegation {
		return false
	}

	if d.Depth <= 0 {
		return false
	}

	return true
}


func (d *Delegation) CreateSubDelegation(delegate common.Address, validUntil int64) (*Delegation, error) {
	if !d.CanSubDelegate() {
		return nil, fmt.Errorf("delegation does not allow sub-delegation")
	}

	
	if d.ValidUntil > 0 && (validUntil == 0 || validUntil > d.ValidUntil) {
		validUntil = d.ValidUntil
	}

	now := time.Now().Unix()
	subDel := &Delegation{
		Delegator:        d.Delegate, 
		Delegate:         delegate,
		RightType:        d.RightType,
		StreamID:         d.StreamID,
		Scope:            d.Scope, 
		Depth:            d.Depth - 1,
		CurrentDepth:     d.CurrentDepth + 1,
		ParentDelegation: d.ID,
		Created:          now,
		ValidFrom:        now,
		ValidUntil:       validUntil,
	}

	
	if d.Policy.InheritPolicyToSubDelegates {
		subDel.Policy = d.Policy
	} else {
		subDel.Policy = DelegationPolicy{
			AllowSubDelegation: d.Depth > 1, 
		}
	}

	subDel.GenerateID()
	return subDel, nil
}


func (d *Delegation) Revoke(revoker common.Address) {
	d.Revoked = true
	d.RevokedAt = time.Now().Unix()
	d.RevokedBy = revoker
}


type DelegationManager struct {
	mu sync.RWMutex

	
	Owner common.Address

	
	Delegations map[[32]byte]*Delegation

	
	DelegationsGiven map[common.Address][][32]byte

	
	DelegationsReceived map[common.Address][][32]byte

	
	DelegationChains map[[32]byte][][32]byte

	
	UsageTracking map[[32]byte]*UsageTracker
}


type UsageTracker struct {
	HourlyUses  int
	DailyUses   int
	HourlyReset int64
	DailyReset  int64
}


func NewDelegationManager(owner common.Address) *DelegationManager {
	return &DelegationManager{
		Owner:               owner,
		Delegations:         make(map[[32]byte]*Delegation),
		DelegationsGiven:    make(map[common.Address][][32]byte),
		DelegationsReceived: make(map[common.Address][][32]byte),
		DelegationChains:    make(map[[32]byte][][32]byte),
		UsageTracking:       make(map[[32]byte]*UsageTracker),
	}
}


func (dm *DelegationManager) CreateDelegation(
	privateKey *ecdsa.PrivateKey,
	delegate common.Address,
	rightType RightType,
	streamID [32]byte,
	scope DelegationScope,
	depth int,
	validUntil int64,
	policy DelegationPolicy,
) (*Delegation, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	delegator := crypto.PubkeyToAddress(privateKey.PublicKey)
	now := time.Now().Unix()

	d := &Delegation{
		Delegator:    delegator,
		Delegate:     delegate,
		RightType:    rightType,
		StreamID:     streamID,
		Scope:        scope,
		Depth:        depth,
		CurrentDepth: 0,
		Created:      now,
		ValidFrom:    now,
		ValidUntil:   validUntil,
		Policy:       policy,
	}

	d.GenerateID()

	if err := d.Sign(privateKey); err != nil {
		return nil, err
	}

	
	dm.Delegations[d.ID] = d
	dm.DelegationsGiven[delegator] = append(dm.DelegationsGiven[delegator], d.ID)
	dm.DelegationsReceived[delegate] = append(dm.DelegationsReceived[delegate], d.ID)
	dm.UsageTracking[d.ID] = &UsageTracker{
		HourlyReset: now + 3600,
		DailyReset:  now + 86400,
	}

	return d, nil
}


func (dm *DelegationManager) CreateSubDelegation(
	privateKey *ecdsa.PrivateKey,
	parentID [32]byte,
	newDelegate common.Address,
	validUntil int64,
) (*Delegation, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	parent, exists := dm.Delegations[parentID]
	if !exists {
		return nil, fmt.Errorf("parent delegation not found")
	}

	
	caller := crypto.PubkeyToAddress(privateKey.PublicKey)
	if caller != parent.Delegate {
		return nil, fmt.Errorf("only the delegate can create sub-delegations")
	}

	
	subDel, err := parent.CreateSubDelegation(newDelegate, validUntil)
	if err != nil {
		return nil, err
	}

	
	if err := subDel.Sign(privateKey); err != nil {
		return nil, err
	}

	
	dm.Delegations[subDel.ID] = subDel
	dm.DelegationsGiven[caller] = append(dm.DelegationsGiven[caller], subDel.ID)
	dm.DelegationsReceived[newDelegate] = append(dm.DelegationsReceived[newDelegate], subDel.ID)
	dm.DelegationChains[parentID] = append(dm.DelegationChains[parentID], subDel.ID)
	dm.UsageTracking[subDel.ID] = &UsageTracker{
		HourlyReset: time.Now().Unix() + 3600,
		DailyReset:  time.Now().Unix() + 86400,
	}

	return subDel, nil
}


func (dm *DelegationManager) RevokeDelegation(delegationID [32]byte, revoker common.Address) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	d, exists := dm.Delegations[delegationID]
	if !exists {
		return fmt.Errorf("delegation not found")
	}

	
	if !dm.canRevoke(d, revoker) {
		return fmt.Errorf("not authorized to revoke this delegation")
	}

	
	d.Revoke(revoker)

	
	dm.revokeSubDelegations(delegationID, revoker)

	return nil
}

func (dm *DelegationManager) canRevoke(d *Delegation, revoker common.Address) bool {
	
	if d.Delegator == revoker {
		return true
	}

	
	if d.ParentDelegation != [32]byte{} {
		parent, exists := dm.Delegations[d.ParentDelegation]
		if exists {
			return dm.canRevoke(parent, revoker)
		}
	}

	return false
}

func (dm *DelegationManager) revokeSubDelegations(parentID [32]byte, revoker common.Address) {
	children := dm.DelegationChains[parentID]
	for _, childID := range children {
		if child, exists := dm.Delegations[childID]; exists {
			child.Revoke(revoker)
			dm.revokeSubDelegations(childID, revoker)
		}
	}
}


func (dm *DelegationManager) CheckAndUse(delegationID [32]byte, operation ConsentType) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	d, exists := dm.Delegations[delegationID]
	if !exists {
		return fmt.Errorf("delegation not found")
	}

	if !d.IsValid() {
		return fmt.Errorf("delegation is not valid")
	}

	
	allowed := false
	for _, op := range d.Scope.AllowedOperations {
		if op == operation {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("operation %s not allowed by delegation scope", operation)
	}

	
	tracker := dm.UsageTracking[delegationID]
	now := time.Now().Unix()

	
	if now >= tracker.HourlyReset {
		tracker.HourlyUses = 0
		tracker.HourlyReset = now + 3600
	}
	if now >= tracker.DailyReset {
		tracker.DailyUses = 0
		tracker.DailyReset = now + 86400
	}

	
	if d.Policy.RateLimitPerHour > 0 && tracker.HourlyUses >= d.Policy.RateLimitPerHour {
		return fmt.Errorf("hourly rate limit exceeded")
	}
	if d.Policy.RateLimitPerDay > 0 && tracker.DailyUses >= d.Policy.RateLimitPerDay {
		return fmt.Errorf("daily rate limit exceeded")
	}

	
	tracker.HourlyUses++
	tracker.DailyUses++

	return nil
}


func (dm *DelegationManager) GetDelegationsFor(delegate common.Address) []*Delegation {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var result []*Delegation
	for _, id := range dm.DelegationsReceived[delegate] {
		if d := dm.Delegations[id]; d != nil && d.IsValid() {
			result = append(result, d)
		}
	}
	return result
}


func (dm *DelegationManager) GetDelegationChain(delegationID [32]byte) []*Delegation {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var chain []*Delegation
	currentID := delegationID

	for currentID != [32]byte{} {
		d, exists := dm.Delegations[currentID]
		if !exists {
			break
		}
		chain = append([]*Delegation{d}, chain...) 
		currentID = d.ParentDelegation
	}

	return chain
}


func (dm *DelegationManager) VerifyDelegationChain(delegationID [32]byte) error {
	chain := dm.GetDelegationChain(delegationID)
	if len(chain) == 0 {
		return fmt.Errorf("delegation not found")
	}

	for i, d := range chain {
		if !d.IsValid() {
			return fmt.Errorf("delegation at depth %d is invalid", i)
		}

		
		if i > 0 {
			parent := chain[i-1]
			if d.ParentDelegation != parent.ID {
				return fmt.Errorf("chain break at depth %d", i)
			}
			if d.Delegator != parent.Delegate {
				return fmt.Errorf("delegator mismatch at depth %d", i)
			}
		}
	}

	return nil
}


func (dm *DelegationManager) ExportDelegations() ([]byte, error) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	return json.Marshal(dm.Delegations)
}
