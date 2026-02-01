package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"runtime"
	"sync"

	"github.com/ethereum/go-ethereum/crypto"
)

type HardwareGate interface {

	Available() bool

	Type() GateType

	Attest(challenge []byte) ([]byte, error)

	Verify(challenge, attestation []byte) bool

	PublicKey() ([]byte, error)
}

type GateType string

const (
	GateTypeSecureEnclave GateType = "secure_enclave"
	GateTypeTPM           GateType = "tpm"
	GateTypeWebAuthn      GateType = "webauthn"
	GateTypeSoftware      GateType = "software"
	GateTypeNone          GateType = "none"
)

type GateCapabilities struct {

	CanAttest bool

	CanVerify bool

	KeyNeverExportable bool

	RequiresBiometric bool

	RequiresPIN bool
}

func DetectHardwareGate() HardwareGate {
	switch runtime.GOOS {
	case "darwin":

		gate := NewSecureEnclaveGate()
		if gate.Available() {
			return gate
		}

	case "windows", "linux":

		gate := NewTPMGate()
		if gate.Available() {
			return gate
		}

	}

	return NewSoftwareGate()
}

type SecureEnclaveGate struct {
	mu        sync.Mutex
	available bool
	pubKey    []byte
}

func NewSecureEnclaveGate() *SecureEnclaveGate {
	gate := &SecureEnclaveGate{}

	gate.available = false
	return gate
}

func (g *SecureEnclaveGate) Available() bool {
	return g.available
}

func (g *SecureEnclaveGate) Type() GateType {
	return GateTypeSecureEnclave
}

func (g *SecureEnclaveGate) Attest(challenge []byte) ([]byte, error) {
	if !g.available {
		return nil, fmt.Errorf("Secure Enclave not available")
	}

	return nil, fmt.Errorf("Secure Enclave not implemented - requires native code")
}

func (g *SecureEnclaveGate) Verify(challenge, attestation []byte) bool {

	return false
}

func (g *SecureEnclaveGate) PublicKey() ([]byte, error) {
	if !g.available {
		return nil, fmt.Errorf("Secure Enclave not available")
	}
	return g.pubKey, nil
}

func (g *SecureEnclaveGate) Capabilities() GateCapabilities {
	return GateCapabilities{
		CanAttest:          true,
		CanVerify:          true,
		KeyNeverExportable: true,
		RequiresBiometric:  true,
		RequiresPIN:        false,
	}
}

type TPMGate struct {
	mu        sync.Mutex
	available bool
	pubKey    []byte
}

func NewTPMGate() *TPMGate {
	gate := &TPMGate{}

	gate.available = false
	return gate
}

func (g *TPMGate) Available() bool {
	return g.available
}

func (g *TPMGate) Type() GateType {
	return GateTypeTPM
}

func (g *TPMGate) Attest(challenge []byte) ([]byte, error) {
	if !g.available {
		return nil, fmt.Errorf("TPM not available")
	}

	return nil, fmt.Errorf("TPM not implemented - requires native code")
}

func (g *TPMGate) Verify(challenge, attestation []byte) bool {
	return false
}

func (g *TPMGate) PublicKey() ([]byte, error) {
	if !g.available {
		return nil, fmt.Errorf("TPM not available")
	}
	return g.pubKey, nil
}

func (g *TPMGate) Capabilities() GateCapabilities {
	return GateCapabilities{
		CanAttest:          true,
		CanVerify:          true,
		KeyNeverExportable: true,
		RequiresBiometric:  false,
		RequiresPIN:        true,
	}
}

type SoftwareGate struct {
	mu         sync.Mutex
	privateKey *ecdsa.PrivateKey
	available  bool
}

func NewSoftwareGate() *SoftwareGate {
	gate := &SoftwareGate{
		available: true,
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		gate.available = false
		return gate
	}
	gate.privateKey = key
	return gate
}

func (g *SoftwareGate) Available() bool {
	return g.available
}

func (g *SoftwareGate) Type() GateType {
	return GateTypeSoftware
}

func (g *SoftwareGate) Attest(challenge []byte) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.available || g.privateKey == nil {
		return nil, fmt.Errorf("software gate not available")
	}

	hash := sha256.Sum256(challenge)

	r, s, err := ecdsa.Sign(rand.Reader, g.privateKey, hash[:])
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	sig := make([]byte, 64)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)

	return sig, nil
}

func (g *SoftwareGate) Verify(challenge, attestation []byte) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.privateKey == nil || len(attestation) != 64 {
		return false
	}

	hash := sha256.Sum256(challenge)

	r := new(big.Int).SetBytes(attestation[:32])
	s := new(big.Int).SetBytes(attestation[32:])

	return ecdsa.Verify(&g.privateKey.PublicKey, hash[:], r, s)
}

func (g *SoftwareGate) PublicKey() ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.privateKey == nil {
		return nil, fmt.Errorf("no key available")
	}

	return elliptic.MarshalCompressed(g.privateKey.Curve, g.privateKey.PublicKey.X, g.privateKey.PublicKey.Y), nil
}

func (g *SoftwareGate) Capabilities() GateCapabilities {
	return GateCapabilities{
		CanAttest:          true,
		CanVerify:          true,
		KeyNeverExportable: false,
		RequiresBiometric:  false,
		RequiresPIN:        false,
	}
}

func (g *SoftwareGate) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.privateKey != nil {

		g.privateKey.D.SetInt64(0)
		g.privateKey = nil
	}
}

type NoGate struct{}

func NewNoGate() *NoGate {
	return &NoGate{}
}

func (g *NoGate) Available() bool {
	return true
}

func (g *NoGate) Type() GateType {
	return GateTypeNone
}

func (g *NoGate) Attest(challenge []byte) ([]byte, error) {

	return []byte{}, nil
}

func (g *NoGate) Verify(challenge, attestation []byte) bool {

	return len(attestation) == 0
}

func (g *NoGate) PublicKey() ([]byte, error) {
	return []byte{}, nil
}

func (g *NoGate) Capabilities() GateCapabilities {
	return GateCapabilities{
		CanAttest:          false,
		CanVerify:          false,
		KeyNeverExportable: false,
		RequiresBiometric:  false,
		RequiresPIN:        false,
	}
}

func AttestationChallenge(chainSalt []byte, timestamp int64, nonce []byte) []byte {

	domain := []byte("ZEAM_ATTESTATION_V1")

	data := make([]byte, 0, len(domain)+len(chainSalt)+8+len(nonce))
	data = append(data, domain...)
	data = append(data, chainSalt...)

	ts := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		ts[i] = byte(timestamp)
		timestamp >>= 8
	}
	data = append(data, ts...)
	data = append(data, nonce...)

	hash := crypto.Keccak256(data)
	return hash
}
