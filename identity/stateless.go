

package identity

import (
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/argon2"
)


type DerivationConfig struct {
	
	Time    uint32 
	Memory  uint32 
	Threads uint8  

	
	Domain string
}


func DefaultDerivationConfig() DerivationConfig {
	return DerivationConfig{
		Time:    3,           
		Memory:  256 * 1024,  
		Threads: 4,           
		Domain:  "ZEAM_IDENTITY_V1",
	}
}


func LightDerivationConfig() DerivationConfig {
	return DerivationConfig{
		Time:    1,
		Memory:  64 * 1024, 
		Threads: 4,
		Domain:  "ZEAM_IDENTITY_V1",
	}
}


type StatelessIdentity struct {
	mu sync.RWMutex

	
	privateKey *ecdsa.PrivateKey
	address    common.Address
	forkID     [32]byte

	
	chainSalt   []byte
	attestation []byte

	
	derivedAt time.Time
	expiresAt time.Time

	
	gate HardwareGate
}


func DeriveIdentity(
	userSecret []byte,
	chainSalt []byte,
	gate HardwareGate,
	config DerivationConfig,
) (*StatelessIdentity, error) {
	if len(userSecret) < 8 {
		return nil, fmt.Errorf("user secret too short (minimum 8 bytes)")
	}
	if len(chainSalt) < 16 {
		return nil, fmt.Errorf("chain salt too short (minimum 16 bytes)")
	}

	
	var attestation []byte
	if gate != nil && gate.Available() && gate.Type() != GateTypeSoftware && gate.Type() != GateTypeNone {
		
		
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return nil, fmt.Errorf("failed to generate nonce: %w", err)
		}
		timestamp := time.Now().Unix()
		challenge := AttestationChallenge(chainSalt, timestamp, nonce)

		var err error
		attestation, err = gate.Attest(challenge)
		if err != nil {
			return nil, fmt.Errorf("hardware attestation failed: %w", err)
		}
	} else {
		
		
		attestation = []byte{}
		if gate == nil {
			gate = NewNoGate()
		}
	}

	
	combined := make([]byte, 0, len(userSecret)+len(chainSalt)+len(attestation))
	combined = append(combined, userSecret...)
	combined = append(combined, chainSalt...)
	combined = append(combined, attestation...)

	
	salt := crypto.Keccak256([]byte(config.Domain))

	
	seed := argon2.IDKey(
		combined,
		salt,
		config.Time,
		config.Memory,
		config.Threads,
		32, 
	)

	
	for i := range combined {
		combined[i] = 0
	}

	
	privateKey, err := crypto.ToECDSA(seed)
	if err != nil {
		
		for i := range seed {
			seed[i] = 0
		}
		return nil, fmt.Errorf("failed to derive private key: %w", err)
	}

	
	for i := range seed {
		seed[i] = 0
	}

	
	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	
	forkIDData := append(chainSalt, address.Bytes()...)
	forkID := crypto.Keccak256Hash(forkIDData)

	now := time.Now()
	identity := &StatelessIdentity{
		privateKey:  privateKey,
		address:     address,
		forkID:      forkID,
		chainSalt:   chainSalt,
		attestation: attestation,
		derivedAt:   now,
		expiresAt:   now.Add(24 * time.Hour), 
		gate:        gate,
	}

	return identity, nil
}


func (si *StatelessIdentity) Address() common.Address {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.address
}


func (si *StatelessIdentity) ForkID() [32]byte {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.forkID
}


func (si *StatelessIdentity) ShortID() string {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return fmt.Sprintf("%x...%x", si.forkID[:4], si.forkID[28:])
}


func (si *StatelessIdentity) IsValid() bool {
	si.mu.RLock()
	defer si.mu.RUnlock()

	if si.privateKey == nil {
		return false
	}
	if time.Now().After(si.expiresAt) {
		return false
	}
	return true
}


func (si *StatelessIdentity) Sign(hash []byte) ([]byte, error) {
	si.mu.RLock()
	defer si.mu.RUnlock()

	if si.privateKey == nil {
		return nil, fmt.Errorf("identity not available (locked or expired)")
	}
	if time.Now().After(si.expiresAt) {
		return nil, fmt.Errorf("session expired")
	}

	return crypto.Sign(hash, si.privateKey)
}


