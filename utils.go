package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"encoding/hex"
	"encoding/json"
	"context"
)

var CORE_CONTEXT string
var TRAIT_MANIFEST string
var ActiveAgents   = map[string]*Agent{}
var ActivePresences = map[string]*Presence{}
var Chains = map[string]*Chain{}
var Vaults = map[string]float64{}


func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hashString(s string) string {
	return hashBytes([]byte(s))
}

func hashContent(content string) string {
	return hashString(content) 
}

func getNowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func writeTemp(data []byte) (string, error) {
	tmp, err := os.CreateTemp("", "zeam-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := tmp.Write(data); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

func cleanupTemp(path string) {
	_ = os.Remove(path)
}

func pinToIPFS(data []byte) (string, error) {
	tmp, err := os.CreateTemp("", "ipfs-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		return "", err
	}
	tmp.Close()

	out, err := exec.Command("ipfs", "add", "-Q", tmp.Name()).Output()
	if err != nil {
		return "", fmt.Errorf("ipfs pin error: %v", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func readFromIPFS(cid string) ([]byte, error) {
	out, err := exec.Command("ipfs", "cat", cid).Output()
	if err != nil {
		return nil, fmt.Errorf("ipfs read error: %v", err)
	}
	return out, nil
}

func LoadShardMap(c *Chain) map[string]string {
	shardMap := make(map[string]string)

	for _, entry := range c.Entries {
		if entry.Type == "shard_index" && entry.Source == "ignite" {
			json.Unmarshal([]byte(entry.Content), &shardMap)
			break
		}
	}

	return shardMap

}

func NewChain(name string) *Chain {
	return &Chain{
		Name:    name,
		Entries: []Input{},
	}
}

func LoadChain(name string) *Chain {
    return &Chain{
        Name:    name,
        Entries: []Input{},
    }
}

func GenerateClientFingerprint() string {
	now := time.Now().UnixNano()
	return fmt.Sprintf("client-%x", sha256.Sum256([]byte(fmt.Sprint(now))))
}

func StartCivicStorageLoop(shardMap map[string]string, clientID string) {
	go func() {
		for {
			assigned := AssignShardsToClient(
				shardMap,
				clientID,
				3,
				Chains["civicL4"],
				Chains["cognitionL4"],
			)

			for _, cid := range assigned {
				cmd := exec.Command("ipfs", "pin", "add", cid)
				cmd.Run()
			}

			time.Sleep(5 * time.Minute) // Recheck occasionally
		}
	}()
}

func StartCivicComputeLoop(civicL4, cognitionL4 *Chain) {
	go func() {
		for {
			tasks := FetchCivicTasks(civicL4, cognitionL4)

			for _, t := range tasks {
				switch t.Type {
				case "agent":
					if a, ok := ActiveAgents[t.ID]; ok {
						RunZARPass(a)
					}
				case "presence":
					if p, ok := ActivePresences[t.ID]; ok {
						RunPresenceTraitPass(p)
					}
				}
			}

			time.Sleep(30 * time.Second) // Throttle for 2% CPU profile
		}
	}()
}

func isGenesisNeeded() bool {
	_, err := os.Stat("./civicL1.json")
	return os.IsNotExist(err)
}

func (c *Chain) Mint(ctx context.Context, input Input) {
	c.Entries = append(c.Entries, input)
}
