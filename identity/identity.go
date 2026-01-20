

package identity

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)


type Identity struct {
	
	PrivateKey *ecdsa.PrivateKey

	
	Address common.Address

	
	ForkID [32]byte

	
	ShortID string
}


type Config struct {
	
	DataDir string

	
	ImportKey string

	
	PasskeySecret []byte

	
	RequirePasskey bool
}


func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".zeam"
	}
	return filepath.Join(home, ".zeam")
}


func New(cfg Config) (*Identity, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}

	
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	keyPath := filepath.Join(cfg.DataDir, "identity.key")
	encKeyPath := filepath.Join(cfg.DataDir, "identity.enc")

	var privateKey *ecdsa.PrivateKey
	var err error

	
	if cfg.ImportKey != "" {
		privateKey, err = crypto.HexToECDSA(cfg.ImportKey)
		if err != nil {
			return nil, fmt.Errorf("invalid import key: %w", err)
		}
		
		if len(cfg.PasskeySecret) > 0 {
			enc, err := EncryptPrivateKey(privateKey, cfg.PasskeySecret)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt imported key: %w", err)
			}
			if err := SaveEncryptedKey(encKeyPath, enc); err != nil {
				return nil, fmt.Errorf("failed to save encrypted key: %w", err)
			}
			
			os.Remove(keyPath)
		} else {
			if err := saveKey(keyPath, privateKey); err != nil {
				return nil, fmt.Errorf("failed to save imported key: %w", err)
			}
		}
	} else if fileExists(encKeyPath) {
		
		if len(cfg.PasskeySecret) == 0 {
			return nil, fmt.Errorf("encrypted key found - passkey required to unlock")
		}
		enc, err := LoadEncryptedKey(encKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load encrypted key: %w", err)
		}
		privateKey, err = DecryptPrivateKey(enc, cfg.PasskeySecret)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt key: %w", err)
		}
	} else if fileExists(keyPath) {
		
		if cfg.RequirePasskey {
			return nil, fmt.Errorf("unencrypted key found but passkey required")
		}
		privateKey, err = loadKey(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load identity: %w", err)
		}
		
		if len(cfg.PasskeySecret) > 0 {
			enc, err := EncryptPrivateKey(privateKey, cfg.PasskeySecret)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt key during migration: %w", err)
			}
			if err := SaveEncryptedKey(encKeyPath, enc); err != nil {
				return nil, fmt.Errorf("failed to save encrypted key: %w", err)
			}
			
			os.Remove(keyPath)
		}
	} else {
		
		privateKey, err = crypto.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("failed to generate key: %w", err)
		}
		
		if len(cfg.PasskeySecret) > 0 {
			enc, err := EncryptPrivateKey(privateKey, cfg.PasskeySecret)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt new key: %w", err)
			}
			if err := SaveEncryptedKey(encKeyPath, enc); err != nil {
				return nil, fmt.Errorf("failed to save encrypted key: %w", err)
			}
		} else {
			if err := saveKey(keyPath, privateKey); err != nil {
				return nil, fmt.Errorf("failed to save identity: %w", err)
			}
		}
	}

	
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	forkID := crypto.Keccak256Hash(address.Bytes())

	return &Identity{
		PrivateKey: privateKey,
		Address:    address,
		ForkID:     forkID,
		ShortID:    address.Hex()[2:10], 
	}, nil
}


func (id *Identity) Sign(data []byte) ([]byte, error) {
	hash := crypto.Keccak256(data)
	return crypto.Sign(hash, id.PrivateKey)
}


func Verify(data []byte, sig []byte, address common.Address) bool {
	hash := crypto.Keccak256(data)
	pubKey, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return false
	}
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	return recoveredAddr == address
}


func (id *Identity) Export() string {
	return hex.EncodeToString(crypto.FromECDSA(id.PrivateKey))
}


func (id *Identity) String() string {
	return fmt.Sprintf("ZEAM Identity: %s (Fork: 0x%s...)", id.Address.Hex(), hex.EncodeToString(id.ForkID[:4]))
}


func (id *Identity) GetForkGenesis() []byte {
	genesis := struct {
		Version   int            `json:"version"`
		ForkID    string         `json:"fork_id"`
		Address   string         `json:"address"`
		Timestamp int64          `json:"timestamp"`
	}{
		Version: 1,
		ForkID:  hex.EncodeToString(id.ForkID[:]),
		Address: id.Address.Hex(),
		Timestamp: 0, 
	}

	data, _ := json.Marshal(genesis)
	return data
}


func saveKey(path string, key *ecdsa.PrivateKey) error {
	keyHex := hex.EncodeToString(crypto.FromECDSA(key))
	return os.WriteFile(path, []byte(keyHex), 0600)
}


func loadKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return crypto.HexToECDSA(string(data))
}


func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}


func GenerateNewKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(crypto.S256(), rand.Reader)
}
