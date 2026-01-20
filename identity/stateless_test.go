package identity

import (
	"bytes"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestDeriveIdentity(t *testing.T) {
	userSecret := []byte("test-secret-phrase-123")
	chainSalt := make([]byte, 32)
	for i := range chainSalt {
		chainSalt[i] = byte(i)
	}

	
	config := LightDerivationConfig()

	
	id1, err := DeriveIdentity(userSecret, chainSalt, nil, config)
	if err != nil {
		t.Fatalf("DeriveIdentity failed: %v", err)
	}
	defer id1.Lock()

	
	if id1.Address() == (common.Address{}) {
		t.Error("Derived address is empty")
	}

	
	if id1.ForkID() == [32]byte{} {
		t.Error("Fork ID is empty")
	}

	
	if !id1.IsValid() {
		t.Error("Identity should be valid")
	}

	
	id2, err := DeriveIdentity(userSecret, chainSalt, nil, config)
	if err != nil {
		t.Fatalf("Second derivation failed: %v", err)
	}
	defer id2.Lock()

	if id1.Address() != id2.Address() {
		t.Errorf("Same inputs should derive same address: %s != %s",
			id1.Address().Hex(), id2.Address().Hex())
	}

	if id1.ForkID() != id2.ForkID() {
		t.Error("Same inputs should derive same fork ID")
	}
}

func TestDeriveIdentityDifferentInputs(t *testing.T) {
	config := LightDerivationConfig()
	chainSalt := make([]byte, 32)

	
	id1, _ := DeriveIdentity([]byte("secret-one-xxx"), chainSalt, nil, config)
	defer id1.Lock()

	id2, _ := DeriveIdentity([]byte("secret-two-xxx"), chainSalt, nil, config)
	defer id2.Lock()

	if id1.Address() == id2.Address() {
		t.Error("Different secrets should give different addresses")
	}

	
	salt1 := make([]byte, 32)
	salt2 := make([]byte, 32)
	salt2[0] = 1

	id3, _ := DeriveIdentity([]byte("same-secret-xx"), salt1, nil, config)
	defer id3.Lock()

	id4, _ := DeriveIdentity([]byte("same-secret-xx"), salt2, nil, config)
	defer id4.Lock()

	if id3.Address() == id4.Address() {
		t.Error("Different chain salts should give different addresses")
	}
}

func TestStatelessIdentitySigning(t *testing.T) {
	userSecret := []byte("signing-test-secret")
	chainSalt := make([]byte, 32)
	config := LightDerivationConfig()

	id, err := DeriveIdentity(userSecret, chainSalt, nil, config)
	if err != nil {
		t.Fatalf("DeriveIdentity failed: %v", err)
	}
	defer id.Lock()

	
	message := []byte("hello world")
	hash := crypto.Keccak256(message)

	sig, err := id.Sign(hash)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		t.Fatalf("SigToPub failed: %v", err)
	}

	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	if recoveredAddr != id.Address() {
		t.Errorf("Signature verification failed: %s != %s",
			recoveredAddr.Hex(), id.Address().Hex())
	}
}

func TestStatelessIdentityExpiry(t *testing.T) {
	userSecret := []byte("expiry-test-secret")
	chainSalt := make([]byte, 32)
	config := LightDerivationConfig()

	id, _ := DeriveIdentity(userSecret, chainSalt, nil, config)
	defer id.Lock()

	
	if !id.IsValid() {
		t.Error("Identity should be valid initially")
	}

	
	id.mu.Lock()
	id.expiresAt = time.Now().Add(-1 * time.Hour)
	id.mu.Unlock()

	
	if id.IsValid() {
		t.Error("Identity should be expired")
	}

	
	_, err := id.Sign([]byte("test"))
	if err == nil {
		t.Error("Signing should fail when expired")
	}
}

func TestStatelessIdentityLock(t *testing.T) {
	userSecret := []byte("lock-test-secret!")
	chainSalt := make([]byte, 32)
	config := LightDerivationConfig()

	id, _ := DeriveIdentity(userSecret, chainSalt, nil, config)

	
	if !id.IsValid() {
		t.Error("Identity should be valid before lock")
	}

	
	id.Lock()

	
	if id.IsValid() {
		t.Error("Identity should be invalid after lock")
	}

	
	_, err := id.Sign([]byte("test"))
	if err == nil {
		t.Error("Signing should fail after lock")
	}
}

