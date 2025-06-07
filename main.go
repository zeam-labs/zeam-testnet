package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"time"
	"strings"
)

var runtime *Cortex

func main() {
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

	http.ListenAndServe(":8080", nil)
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

	runtime.Interpret(input)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func handlePresenceSpawn(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var req spawnRequest
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err := SpawnPresence(r.Context(), req.ID, req.Name)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func FetchCivicTasks(l4s ...*Chain) []CivicTask {
	var tasks []CivicTask

	for _, l4 := range l4s {
		for _, entry := range l4.Entries {
			if entry.Type == "civic.task" && strings.HasPrefix(entry.Content, "run:") {
				parts := strings.Split(entry.Content, ":")
				if len(parts) == 3 {
					tasks = append(tasks, CivicTask{
						Type: parts[1],
						ID:   parts[2],
					})
				}
			}
		}
	}

	return tasks
}

func StartCivicComputeWorker(civicL4, cognitionL4 *Chain) {
	go func() {
		for {
			tasks := FetchCivicTasks(civicL4, cognitionL4)
			for _, t := range tasks {
				switch t.Type {
				case "agent":
					if agent, ok := ActiveAgents[t.ID]; ok {
						RunZARPass(agent)
					}
				case "presence":
					if p, ok := ActivePresences[t.ID]; ok {
						RunPresenceTraitPass(p)
					}
				}
			}
			time.Sleep(1 * time.Minute)
		}
	}()
}
