package ipfs

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"time"
)

func AssignAndPin(allHashes []string, percent float64) []string {
	total := len(allHashes)
	if total == 0 || percent <= 0 {
		return nil
	}

	target := int(float64(total) * (percent / 100))
	if target < 1 {
		target = 1
	}

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(allHashes), func(i, j int) {
		allHashes[i], allHashes[j] = allHashes[j], allHashes[i]
	})

	selected := allHashes[:target]
	for _, hash := range selected {
		_ = PinShard(hash)
	}

	return selected
}

func PrintHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
