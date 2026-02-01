package identity

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type ProofType string

const (

	ProofExistence ProofType = "existence"

	ProofRange ProofType = "range"

	ProofMembership ProofType = "membership"

	ProofEquality ProofType = "equality"

	ProofComposite ProofType = "composite"

	ProofPredicate ProofType = "predicate"
)

type SelectiveDisclosureProof struct {

	ID [32]byte `json:"id"`

	Type ProofType `json:"type"`

	Prover common.Address `json:"prover"`

	StreamID [32]byte `json:"stream_id"`

	Commitment []byte `json:"commitment"`

	Claim string `json:"claim"`

	ProofData []byte `json:"proof_data"`

	Created int64 `json:"created"`

	ValidUntil int64 `json:"valid_until,omitempty"`

	Revocable bool `json:"revocable"`

	Revoked bool `json:"revoked"`

	RevokedAt int64 `json:"revoked_at,omitempty"`

	Nonce []byte `json:"nonce"`

	Signature []byte `json:"signature"`
}

type RangeProofData struct {

	Min *big.Int `json:"min"`

	Max *big.Int `json:"max"`

	ValueCommitment []byte `json:"value_commitment"`
	RangeCommitment []byte `json:"range_commitment"`

	Challenge []byte `json:"challenge"`
	Response  []byte `json:"response"`
}

type MembershipProofData struct {

	SetCommitment []byte `json:"set_commitment"`

	MerkleRoot []byte `json:"merkle_root"`

	MerklePath [][]byte `json:"merkle_path"`

	PathIndices []bool `json:"path_indices"`

	LeafCommitment []byte `json:"leaf_commitment"`
}

type PredicateProofData struct {

	Predicate string `json:"predicate"`

	InputCommitments [][]byte `json:"input_commitments"`

	OutputCommitment []byte `json:"output_commitment"`

	CircuitProof []byte `json:"circuit_proof"`
}

type CompositeProofData struct {

	SubProofs [][32]byte `json:"sub_proofs"`

	Operator string `json:"operator"`

	Threshold int `json:"threshold,omitempty"`

	BindingProof []byte `json:"binding_proof"`
}

func (sdp *SelectiveDisclosureProof) GenerateID() {
	data, _ := json.Marshal(struct {
		Type     ProofType
		Prover   common.Address
		StreamID [32]byte
		Claim    string
		Created  int64
		Nonce    []byte
	}{sdp.Type, sdp.Prover, sdp.StreamID, sdp.Claim, sdp.Created, sdp.Nonce})

	sdp.ID = crypto.Keccak256Hash(data)
}

func (sdp *SelectiveDisclosureProof) Sign(privateKey *ecdsa.PrivateKey) error {
	addr := crypto.PubkeyToAddress(privateKey.PublicKey)
	if addr != sdp.Prover {
		return fmt.Errorf("signer %s is not prover %s", addr.Hex(), sdp.Prover.Hex())
	}

	sdp.Signature = nil
	data, err := json.Marshal(sdp)
	if err != nil {
		return fmt.Errorf("failed to marshal for signing: %w", err)
	}

	hash := crypto.Keccak256(data)
	sig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	sdp.Signature = sig
	return nil
}

func (sdp *SelectiveDisclosureProof) Verify() bool {
	if len(sdp.Signature) == 0 {
		return false
	}

	sig := sdp.Signature
	sdp.Signature = nil
	data, err := json.Marshal(sdp)
	sdp.Signature = sig
	if err != nil {
		return false
	}

	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false
	}

	return crypto.PubkeyToAddress(*pubKey) == sdp.Prover
}

func (sdp *SelectiveDisclosureProof) IsValid() bool {
	if sdp.Revoked {
		return false
	}

	now := time.Now().Unix()
	if sdp.ValidUntil > 0 && now > sdp.ValidUntil {
		return false
	}

	return sdp.Verify()
}

func (sdp *SelectiveDisclosureProof) Revoke() {
	sdp.Revoked = true
	sdp.RevokedAt = time.Now().Unix()
}

func CreateCommitment(value []byte, blinding []byte) []byte {

	h := sha256.New()
	h.Write(value)
	h.Write(blinding)
	return h.Sum(nil)
}

func VerifyCommitment(commitment, value, blinding []byte) bool {
	expected := CreateCommitment(value, blinding)
	if len(commitment) != len(expected) {
		return false
	}
	for i := range commitment {
		if commitment[i] != expected[i] {
			return false
		}
	}
	return true
}

