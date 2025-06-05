package ipfs

import (
	"bytes"
	"fmt"
	"io"

	shell "github.com/ipfs/go-ipfs-api"
)

func FetchShard(cid string) ([]byte, error) {
	if sh == nil {
		return nil, fmt.Errorf("IPFS not initialized. Call InitIPFS first.")
	}

	var buf bytes.Buffer
	err := sh.Cat(cid, &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shard for CID %s: %w", cid, err)
	}

	return buf.Bytes(), nil
}

func FetchShardToWriter(cid string, writer io.Writer) error {
	if sh == nil {
		return fmt.Errorf("IPFS not initialized. Call InitIPFS first.")
	}

	err := sh.Cat(cid, writer)
	if err != nil {
		return fmt.Errorf("failed to fetch shard to writer: %w", err)
	}
	return nil
}
