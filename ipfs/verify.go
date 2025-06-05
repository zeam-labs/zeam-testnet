package ipfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// VerifyShard checks if the SHA256 hash of the data matches the expected hex string.
// This is used to confirm shard integrity after IPFS retrieval.
func VerifyShard(data []byte, expectedHash string) bool {
	hash := sha256.Sum256(data)
	calculated := hex.EncodeToString(hash[:])
	return calculated == expectedHash
}

// Debug helper for verbose confirmation
func PrintHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
