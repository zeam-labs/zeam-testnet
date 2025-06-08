package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

var runtime *Cortex

func main() {
	fmt.Println("Starting ZEAM...")

	if isGenesisNeeded() {
		fmt.Println("No civicL1 chain found. Running genesis...")
		genesis()
	} else {
		fmt.Println("Genesis already complete. Skipping.")
	}

	Chains["civicL1"] = LoadChain("civicL1")
	Chains["cognitionL1"] = LoadChain("cognitionL1")
	Chains["civicL4"] = LoadChain("civicL4")
	Chains["cognitionL4"] = LoadChain("cognitionL4")
	Chains["civicL5"] = LoadChain("civicL5")
	Chains["cognitionL5"] = LoadChain("cognitionL5")
	Chains["civicL6"] = LoadChain("civicL6")
	Chains["cognitionL6"] = LoadChain("cognitionL6")

	shardMap := LoadShardMap(Chains["civicL1"])
	clientID := GenerateClientFingerprint()

	_ = AssignShardsToClient(
		shardMap,
		clientID,
		3,
		Chains["civicL4"],
		Chains["cognitionL4"],
	)

	runtime = StartCortex(
		Chains["civicL1"], Chains["cognitionL1"],
		Chains["civicL4"], Chains["cognitionL4"],
		Chains["civicL5"], Chains["cognitionL5"],
		Chains["civicL6"], Chains["cognitionL6"],
		shardMap,
	)

	StartCivicStorageLoop(shardMap, clientID)
	StartCivicComputeLoop(Chains["civicL4"], Chains["cognitionL4"])

	http.HandleFunc("/input", handleInput)
	http.HandleFunc("/presence", handlePresenceSpawn)
	http.HandleFunc("/output", handleOutput)
	http.HandleFunc("/compute", handleCivicCompute)
	http.HandleFunc("/storage", handleCivicStorage)

	fmt.Println("ZEAM ignition complete.")
	http.ListenAndServe("0.0.0.0:8080", nil)
}

func handleInput(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var input Input
	_ = json.Unmarshal(body, &input)

	runtime.Interpret(input)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func handlePresenceSpawn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var req spawnRequest
	_ = json.Unmarshal(body, &req)

	id := AttestBiometric([]byte(req.ID))
	if id != "" {
		SpawnPresence(r.Context(), id, "Presence-"+id[:6])
		json.NewEncoder(w).Encode(map[string]string{
			"id":   id,
			"name": "Presence-" + id[:6],
		})
		return
	}

	SpawnPresence(r.Context(), req.ID, req.Name)
	json.NewEncoder(w).Encode(map[string]string{
		"id":   req.ID,
		"name": req.Name,
	})
}

func handleOutput(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if runtime == nil || runtime.Output == nil {
		json.NewEncoder(w).Encode([]string{})
		return
	}

	json.NewEncoder(w).Encode(runtime.Output)
}

func handleCivicCompute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleCivicStorage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func AttestBiometric(input []byte) string {
	hash := string(input)

	for _, entry := range Chains["civicL1"].Entries {
		if entry.Type == "spawn" && strings.Contains(entry.Content, hash) {
			parts := strings.Split(entry.Content, "|")
			for _, p := range parts {
				if strings.Contains(p, "ID:") {
					id := strings.TrimSpace(strings.Split(p, ":")[1])
					return id
				}
			}
		}
	}
	return ""
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
