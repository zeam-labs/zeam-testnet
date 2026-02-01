package identity

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type IdentityMode int

const (

	ModeFileBased IdentityMode = iota

	ModeStateless

	ModeHybrid
)

type UnifiedIdentity struct {

	PrivateKey *ecdsa.PrivateKey
	Address    common.Address
	ForkID     [32]byte
	ShortID    string

	Mode IdentityMode

	stateless *StatelessIdentity

	forkState *ForkState

	dataDir string
}

type UnifiedConfig struct {

	DataDir string

	Mode IdentityMode

	ImportKey     string
	PasskeySecret []byte

	UserSecret  []byte
	ChainSalt   []byte
	ChainID     string
	Gate        HardwareGate
	DerivConfig DerivationConfig
}

func NewUnifiedIdentity(cfg UnifiedConfig) (*UnifiedIdentity, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}

	return newStatelessIdentity(cfg)
}

func newFileBasedIdentity(cfg UnifiedConfig) (*UnifiedIdentity, error) {
	id, err := New(Config{
		DataDir:       cfg.DataDir,
		ImportKey:     cfg.ImportKey,
		PasskeySecret: cfg.PasskeySecret,
	})
	if err != nil {
		return nil, err
	}

	return &UnifiedIdentity{
		PrivateKey: id.PrivateKey,
		Address:    id.Address,
		ForkID:     id.ForkID,
		ShortID:    id.ShortID,
		Mode:       ModeFileBased,
		dataDir:    cfg.DataDir,
	}, nil
}

func newStatelessIdentity(cfg UnifiedConfig) (*UnifiedIdentity, error) {
	log.Printf("[STATELESS] newStatelessIdentity called, DataDir=%s, SecretLen=%d", cfg.DataDir, len(cfg.UserSecret))

	if len(cfg.UserSecret) < 8 {
		return nil, fmt.Errorf("user secret too short (minimum 8 bytes)")
	}

	chainSalt := cfg.ChainSalt
	if len(chainSalt) == 0 {

		saltPath := filepath.Join(cfg.DataDir, "chain_salt.anchor")
		log.Printf("[STATELESS] Checking for chain salt at: %s\n", saltPath)
		if data, err := os.ReadFile(saltPath); err == nil {
			log.Printf("[STATELESS] Found existing chain salt (%d bytes)\n", len(data))
			chainSalt = data
		} else {
			log.Printf("[STATELESS] No chain salt found, generating new one. Error was: %v\n", err)

			var err error
			chainSalt, err = GenerateChainSalt()
			if err != nil {
				return nil, fmt.Errorf("failed to generate chain salt: %w", err)
			}

			log.Printf("[STATELESS] Creating data dir: %s\n", cfg.DataDir)
			if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
				log.Printf("[STATELESS] Failed to create data dir: %v\n", err)
				return nil, err
			}
			log.Printf("[STATELESS] Writing chain salt to: %s\n", saltPath)
			if err := os.WriteFile(saltPath, chainSalt, 0600); err != nil {
				log.Printf("[STATELESS] Failed to write chain salt: %v\n", err)
				return nil, fmt.Errorf("failed to save chain salt: %w", err)
			}
			log.Printf("[STATELESS] Chain salt saved successfully!\n")
		}
	}

	derivConfig := cfg.DerivConfig
	if derivConfig.Domain == "" {
		derivConfig = DefaultDerivationConfig()
	}

	gate := cfg.Gate
	if gate == nil {
		gate = DetectHardwareGate()
	}

	stateless, err := DeriveIdentity(cfg.UserSecret, chainSalt, gate, derivConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to derive identity: %w", err)
	}

	return &UnifiedIdentity{
		PrivateKey: stateless.privateKey,
		Address:    stateless.Address(),
		ForkID:     stateless.ForkID(),
		ShortID:    stateless.ShortID(),
		Mode:       ModeStateless,
		stateless:  stateless,
		dataDir:    cfg.DataDir,
	}, nil
}

func newHybridIdentity(cfg UnifiedConfig) (*UnifiedIdentity, error) {

	if len(cfg.UserSecret) > 0 && len(cfg.ChainSalt) > 0 {
		return newStatelessIdentity(cfg)
	}

	saltPath := filepath.Join(cfg.DataDir, "chain_salt.anchor")
	if _, err := os.Stat(saltPath); err == nil && len(cfg.UserSecret) > 0 {
		return newStatelessIdentity(cfg)
	}

	return newFileBasedIdentity(cfg)
}

func (ui *UnifiedIdentity) IsStateless() bool {
	return ui.Mode == ModeStateless && ui.stateless != nil
}

func (ui *UnifiedIdentity) GetStatelessIdentity() *StatelessIdentity {
	return ui.stateless
}

func (ui *UnifiedIdentity) GetForkState() *ForkState {
	return ui.forkState
}

func (ui *UnifiedIdentity) Sign(data []byte) ([]byte, error) {
	if ui.stateless != nil {
		return ui.stateless.Sign(crypto.Keccak256(data))
	}
	if ui.PrivateKey == nil {
		return nil, fmt.Errorf("no private key available")
	}
	hash := crypto.Keccak256(data)
	return crypto.Sign(hash, ui.PrivateKey)
}

