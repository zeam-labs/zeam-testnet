package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/argon2"
)

type EncryptedKey struct {

	Version int `json:"version"`

	Salt []byte `json:"salt"`

	Nonce []byte `json:"nonce"`

	Ciphertext []byte `json:"ciphertext"`

	CredentialID []byte `json:"credential_id,omitempty"`
}

type PasskeyConfig struct {

	RelyingPartyID string

	UserID string
}

func DeriveKeyFromPasskey(secret []byte, salt []byte) []byte {

	return argon2.IDKey(secret, salt, 3, 64*1024, 4, 32)
}

func EncryptPrivateKey(privateKey *ecdsa.PrivateKey, passkeySecret []byte) (*EncryptedKey, error) {

	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	encKey := DeriveKeyFromPasskey(passkeySecret, salt)

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	plaintext := crypto.FromECDSA(privateKey)
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	for i := range plaintext {
		plaintext[i] = 0
	}

	return &EncryptedKey{
		Version:    1,
		Salt:       salt,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}, nil
}

func DecryptPrivateKey(enc *EncryptedKey, passkeySecret []byte) (*ecdsa.PrivateKey, error) {

	encKey := DeriveKeyFromPasskey(passkeySecret, enc.Salt)

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, enc.Nonce, enc.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong passkey?): %w", err)
	}

	privateKey, err := crypto.ToECDSA(plaintext)

	for i := range plaintext {
		plaintext[i] = 0
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse decrypted key: %w", err)
	}

	return privateKey, nil
}

func SaveEncryptedKey(path string, enc *EncryptedKey) error {
	data, err := json.MarshalIndent(enc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal encrypted key: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func LoadEncryptedKey(path string) (*EncryptedKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var enc EncryptedKey
	if err := json.Unmarshal(data, &enc); err != nil {
		return nil, fmt.Errorf("failed to parse encrypted key: %w", err)
	}

	return &enc, nil
}

func IsEncryptedKeyFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	return len(data) > 0 && data[0] == '{'
}

func HashPasskeySecret(credentialID []byte, clientDataJSON []byte, authenticatorData []byte) []byte {
	h := sha256.New()
	h.Write(credentialID)
	h.Write(clientDataJSON)
	h.Write(authenticatorData)
	return h.Sum(nil)
}

func GenerateRecoveryPhrase() (string, []byte, error) {

	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return "", nil, fmt.Errorf("failed to generate entropy: %w", err)
	}

	phrase := hex.EncodeToString(entropy)

	secret := sha256.Sum256(entropy)

	return phrase, secret[:], nil
}

func RecoveryPhraseToSecret(phrase string) ([]byte, error) {
	entropy, err := hex.DecodeString(phrase)
	if err != nil {
		return nil, fmt.Errorf("invalid recovery phrase: %w", err)
	}

	secret := sha256.Sum256(entropy)
	return secret[:], nil
}
