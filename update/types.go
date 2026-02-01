package update

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type Manifest struct {
	Version   string            `json:"version"`
	Released  time.Time         `json:"released"`
	Changelog string            `json:"changelog"`
	Binaries  map[string]Binary `json:"binaries"`
	MinVersion string           `json:"min_version,omitempty"`
}

type Binary struct {
	URL    string `json:"url"`
	Hash   string `json:"hash"`
	Size   uint64 `json:"size"`
	Signature string `json:"signature,omitempty"`
}

func Platform() string {
	return platformOS + "-" + platformArch
}

type Config struct {

	RegistryAddress common.Address

	RPCURL string

	CheckInterval time.Duration

	AutoUpdate bool

	BinaryPath string

	DataDir string

	CurrentVersion string
}

func DefaultConfig() *Config {
	return &Config{
		RegistryAddress: common.Address{},
		RPCURL:          "https://mainnet.base.org",
		CheckInterval:   1 * time.Hour,
		AutoUpdate:      false,
		CurrentVersion:  "0.0.0",
	}
}

type UpdateStatus struct {
	CurrentVersion   string
	LatestVersion    string
	UpdateAvailable  bool
	ManifestHash     [32]byte
	LastChecked      time.Time
	LastError        error
	DownloadProgress float64
	State            UpdateState
}

type UpdateState int

const (
	StateIdle UpdateState = iota
	StateChecking
	StateDownloading
	StateVerifying
	StateReady
	StateApplying
	StateFailed
)

func (s UpdateState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateChecking:
		return "checking"
	case StateDownloading:
		return "downloading"
	case StateVerifying:
		return "verifying"
	case StateReady:
		return "ready"
	case StateApplying:
		return "applying"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}
