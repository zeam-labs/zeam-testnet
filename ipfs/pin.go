package ipfs

import (
	"fmt"
	"os"

	shell "github.com/ipfs/go-ipfs-api"
)

var sh *shell.Shell

func InitIPFS(host string) error {
	sh = shell.NewShell(host)
	if !sh.IsUp() {
		return fmt.Errorf("IPFS daemon not running at %s", host)
	}
	return nil
}

func PinFile(path string) (string, error) {
	if sh == nil {
		return "", fmt.Errorf("IPFS not initialized")
	}

	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("could not open file: %w", err)
	}
	defer file.Close()

	cid, err := sh.Add(file)
	if err != nil {
		return "", fmt.Errorf("failed to add file to IPFS: %w", err)
	}

	err = sh.Pin(cid)
	if err != nil {
		return "", fmt.Errorf("failed to pin CID: %w", err)
	}

	return cid, nil
}