func TestSessionManager(t *testing.T) {
	gate := NewSoftwareGate()
	config := LightDerivationConfig()

	sm := NewSessionManager(gate, config)

	
	if sm.IsUnlocked() {
		t.Error("Should be locked initially")
	}

	
	userSecret := []byte("session-test-secret")
	chainSalt := make([]byte, 32)

	err := sm.Unlock(userSecret, chainSalt)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	
	if !sm.IsUnlocked() {
		t.Error("Should be unlocked after Unlock")
	}

	current := sm.Current()
	if current == nil {
		t.Error("Current should not be nil")
	}

	
	sm.Lock()

	if sm.IsUnlocked() {
		t.Error("Should be locked after Lock")
	}
}

func TestSoftwareGate(t *testing.T) {
	gate := NewSoftwareGate()

	if !gate.Available() {
		t.Error("Software gate should always be available")
	}

	if gate.Type() != GateTypeSoftware {
		t.Errorf("Wrong gate type: %s", gate.Type())
	}

	
	challenge := []byte("test-challenge")
	attestation, err := gate.Attest(challenge)
	if err != nil {
		t.Fatalf("Attest failed: %v", err)
	}

	if len(attestation) != 64 {
		t.Errorf("Expected 64-byte attestation, got %d", len(attestation))
	}

	
	if !gate.Verify(challenge, attestation) {
		t.Error("Attestation should verify")
	}

	
	if gate.Verify([]byte("different"), attestation) {
		t.Error("Different challenge should not verify")
	}
}

func TestNoGate(t *testing.T) {
	gate := NewNoGate()

	if !gate.Available() {
		t.Error("NoGate should always be available")
	}

	if gate.Type() != GateTypeNone {
		t.Errorf("Wrong gate type: %s", gate.Type())
	}

	
	attestation, err := gate.Attest([]byte("challenge"))
	if err != nil {
		t.Fatalf("Attest failed: %v", err)
	}

	if len(attestation) != 0 {
		t.Error("NoGate attestation should be empty")
	}

	
	if !gate.Verify([]byte("challenge"), []byte{}) {
		t.Error("NoGate should verify empty attestation")
	}
}

func TestChainSaltGeneration(t *testing.T) {
	salt1, err := GenerateChainSalt()
	if err != nil {
		t.Fatalf("GenerateChainSalt failed: %v", err)
	}

	if len(salt1) != 32 {
		t.Errorf("Expected 32-byte salt, got %d", len(salt1))
	}

	
	salt2, _ := GenerateChainSalt()
	if bytes.Equal(salt1, salt2) {
		t.Error("Chain salts should be unique")
	}
}

func TestChainSaltFromBlockHash(t *testing.T) {
	blockHash := make([]byte, 32)
	for i := range blockHash {
		blockHash[i] = byte(i)
	}

	commitment := make([]byte, 32)
	for i := range commitment {
		commitment[i] = byte(255 - i)
	}

	salt := ChainSaltFromBlockHash(blockHash, commitment)

	if len(salt) != 32 {
		t.Errorf("Expected 32-byte salt, got %d", len(salt))
	}

	
	salt2 := ChainSaltFromBlockHash(blockHash, commitment)
	if !bytes.Equal(salt, salt2) {
		t.Error("Same inputs should give same salt")
	}

	
	blockHash[0] = 99
	salt3 := ChainSaltFromBlockHash(blockHash, commitment)
	if bytes.Equal(salt, salt3) {
		t.Error("Different inputs should give different salt")
	}
}

func TestVerifyIdentity(t *testing.T) {
	userSecret := []byte("verify-test-secret")
	chainSalt := make([]byte, 32)
	config := LightDerivationConfig()

	
	id, _ := DeriveIdentity(userSecret, chainSalt, nil, config)
	expectedAddr := id.Address()
	id.Lock()

	
	valid, err := VerifyIdentity(userSecret, chainSalt, expectedAddr, nil, config)
	if err != nil {
		t.Fatalf("VerifyIdentity failed: %v", err)
	}
	if !valid {
		t.Error("Verification should succeed with correct inputs")
	}

	
	valid, _ = VerifyIdentity([]byte("wrong-secret-here"), chainSalt, expectedAddr, nil, config)
	if valid {
		t.Error("Verification should fail with wrong secret")
	}
}