func CreateExistenceProof(
	privateKey *ecdsa.PrivateKey,
	streamID [32]byte,
	data []byte,
	validUntil int64,
) (*SelectiveDisclosureProof, []byte, error) {
	prover := crypto.PubkeyToAddress(privateKey.PublicKey)

	blinding := make([]byte, 32)
	copy(blinding, crypto.Keccak256(data, []byte("blinding"))[:32])

	commitment := CreateCommitment(data, blinding)

	now := time.Now().Unix()
	nonce := crypto.Keccak256(data, []byte(fmt.Sprintf("%d", now)))[:16]

	proof := &SelectiveDisclosureProof{
		Type:       ProofExistence,
		Prover:     prover,
		StreamID:   streamID,
		Commitment: commitment,
		Claim:      "data_exists",
		ProofData:  nil,
		Created:    now,
		ValidUntil: validUntil,
		Revocable:  true,
		Nonce:      nonce,
	}

	proof.GenerateID()
	if err := proof.Sign(privateKey); err != nil {
		return nil, nil, err
	}

	return proof, blinding, nil
}

func CreateRangeProof(
	privateKey *ecdsa.PrivateKey,
	streamID [32]byte,
	value *big.Int,
	min, max *big.Int,
	claim string,
	validUntil int64,
) (*SelectiveDisclosureProof, error) {
	prover := crypto.PubkeyToAddress(privateKey.PublicKey)

	if value.Cmp(min) < 0 || value.Cmp(max) > 0 {
		return nil, fmt.Errorf("value is not in range [%s, %s]", min.String(), max.String())
	}

	valueBytes := value.Bytes()
	blinding := crypto.Keccak256(valueBytes, []byte("range_blinding"))[:32]
	valueCommitment := CreateCommitment(valueBytes, blinding)

	rangeData := append(min.Bytes(), max.Bytes()...)
	rangeCommitment := CreateCommitment(rangeData, blinding)

	challenge := crypto.Keccak256(valueCommitment, rangeCommitment)

	response := crypto.Keccak256(challenge, blinding)

	rangeProofData := RangeProofData{
		Min:             min,
		Max:             max,
		ValueCommitment: valueCommitment,
		RangeCommitment: rangeCommitment,
		Challenge:       challenge,
		Response:        response,
	}

	proofDataBytes, _ := json.Marshal(rangeProofData)

	now := time.Now().Unix()
	nonce := crypto.Keccak256(valueBytes, []byte(fmt.Sprintf("%d", now)))[:16]

	proof := &SelectiveDisclosureProof{
		Type:       ProofRange,
		Prover:     prover,
		StreamID:   streamID,
		Commitment: valueCommitment,
		Claim:      claim,
		ProofData:  proofDataBytes,
		Created:    now,
		ValidUntil: validUntil,
		Revocable:  true,
		Nonce:      nonce,
	}

	proof.GenerateID()
	if err := proof.Sign(privateKey); err != nil {
		return nil, err
	}

	return proof, nil
}

func CreateMembershipProof(
	privateKey *ecdsa.PrivateKey,
	streamID [32]byte,
	element []byte,
	set [][]byte,
	claim string,
	validUntil int64,
) (*SelectiveDisclosureProof, error) {
	prover := crypto.PubkeyToAddress(privateKey.PublicKey)

	elementIndex := -1
	for i, e := range set {
		if string(e) == string(element) {
			elementIndex = i
			break
		}
	}
	if elementIndex < 0 {
		return nil, fmt.Errorf("element not in set")
	}

	leaves := make([][]byte, len(set))
	for i, e := range set {
		leaves[i] = crypto.Keccak256(e)
	}

	root, path, pathIndices := computeMerkleProof(leaves, elementIndex)

	blinding := crypto.Keccak256(element, []byte("membership_blinding"))[:32]
	leafCommitment := CreateCommitment(element, blinding)
	setCommitment := CreateCommitment(root, blinding)

	membershipData := MembershipProofData{
		SetCommitment:  setCommitment,
		MerkleRoot:     root,
		MerklePath:     path,
		PathIndices:    pathIndices,
		LeafCommitment: leafCommitment,
	}

	proofDataBytes, _ := json.Marshal(membershipData)

	now := time.Now().Unix()
	nonce := crypto.Keccak256(element, []byte(fmt.Sprintf("%d", now)))[:16]

	proof := &SelectiveDisclosureProof{
		Type:       ProofMembership,
		Prover:     prover,
		StreamID:   streamID,
		Commitment: leafCommitment,
		Claim:      claim,
		ProofData:  proofDataBytes,
		Created:    now,
		ValidUntil: validUntil,
		Revocable:  true,
		Nonce:      nonce,
	}

	proof.GenerateID()
	if err := proof.Sign(privateKey); err != nil {
		return nil, err
	}

	return proof, nil
}

