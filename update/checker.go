package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"zeam/content"
)

type Checker struct {
	mu sync.RWMutex

	config   *Config
	registry *RegistryClient
	content  *content.ContentProtocol

	status UpdateStatus

	OnUpdateAvailable func(manifest *Manifest)
	OnUpdateReady     func(binaryPath string)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewChecker(cfg *Config, contentProtocol *content.ContentProtocol) (*Checker, error) {
	registry, err := NewRegistryClient(cfg.RPCURL, cfg.RegistryAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Checker{
		config:   cfg,
		registry: registry,
		content:  contentProtocol,
		status: UpdateStatus{
			CurrentVersion: cfg.CurrentVersion,
			State:          StateIdle,
		},
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (c *Checker) Start() {
	c.wg.Add(1)
	go c.checkLoop()
	fmt.Println("[Update] Checker started")
}

func (c *Checker) checkLoop() {
	defer c.wg.Done()

	c.CheckNow()

	ticker := time.NewTicker(c.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.CheckNow()
		}
	}
}

func (c *Checker) CheckNow() {
	c.mu.Lock()
	c.status.State = StateChecking
	c.status.LastChecked = time.Now()
	c.mu.Unlock()

	manifestHash, err := c.registry.GetManifestHash(c.ctx)
	if err != nil {
		c.setError(fmt.Errorf("failed to get manifest hash: %w", err))
		return
	}

	if manifestHash == [32]byte{} {
		c.mu.Lock()
		c.status.State = StateIdle
		c.status.UpdateAvailable = false
		c.mu.Unlock()
		return
	}

	c.mu.RLock()
	currentHash := c.status.ManifestHash
	c.mu.RUnlock()

	if manifestHash == currentHash {
		c.mu.Lock()
		c.status.State = StateIdle
		c.mu.Unlock()
		return
	}

	manifest, err := c.fetchManifest(manifestHash)
	if err != nil {
		c.setError(fmt.Errorf("failed to fetch manifest: %w", err))
		return
	}

	if !c.isNewerVersion(manifest.Version) {
		c.mu.Lock()
		c.status.State = StateIdle
		c.status.ManifestHash = manifestHash
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	c.status.ManifestHash = manifestHash
	c.status.LatestVersion = manifest.Version
	c.status.UpdateAvailable = true
	c.status.State = StateIdle
	c.mu.Unlock()

	fmt.Printf("[Update] New version available: %s (current: %s)\n",
		manifest.Version, c.config.CurrentVersion)

	if c.OnUpdateAvailable != nil {
		c.OnUpdateAvailable(manifest)
	}

	if c.config.AutoUpdate {
		go c.DownloadUpdate(manifest)
	}
}

func (c *Checker) fetchManifest(hash [32]byte) (*Manifest, error) {
	hashHex := hex.EncodeToString(hash[:])

	if c.content != nil {
		data, err := c.fetchFromContent(hash)
		if err == nil {
			var manifest Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("failed to parse manifest: %w", err)
			}
			return &manifest, nil
		}

	}

	gateways := []string{
		"https://ipfs.io/ipfs/",
		"https://cloudflare-ipfs.com/ipfs/",
		"https://gateway.pinata.cloud/ipfs/",
	}

	var lastErr error
	for _, gateway := range gateways {
		url := gateway + hashHex
		data, err := c.httpFetch(url)
		if err != nil {
			lastErr = err
			continue
		}

		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			lastErr = fmt.Errorf("failed to parse manifest: %w", err)
			continue
		}

		return &manifest, nil
	}

	return nil, fmt.Errorf("failed to fetch manifest from any gateway: %w", lastErr)
}

func (c *Checker) fetchFromContent(hash [32]byte) ([]byte, error) {
	if c.content == nil {
		return nil, fmt.Errorf("no content protocol")
	}

	var contentID content.ContentID
	copy(contentID[:], hash[:])

	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	return c.content.FetchContent(ctx, contentID)
}

func (c *Checker) httpFetch(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *Checker) isNewerVersion(version string) bool {

	current := parseVersion(c.config.CurrentVersion)
	new := parseVersion(version)

	if new[0] > current[0] {
		return true
	}
	if new[0] == current[0] && new[1] > current[1] {
		return true
	}
	if new[0] == current[0] && new[1] == current[1] && new[2] > current[2] {
		return true
	}
	return false
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	var result [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		fmt.Sscanf(parts[i], "%d", &result[i])
	}
	return result
}

func (c *Checker) DownloadUpdate(manifest *Manifest) error {
	platform := Platform()
	binary, ok := manifest.Binaries[platform]
	if !ok {
		return fmt.Errorf("no binary available for platform %s", platform)
	}

	c.mu.Lock()
	c.status.State = StateDownloading
	c.status.DownloadProgress = 0
	c.mu.Unlock()

	downloadDir := filepath.Join(c.config.DataDir, "updates", manifest.Version)
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		c.setError(fmt.Errorf("failed to create download dir: %w", err))
		return err
	}

	binaryPath := filepath.Join(downloadDir, "zeam")
	if platformOS == "windows" {
		binaryPath += ".exe"
	}

	var data []byte
	var err error

	if strings.HasPrefix(binary.URL, "content://") {

		hashStr := strings.TrimPrefix(binary.URL, "content://")
		var hash [32]byte
		if decoded, err := hex.DecodeString(hashStr); err == nil && len(decoded) == 32 {
			copy(hash[:], decoded)
			data, err = c.fetchFromContent(hash)
		}
	} else {

		data, err = c.downloadWithProgress(binary.URL, binary.Size)
	}

	if err != nil {
		c.setError(fmt.Errorf("failed to download binary: %w", err))
		return err
	}

	c.mu.Lock()
	c.status.State = StateVerifying
	c.mu.Unlock()

	actualHash := sha256.Sum256(data)
	expectedHash, err := hex.DecodeString(binary.Hash)
	if err != nil {
		c.setError(fmt.Errorf("invalid hash in manifest: %w", err))
		return err
	}

	if len(expectedHash) != 32 {
		c.setError(fmt.Errorf("invalid hash length in manifest"))
		return fmt.Errorf("invalid hash length")
	}

	var expected [32]byte
	copy(expected[:], expectedHash)

	if actualHash != expected {
		c.setError(fmt.Errorf("hash mismatch: expected %s, got %s",
			binary.Hash, hex.EncodeToString(actualHash[:])))
		return fmt.Errorf("hash mismatch")
	}

	if err := os.WriteFile(binaryPath, data, 0755); err != nil {
		c.setError(fmt.Errorf("failed to write binary: %w", err))
		return err
	}

	c.mu.Lock()
	c.status.State = StateReady
	c.mu.Unlock()

	fmt.Printf("[Update] Update ready: %s\n", binaryPath)

	if c.OnUpdateReady != nil {
		c.OnUpdateReady(binaryPath)
	}

	return nil
}

func (c *Checker) downloadWithProgress(url string, expectedSize uint64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data := make([]byte, 0, expectedSize)
	buf := make([]byte, 64*1024)
	downloaded := uint64(0)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
			downloaded += uint64(n)

			if expectedSize > 0 {
				c.mu.Lock()
				c.status.DownloadProgress = float64(downloaded) / float64(expectedSize)
				c.mu.Unlock()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	return data, nil
}

func (c *Checker) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.State = StateFailed
	c.status.LastError = err
	fmt.Printf("[Update] Error: %v\n", err)
}

func (c *Checker) Status() UpdateStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Checker) Stop() {
	c.cancel()
	c.wg.Wait()
	if c.registry != nil {
		c.registry.Close()
	}
	fmt.Println("[Update] Checker stopped")
}
