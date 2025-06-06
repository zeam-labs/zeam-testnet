package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"

	"zeam/app"
	"zeam/ipfs"
	"zeam/core/spawn/presence_spawner"
)

var runtime *app.Cortex
var Output []string
var Chains = map[string]*Chain{}

type Input struct {
    Source    string    `json:"source"`
    Type      string    `json:"type"`
    Content   string    `json:"content"`
    Timestamp time.Time `json:"timestamp"`
}

type spawnRequest struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	PresenceID string `json:"presence_id,omitempty"`

}

func MountMemory() {
	Chains["civicL1"] = LoadChain("civicL1")
	Chains["cognitionL1"] = LoadChain("cognitionL1")
	Chains["civicL4"] = LoadChain("civicL4")
	Chains["cognitionL4"] = LoadChain("cognitionL4")
	Chains["civicL5"] = LoadChain("civicL5")
	Chains["cognitionL5"] = LoadChain("cognitionL5")
	Chains["civicL6"] = LoadChain("civicL6")
	Chains["cognitionL6"] = LoadChain("cognitionL6")

	ipfs.InitIPFS("localhost:5001")

	shardMap := GetShardMap(Chains["civicL1"])
	clientID := GenerateClientFingerprint()

	assignedShards := AssignShardsToClient(
		shardMap,
		clientID,
		3,
		Chains["civicL4"],
		Chains["cognitionL4"],
	)

	ServeShards(assignedShards)

	runtime = app.StartCortex(
		Chains["civicL1"],
		Chains["cognitionL1"],
		Chains["civicL4"],
		Chains["cognitionL4"],
		Chains["civicL5"],
		Chains["cognitionL5"],
		Chains["civicL6"],
		Chains["cognitionL6"],
	)

	http.HandleFunc("/input", handleInput)
	http.HandleFunc("/output", handleOutput) 
	http.HandleFunc("/presence", handlePresenceSpawn)

	http.ListenAndServe(":8080", nil)
}

func GetShardMap(c *Chain) map[string]string {
	shardMap := make(map[string]string)

	for _, entry := range c.Log {
		if entry.Type == "shard_index" && entry.Source == "ignite" {
			json.Unmarshal([]byte(entry.Content), &shardMap)
			break
		}
	}

	return shardMap
}

func AssignShardsToClient(
	shardMap map[string]string,
	clientID string,
	replicationFactor int,
	civicL4, cognitionL4 *Chain,
	) []string {
	assigned := []string{}
	var shardNames []string

	for name := range shardMap {
		shardNames = append(shardNames, name)
	}
	sort.Strings(shardNames)

	clientHash := sha256.Sum256([]byte(clientID))
	clientHashHex := hex.EncodeToString(clientHash[:])

	for _, name := range shardNames {
		hash := sha256.Sum256([]byte(clientHashHex + name))
		sum := int(hash[0])

		if sum%replicationFactor == 0 {
			cid := shardMap[name]
			assigned = append(assigned, cid)

			entry := Input{
				Source:    "civic.storage",
				Type:      "shard_offer",
				Content:   "shard_offer:" + cid,
				Timestamp: time.Now().UTC(),
			}

			ctx := context.Background()
			civicL4.Mint(ctx, entry)
			cognitionL4.Mint(ctx, entry)
		}
	}

	return assigned
}

func handleInput(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var input Input
	err := json.Unmarshal(body, &input)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	msg := runtime.Interpret(input)
	if msg != "" {
		Output = append(Output, msg)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func handleOutput(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Output)
	Output = []string{} 
}

func handlePresenceSpawn(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var req spawnRequest
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	presence_spawner.SpawnPresence(r.Context(), presence_spawner.PresenceParams{
		PresenceID:   req.ID,
		AssignedName: req.Name,
	})

	w.WriteHeader(http.StatusOK)
}