func (ui *UnifiedIdentity) Export() (string, error) {
	if ui.Mode == ModeStateless {
		return "", fmt.Errorf("stateless identity cannot be exported - re-derive with user secret")
	}
	if ui.PrivateKey == nil {
		return "", fmt.Errorf("no private key available")
	}
	return hex.EncodeToString(crypto.FromECDSA(ui.PrivateKey)), nil
}

func (ui *UnifiedIdentity) Lock() {
	if ui.stateless != nil {
		ui.stateless.Lock()
		ui.stateless = nil
	}
	if ui.PrivateKey != nil {
		ui.PrivateKey.D.SetInt64(0)
		ui.PrivateKey = nil
	}
}

func (ui *UnifiedIdentity) IsValid() bool {
	if ui.stateless != nil {
		return ui.stateless.IsValid()
	}
	return ui.PrivateKey != nil
}

func (ui *UnifiedIdentity) GateType() GateType {
	if ui.stateless != nil {
		return ui.stateless.GateType()
	}
	return GateTypeNone
}

type MigrationInfo struct {
	Address       common.Address `json:"address"`
	ForkID        [32]byte       `json:"fork_id"`
	HasEncrypted  bool           `json:"has_encrypted"`
	HasPlain      bool           `json:"has_plain"`
	DataDir       string         `json:"data_dir"`
	MigratedAt    *time.Time     `json:"migrated_at,omitempty"`
	NewChainSalt  []byte         `json:"new_chain_salt,omitempty"`
}

func CheckMigrationStatus(dataDir string) (*MigrationInfo, error) {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}

	info := &MigrationInfo{
		DataDir:      dataDir,
		HasEncrypted: fileExists(filepath.Join(dataDir, "identity.enc")),
		HasPlain:     fileExists(filepath.Join(dataDir, "identity.key")),
	}

	if !info.HasEncrypted && !info.HasPlain {
		return nil, nil
	}

	migratedPath := filepath.Join(dataDir, "identity.migrated")
	if data, err := os.ReadFile(migratedPath); err == nil {
		var migrated struct {
			MigratedAt   time.Time `json:"migrated_at"`
			NewChainSalt []byte    `json:"new_chain_salt"`
		}
		if json.Unmarshal(data, &migrated) == nil {
			info.MigratedAt = &migrated.MigratedAt
			info.NewChainSalt = migrated.NewChainSalt
		}
	}

	return info, nil
}

func MigrateToStateless(
	dataDir string,
	passkey []byte,
	newUserSecret []byte,
	gate HardwareGate,
) (*UnifiedIdentity, []byte, error) {

	oldID, err := New(Config{
		DataDir:       dataDir,
		PasskeySecret: passkey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load old identity: %w", err)
	}

	commitment, err := GenerateChainSalt()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate commitment: %w", err)
	}

	chainSalt := ChainSaltFromBlockHash(oldID.Address.Bytes(), commitment)

	if gate == nil {
		gate = DetectHardwareGate()
	}

	stateless, err := DeriveIdentity(newUserSecret, chainSalt, gate, DefaultDerivationConfig())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to derive stateless identity: %w", err)
	}

	migrated := struct {
		MigratedAt   time.Time      `json:"migrated_at"`
		OldAddress   common.Address `json:"old_address"`
		NewAddress   common.Address `json:"new_address"`
		NewChainSalt []byte         `json:"new_chain_salt"`
	}{
		MigratedAt:   time.Now(),
		OldAddress:   oldID.Address,
		NewAddress:   stateless.Address(),
		NewChainSalt: chainSalt,
	}

	migratedData, _ := json.MarshalIndent(migrated, "", "  ")
	migratedPath := filepath.Join(dataDir, "identity.migrated")
	if err := os.WriteFile(migratedPath, migratedData, 0600); err != nil {
		return nil, nil, fmt.Errorf("failed to write migration marker: %w", err)
	}

	saltPath := filepath.Join(dataDir, "chain_salt.anchor")
	if err := os.WriteFile(saltPath, chainSalt, 0600); err != nil {
		return nil, nil, fmt.Errorf("failed to save chain salt: %w", err)
	}

	unified := &UnifiedIdentity{
		PrivateKey: stateless.privateKey,
		Address:    stateless.Address(),
		ForkID:     stateless.ForkID(),
		ShortID:    stateless.ShortID(),
		Mode:       ModeStateless,
		stateless:  stateless,
		dataDir:    dataDir,
	}

	return unified, chainSalt, nil
}

func CreateGenesisForMigration(
	ui *UnifiedIdentity,
	oldAddress common.Address,
	recoveryConfig RecoveryConfig,
) (*ForkGenesis, error) {
	if ui.stateless == nil {
		return nil, fmt.Errorf("stateless identity required for genesis")
	}

	genesis, err := NewForkGenesis(ui.stateless, recoveryConfig)
	if err != nil {
		return nil, err
	}

	return genesis, nil
}

func DetectIdentityMode(dataDir string) IdentityMode {
	return ModeStateless
}

func QuickStateless(userSecret []byte) (*UnifiedIdentity, error) {
	return NewUnifiedIdentity(UnifiedConfig{
		Mode:       ModeStateless,
		UserSecret: userSecret,
	})
}