func computeMerkleProof(leaves [][]byte, index int) ([]byte, [][]byte, []bool) {
	if len(leaves) == 0 {
		return nil, nil, nil
	}

	n := 1
	for n < len(leaves) {
		n *= 2
	}
	for len(leaves) < n {
		leaves = append(leaves, crypto.Keccak256([]byte{}))
	}

	var path [][]byte
	var pathIndices []bool

	currentLevel := leaves
	currentIndex := index

	for len(currentLevel) > 1 {
		var nextLevel [][]byte

		for i := 0; i < len(currentLevel); i += 2 {
			left := currentLevel[i]
			right := currentLevel[i+1]

			if i == currentIndex || i+1 == currentIndex {
				if currentIndex%2 == 0 {
					path = append(path, right)
					pathIndices = append(pathIndices, true)
				} else {
					path = append(path, left)
					pathIndices = append(pathIndices, false)
				}
			}

			combined := crypto.Keccak256(append(left, right...))
			nextLevel = append(nextLevel, combined)
		}

		currentLevel = nextLevel
		currentIndex = currentIndex / 2
	}

	return currentLevel[0], path, pathIndices
}

func VerifyMerkleProof(leaf, root []byte, path [][]byte, pathIndices []bool) bool {
	current := leaf
	for i, sibling := range path {
		if pathIndices[i] {

			current = crypto.Keccak256(append(current, sibling...))
		} else {

			current = crypto.Keccak256(append(sibling, current...))
		}
	}

	if len(current) != len(root) {
		return false
	}
	for i := range current {
		if current[i] != root[i] {
			return false
		}
	}
	return true
}

type ProofManager struct {
	mu sync.RWMutex

	Owner common.Address

	Proofs map[[32]byte]*SelectiveDisclosureProof

	ProofsByStream map[[32]byte][][32]byte

	ProofsByType map[ProofType][][32]byte

	VerificationLog []VerificationEntry
}

type VerificationEntry struct {
	ProofID   [32]byte       `json:"proof_id"`
	Verifier  common.Address `json:"verifier"`
	Timestamp int64          `json:"timestamp"`
	Result    bool           `json:"result"`
}

func NewProofManager(owner common.Address) *ProofManager {
	return &ProofManager{
		Owner:           owner,
		Proofs:          make(map[[32]byte]*SelectiveDisclosureProof),
		ProofsByStream:  make(map[[32]byte][][32]byte),
		ProofsByType:    make(map[ProofType][][32]byte),
		VerificationLog: make([]VerificationEntry, 0),
	}
}

func (pm *ProofManager) AddProof(proof *SelectiveDisclosureProof) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !proof.Verify() {
		return fmt.Errorf("invalid proof signature")
	}

	pm.Proofs[proof.ID] = proof
	pm.ProofsByStream[proof.StreamID] = append(pm.ProofsByStream[proof.StreamID], proof.ID)
	pm.ProofsByType[proof.Type] = append(pm.ProofsByType[proof.Type], proof.ID)

	return nil
}

func (pm *ProofManager) GetProof(proofID [32]byte) *SelectiveDisclosureProof {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.Proofs[proofID]
}

func (pm *ProofManager) VerifyProof(proofID [32]byte, verifier common.Address) (bool, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	proof, exists := pm.Proofs[proofID]
	if !exists {
		return false, fmt.Errorf("proof not found")
	}

	result := proof.IsValid()

	pm.VerificationLog = append(pm.VerificationLog, VerificationEntry{
		ProofID:   proofID,
		Verifier:  verifier,
		Timestamp: time.Now().Unix(),
		Result:    result,
	})

	return result, nil
}

func (pm *ProofManager) RevokeProof(proofID [32]byte, revoker common.Address) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	proof, exists := pm.Proofs[proofID]
	if !exists {
		return fmt.Errorf("proof not found")
	}

	if !proof.Revocable {
		return fmt.Errorf("proof is not revocable")
	}

	if revoker != proof.Prover {
		return fmt.Errorf("only prover can revoke")
	}

	proof.Revoke()
	return nil
}

func (pm *ProofManager) GetProofsForStream(streamID [32]byte) []*SelectiveDisclosureProof {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var result []*SelectiveDisclosureProof
	for _, id := range pm.ProofsByStream[streamID] {
		if proof := pm.Proofs[id]; proof != nil && proof.IsValid() {
			result = append(result, proof)
		}
	}
	return result
}

func (pm *ProofManager) ExportProofs() ([]byte, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return json.Marshal(pm.Proofs)
}

func IntToBytes(n int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(n))
	return b
}

func BytesToInt(b []byte) int64 {
	if len(b) < 8 {
		padded := make([]byte, 8)
		copy(padded[8-len(b):], b)
		b = padded
	}
	return int64(binary.BigEndian.Uint64(b))
}