func (si *StatelessIdentity) SignTransaction(txHash []byte) ([]byte, error) {
	return si.Sign(txHash)
}


func (si *StatelessIdentity) Lock() {
	si.mu.Lock()
	defer si.mu.Unlock()

	if si.privateKey != nil {
		
		si.privateKey.D.SetInt64(0)
		si.privateKey = nil
	}

	
	for i := range si.attestation {
		si.attestation[i] = 0
	}
	si.attestation = nil
}


func (si *StatelessIdentity) ExtendSession(duration time.Duration) {
	si.mu.Lock()
	defer si.mu.Unlock()
	si.expiresAt = time.Now().Add(duration)
}


func (si *StatelessIdentity) GateType() GateType {
	si.mu.RLock()
	defer si.mu.RUnlock()
	if si.gate == nil {
		return GateTypeNone
	}
	return si.gate.Type()
}


func (si *StatelessIdentity) DerivedAt() time.Time {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.derivedAt
}


func (si *StatelessIdentity) ExpiresAt() time.Time {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.expiresAt
}


func (si *StatelessIdentity) ChainSalt() []byte {
	si.mu.RLock()
	defer si.mu.RUnlock()
	result := make([]byte, len(si.chainSalt))
	copy(result, si.chainSalt)
	return result
}


func GenerateChainSalt() ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate chain salt: %w", err)
	}
	return salt, nil
}


func ChainSaltFromBlockHash(blockHash []byte, randomCommitment []byte) []byte {
	data := make([]byte, 0, len(blockHash)+len(randomCommitment)+len("ZEAM_CHAIN_SALT_V1"))
	data = append(data, []byte("ZEAM_CHAIN_SALT_V1")...)
	data = append(data, blockHash...)
	data = append(data, randomCommitment...)
	return crypto.Keccak256(data)
}


func VerifyIdentity(
	userSecret []byte,
	chainSalt []byte,
	expectedAddress common.Address,
	gate HardwareGate,
	config DerivationConfig,
) (bool, error) {
	identity, err := DeriveIdentity(userSecret, chainSalt, gate, config)
	if err != nil {
		return false, err
	}
	defer identity.Lock()

	return identity.Address() == expectedAddress, nil
}


type SessionManager struct {
	mu sync.RWMutex

	current     *StatelessIdentity
	autoLockAt  time.Time
	lockTimeout time.Duration
	gate        HardwareGate
	config      DerivationConfig
}


func NewSessionManager(gate HardwareGate, config DerivationConfig) *SessionManager {
	if gate == nil {
		gate = DetectHardwareGate()
	}
	return &SessionManager{
		lockTimeout: 15 * time.Minute, 
		gate:        gate,
		config:      config,
	}
}


func (sm *SessionManager) Unlock(userSecret, chainSalt []byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	
	if sm.current != nil {
		sm.current.Lock()
	}

	identity, err := DeriveIdentity(userSecret, chainSalt, sm.gate, sm.config)
	if err != nil {
		return err
	}

	sm.current = identity
	sm.autoLockAt = time.Now().Add(sm.lockTimeout)

	return nil
}


func (sm *SessionManager) Lock() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.current != nil {
		sm.current.Lock()
		sm.current = nil
	}
}


func (sm *SessionManager) Current() *StatelessIdentity {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.current == nil {
		return nil
	}

	
	if time.Now().After(sm.autoLockAt) {
		
		sm.mu.RUnlock()
		sm.Lock()
		sm.mu.RLock()
		return nil
	}

	if !sm.current.IsValid() {
		return nil
	}

	return sm.current
}


func (sm *SessionManager) Touch() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.autoLockAt = time.Now().Add(sm.lockTimeout)
}


func (sm *SessionManager) SetLockTimeout(d time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.lockTimeout = d
	sm.autoLockAt = time.Now().Add(d)
}


func (sm *SessionManager) IsUnlocked() bool {
	return sm.Current() != nil
}
