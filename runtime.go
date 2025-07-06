package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
)

var ActiveAgents = map[string]*Agent{}
var ActivePresences = map[string]*Presence{}
var Chains = map[string]*Chain{}
var Vaults = map[string]float64{}

func generateClientID() string {
	host, _ := os.Hostname()
	raw := "zeam-" + host
	hash := sha256.Sum256([]byte(raw))
	id := fmt.Sprintf("client-%x", hash)
	Log("🔑 Generated client ID: %s", id) // optional
	return id
}

func NewChain(name string) *Chain {
	return &Chain{Name: name, Entries: []Input{}}
}

func (c *Chain) Mint(ctx context.Context, input Input) {
	c.Entries = append(c.Entries, input)
	Log("📝 Minted to %s by %s: %s", c.Name, input.Source, input.Content)
}

var lastMeshState struct {
	Used  uint64
	Max   uint64
	CPU   float64
	Stamp time.Time
}

func CaptureMeshHealth(cog *Cognition) MeshHealthSnapshot {
	now := time.Now().UTC()

	// 🧠 CPU usage
	usage, _ := cpu.Percent(0, false)
	cpuStr := "N/A"
	cpuFree := 0.0
	if len(usage) > 0 {
		cpuStr = fmt.Sprintf("%.1f%%", usage[0])
		cpuFree = 100.0 - usage[0]
	}

	var totalUsed, totalMax uint64
	var flows []string
	var localChanged bool

	for clientID, node := range StorageState {
		if node == nil {
			continue
		}

		if contains(cog.ClientID, clientID) {
			if node.Used != lastMeshState.Used ||
				node.Max != lastMeshState.Max ||
				math.Abs(usage[0]-lastMeshState.CPU) > 5.0 {
				localChanged = true
				lastMeshState.Used = node.Used
				lastMeshState.Max = node.Max
				lastMeshState.CPU = usage[0]
				lastMeshState.Stamp = now
			}
		}

		for cid, kind := range node.Assigned {
			if kind == "shard" {
				flows = append(flows, fmt.Sprintf("%s → %s", cid[:8], node.ClientID))
			}
		}

		totalUsed += node.Used
		totalMax += node.Max
	}

	// 📝 Only mint if local node changed
	if localChanged {
		for _, id := range cog.ClientID {
			if node := StorageState[id]; node != nil {
				utilization := float64(node.Used) / float64(node.Max) * 100.0
				assignedCount := len(node.Assigned)

				report := map[string]interface{}{
					"client_id":       id,
					"used_bytes":      node.Used,
					"max_bytes":       node.Max,
					"utilization_pct": math.Round(utilization*100) / 100,
					"assigned_count":  assignedCount,
					"cpu_usage_pct":   math.Round(usage[0]*100) / 100,
					"cpu_free_pct":    math.Round(cpuFree*100) / 100,
					"last_seen":       now.Format(time.RFC3339),
				}

				payload, _ := json.Marshal(report)
				input := Input{
					Source:    "mesh.health",
					Type:      "mesh_health",
					Content:   string(payload),
					Timestamp: now,
				}

				Chains["civicL4"].Mint(context.Background(), input)

				Log("📊 Captured mesh health for %s — %d assigned, %.2f%% full",
					id, assignedCount, utilization)
			}
		}
	} // <-- this closing brace was missing

	storageStr := "N/A"
	if totalMax > 0 {
		storageStr = fmt.Sprintf("%.2fGB / %.2fGB", float64(totalUsed)/1e9, float64(totalMax)/1e9)
	}

	// Construct mesh display summary
	health := MeshHealthSnapshot{
		CPU:       cpuStr,
		Storage:   storageStr,
		NodeCount: len(StorageState),
		Flow:      flows,
	}

	// Push to UI (non-blocking)
	select {
	case logChannel <- health:
	default:
	}

	return health
}

func StartCivicMaintenanceLoop(cog *Cognition, clientIDs []string) {
	var lastCPU float64

	// 🧠 CPU-based health trigger
	go func() {
		for {
			time.Sleep(15 * time.Second)
			usage, _ := cpu.Percent(0, false)
			if len(usage) == 0 {
				continue
			}
			if math.Abs(usage[0]-lastCPU) > 5.0 {
				lastCPU = usage[0]
				CaptureMeshHealth(cog)
			}
		}
	}()

	// 🧼 Hourly storage repin + maintenance
	go func() {
		Log("🧭 Civic maintenance loop started (1h interval)")

		for {
			time.Sleep(1 * time.Hour)

			Log("🧼 Running scheduled mesh maintenance...")

			activeClients := []string{}
			for id := range StorageState {
				activeClients = append(activeClients, id)
			}

			StartCivicStorageRepinnerLoop(
				cog.ShardIndex,
				cog.WasmIndex,
				activeClients,
			)
		}
	}()

	// 🧪 Immediate health capture at startup
	CaptureMeshHealth(cog)

	// 🔄 Light resync with 1 peer
	peers := DiscoverDNSBootstrapPeers("public.zeam.foundation")
	if len(peers) > 1 {
		for _, peer := range peers {
			host := strings.Split(peer, ":")[0]
			if host != getSelfIP() {
				for _, id := range clientIDs {
					go StartGossipSyncClient(peer, id)
				}
				break
			}
		}
	}
}

func contains(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}
