

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


type LayerID string


const (
	LayerPublic     LayerID = "public"     
	LayerRestricted LayerID = "restricted" 
	LayerPrivate    LayerID = "private"    
	LayerLocal      LayerID = "local"      
	LayerArchive    LayerID = "archive"    
)


type LayerConfig struct {
	
	ID LayerID `json:"id"`

	
	Parent LayerID `json:"parent,omitempty"`

	
	Rules PrivacyRules `json:"rules"`

	
	TrustedLayers []LayerID `json:"trusted_layers,omitempty"`

	
	Created int64 `json:"created"`

	
	Owner common.Address `json:"owner"`

	
	Signature []byte `json:"signature,omitempty"`
}


type PrivacyRules struct {
	
	ReadAccess AccessPolicy `json:"read_access"`

	
	WriteAccess AccessPolicy `json:"write_access"`

	
	CrossLayerExport bool `json:"cross_layer_export"`

	
	ExportRequiresConsent bool `json:"export_requires_consent"`

	
	RetentionDays int `json:"retention_days,omitempty"` 

	
	EncryptionRequired bool `json:"encryption_required"`

	
	AuditLogging bool `json:"audit_logging"`

	
	InheritFromParent bool `json:"inherit_from_parent"`
}


type AccessPolicy string

const (
	
	AccessOwnerOnly AccessPolicy = "owner_only"
	
	AccessExplicitGrant AccessPolicy = "explicit_grant"
	
	AccessWithProof AccessPolicy = "with_proof"
	
	AccessPublic AccessPolicy = "public"
)


func (lc *LayerConfig) Sign(privateKey *ecdsa.PrivateKey) error {
	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	if addr != lc.Owner {
		return fmt.Errorf("signer %s is not owner %s", addr.Hex(), lc.Owner.Hex())
	}

	lc.Signature = nil
	data, err := json.Marshal(lc)
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	lc.Signature = sig
	return nil
}


func (lc *LayerConfig) Verify() bool {
	if len(lc.Signature) == 0 {
		return false
	}

	sig := lc.Signature
	lc.Signature = nil
	data, err := json.Marshal(lc)
	lc.Signature = sig
	if err != nil {
		return false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false
	}

	return crypto.PubkeyToAddress(*pubKey) == lc.Owner
}


type MemoryLayerManager struct {
	mu sync.RWMutex

	
	Owner common.Address

	
	Layers map[LayerID]*LayerConfig

	
	DataPlacement map[[32]byte]LayerID
}


func NewMemoryLayerManager(owner common.Address) *MemoryLayerManager {
	mlm := &MemoryLayerManager{
		Owner:         owner,
		Layers:        make(map[LayerID]*LayerConfig),
		DataPlacement: make(map[[32]byte]LayerID),
	}

	
	mlm.Layers[LayerPublic] = &LayerConfig{
		ID:      LayerPublic,
		Created: time.Now().Unix(),
		Owner:   owner,
		Rules: PrivacyRules{
			ReadAccess:         AccessPublic,
			WriteAccess:        AccessOwnerOnly,
			CrossLayerExport:   true,
			EncryptionRequired: false,
			AuditLogging:       true,
		},
	}

	mlm.Layers[LayerRestricted] = &LayerConfig{
		ID:      LayerRestricted,
		Parent:  LayerPublic,
		Created: time.Now().Unix(),
		Owner:   owner,
		Rules: PrivacyRules{
			ReadAccess:            AccessWithProof,
			WriteAccess:           AccessExplicitGrant,
			CrossLayerExport:      true,
			ExportRequiresConsent: true,
			EncryptionRequired:    true,
			AuditLogging:          true,
			InheritFromParent:     true,
		},
	}

	mlm.Layers[LayerPrivate] = &LayerConfig{
		ID:      LayerPrivate,
		Parent:  LayerRestricted,
		Created: time.Now().Unix(),
		Owner:   owner,
		Rules: PrivacyRules{
			ReadAccess:            AccessExplicitGrant,
			WriteAccess:           AccessOwnerOnly,
			CrossLayerExport:      true,
			ExportRequiresConsent: true,
			EncryptionRequired:    true,
			AuditLogging:          true,
			InheritFromParent:     true,
		},
	}

	mlm.Layers[LayerLocal] = &LayerConfig{
		ID:      LayerLocal,
		Parent:  LayerPrivate,
		Created: time.Now().Unix(),
		Owner:   owner,
		Rules: PrivacyRules{
			ReadAccess:         AccessOwnerOnly,
			WriteAccess:        AccessOwnerOnly,
			CrossLayerExport:   false, 
			EncryptionRequired: true,
			AuditLogging:       false, 
		},
	}

	mlm.Layers[LayerArchive] = &LayerConfig{
		ID:      LayerArchive,
		Parent:  LayerPrivate,
		Created: time.Now().Unix(),
		Owner:   owner,
		Rules: PrivacyRules{
			ReadAccess:            AccessExplicitGrant,
			WriteAccess:           AccessOwnerOnly,
			CrossLayerExport:      true,
			ExportRequiresConsent: true,
			RetentionDays:         0, 
			EncryptionRequired:    true,
			AuditLogging:          true,
		},
	}

	return mlm
}


