package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

type Applier struct {
	config *Config
}

func NewApplier(cfg *Config) *Applier {
	return &Applier{config: cfg}
}

func (a *Applier) Apply(newBinaryPath string) error {
	currentBinary := a.config.BinaryPath
	if currentBinary == "" {
		var err error
		currentBinary, err = os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get current executable: %w", err)
		}
	}

	currentBinary, err := filepath.EvalSymlinks(currentBinary)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	info, err := os.Stat(newBinaryPath)
	if err != nil {
		return fmt.Errorf("new binary not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("new binary path is a directory")
	}

	if runtime.GOOS == "windows" {
		return a.applyWindows(currentBinary, newBinaryPath)
	}
	return a.applyUnix(currentBinary, newBinaryPath)
}

func (a *Applier) applyUnix(currentBinary, newBinaryPath string) error {

	backupPath := currentBinary + ".backup"
	if err := os.Rename(currentBinary, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	if err := os.Rename(newBinaryPath, currentBinary); err != nil {

		os.Rename(backupPath, currentBinary)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	if err := os.Chmod(currentBinary, 0755); err != nil {

		os.Rename(currentBinary, newBinaryPath)
		os.Rename(backupPath, currentBinary)
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	fmt.Println("[Update] Binary replaced, restarting...")

	return syscall.Exec(currentBinary, os.Args, os.Environ())
}

func (a *Applier) applyWindows(currentBinary, newBinaryPath string) error {

	oldPath := currentBinary + ".old"

	os.Remove(oldPath)

	if err := os.Rename(currentBinary, oldPath); err != nil {
		return fmt.Errorf("failed to rename current binary: %w", err)
	}

	newData, err := os.ReadFile(newBinaryPath)
	if err != nil {

		os.Rename(oldPath, currentBinary)
		return fmt.Errorf("failed to read new binary: %w", err)
	}

	if err := os.WriteFile(currentBinary, newData, 0755); err != nil {

		os.Rename(oldPath, currentBinary)
		return fmt.Errorf("failed to write new binary: %w", err)
	}

	fmt.Println("[Update] Binary replaced, starting new process...")

	cmd := exec.Command(currentBinary, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new process: %w", err)
	}

	os.Exit(0)
	return nil
}

func (a *Applier) Rollback() error {
	currentBinary := a.config.BinaryPath
	if currentBinary == "" {
		var err error
		currentBinary, err = os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get current executable: %w", err)
		}
	}

	currentBinary, err := filepath.EvalSymlinks(currentBinary)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	backupPath := currentBinary + ".backup"
	if runtime.GOOS == "windows" {
		backupPath = currentBinary + ".old"
	}

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("no backup found at %s", backupPath)
	}

	tempPath := currentBinary + ".temp"
	if err := os.Rename(currentBinary, tempPath); err != nil {
		return fmt.Errorf("failed to move current binary: %w", err)
	}

	if err := os.Rename(backupPath, currentBinary); err != nil {
		os.Rename(tempPath, currentBinary)
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	os.Remove(tempPath)

	fmt.Println("[Update] Rollback complete")
	return nil
}

func (a *Applier) CleanupOldVersions(keepVersions int) error {
	updatesDir := filepath.Join(a.config.DataDir, "updates")

	entries, err := os.ReadDir(updatesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type versionDir struct {
		name    string
		modTime int64
	}
	var versions []versionDir
	for _, entry := range entries {
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			versions = append(versions, versionDir{
				name:    entry.Name(),
				modTime: info.ModTime().Unix(),
			})
		}
	}

	for i := 0; i < len(versions)-1; i++ {
		for j := i + 1; j < len(versions); j++ {
			if versions[j].modTime > versions[i].modTime {
				versions[i], versions[j] = versions[j], versions[i]
			}
		}
	}

	for i := keepVersions; i < len(versions); i++ {
		path := filepath.Join(updatesDir, versions[i].name)
		if err := os.RemoveAll(path); err != nil {
			fmt.Printf("[Update] Failed to remove old version %s: %v\n", versions[i].name, err)
		} else {
			fmt.Printf("[Update] Cleaned up old version: %s\n", versions[i].name)
		}
	}

	return nil
}