func (mlm *MemoryLayerManager) AddLayer(config *LayerConfig) error {
	mlm.mu.Lock()
	defer mlm.mu.Unlock()

	if !config.Verify() {
		return fmt.Errorf("invalid signature on layer config")
	}

	if config.Parent != "" {
		if _, exists := mlm.Layers[config.Parent]; !exists {
			return fmt.Errorf("parent layer %s does not exist", config.Parent)
		}
	}

	mlm.Layers[config.ID] = config
	return nil
}


func (mlm *MemoryLayerManager) GetEffectiveRules(layerID LayerID) (*PrivacyRules, error) {
	mlm.mu.RLock()
	defer mlm.mu.RUnlock()

	layer, exists := mlm.Layers[layerID]
	if !exists {
		return nil, fmt.Errorf("layer %s does not exist", layerID)
	}

	
	if !layer.Rules.InheritFromParent || layer.Parent == "" {
		rules := layer.Rules 
		return &rules, nil
	}

	
	parentRules, err := mlm.GetEffectiveRules(layer.Parent)
	if err != nil {
		return nil, err
	}

	
	merged := *parentRules
	childRules := layer.Rules

	
	if childRules.ReadAccess != "" {
		merged.ReadAccess = childRules.ReadAccess
	}
	if childRules.WriteAccess != "" {
		merged.WriteAccess = childRules.WriteAccess
	}
	
	merged.CrossLayerExport = childRules.CrossLayerExport
	merged.ExportRequiresConsent = childRules.ExportRequiresConsent
	merged.EncryptionRequired = childRules.EncryptionRequired || parentRules.EncryptionRequired
	merged.AuditLogging = childRules.AuditLogging || parentRules.AuditLogging

	if childRules.RetentionDays > 0 {
		merged.RetentionDays = childRules.RetentionDays
	}

	return &merged, nil
}


func (mlm *MemoryLayerManager) PlaceData(streamID [32]byte, layerID LayerID) error {
	mlm.mu.Lock()
	defer mlm.mu.Unlock()

	if _, exists := mlm.Layers[layerID]; !exists {
		return fmt.Errorf("layer %s does not exist", layerID)
	}

	mlm.DataPlacement[streamID] = layerID
	return nil
}


func (mlm *MemoryLayerManager) GetDataLayer(streamID [32]byte) (LayerID, bool) {
	mlm.mu.RLock()
	defer mlm.mu.RUnlock()
	layer, exists := mlm.DataPlacement[streamID]
	return layer, exists
}


func (mlm *MemoryLayerManager) CanAccess(addr common.Address, layerID LayerID, isWrite bool) (bool, error) {
	rules, err := mlm.GetEffectiveRules(layerID)
	if err != nil {
		return false, err
	}

	var policy AccessPolicy
	if isWrite {
		policy = rules.WriteAccess
	} else {
		policy = rules.ReadAccess
	}

	switch policy {
	case AccessPublic:
		return true, nil
	case AccessOwnerOnly:
		return addr == mlm.Owner, nil
	case AccessExplicitGrant, AccessWithProof:
		
		
		return addr == mlm.Owner, nil
	default:
		return false, nil
	}
}


func (mlm *MemoryLayerManager) IsTrustedLayer(from, to LayerID) bool {
	mlm.mu.RLock()
	defer mlm.mu.RUnlock()

	layer, exists := mlm.Layers[from]
	if !exists {
		return false
	}

	for _, trusted := range layer.TrustedLayers {
		if trusted == to {
			return true
		}
	}
	return false
}


func (mlm *MemoryLayerManager) ListLayers() []LayerID {
	mlm.mu.RLock()
	defer mlm.mu.RUnlock()

	layers := make([]LayerID, 0, len(mlm.Layers))
	for id := range mlm.Layers {
		layers = append(layers, id)
	}
	return layers
}
