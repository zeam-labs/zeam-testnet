package main

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
)

func StartCivicComputeLoop(cog *Cognition) {
	go func() {
		Log("🧠 Civic Compute loop started (silent mode)")

		for {
			time.Sleep(10 * time.Second)

			// Check for recent entries in L4 chains
			recent := false
			now := time.Now()

			for _, chain := range []*Chain{
				cog.Civic.L1, cog.Civic.L4, cog.Civic.L5, cog.Civic.L6,
				cog.Cog.L1, cog.Cog.L4, cog.Cog.L5, cog.Cog.L6,
			} {
				if chain == nil || len(chain.Entries) == 0 {
					continue
				}
				latest := chain.Entries[len(chain.Entries)-1].Timestamp
				if now.Sub(latest) < 5*time.Second {
					recent = true
					break
				}
			}

			if !recent {
				continue // nothing new, stay silent
			}

			if !ShouldRunCompute() {
				Log("🛑 Skipping compute — CPU too high")
				continue
			}

			Log("⚙️ Triggered Civic Compute due to recent mesh activity")
			RunAllChains(cog)
		}
	}()
}

func ShouldRunCompute() bool {
	usage, err := cpu.Percent(0, false)
	if err != nil || len(usage) == 0 {
		Log("⚠️  Could not read CPU stats. Defaulting to OFF.")
		return false
	}
	current := usage[0]
	Log("🧠 CPU usage: %.2f%%\n", current)
	return current < 15.0
}

func EmitComputeOffer(cog *Cognition) {
	usage, err := cpu.Percent(0, false)
	if err != nil || len(usage) == 0 {
		return
	}
	cpuFree := 100.0 - usage[0]

	report := map[string]interface{}{
		"clientID":     cog.ClientID,
		"cpu_free_pct": math.Round(cpuFree*100) / 100,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}

	payload, err := json.Marshal(report)
	if err != nil {
		Log("❌ Failed to marshal compute_offer: %v", err)
		return
	}

	input := Input{
		Type:      "compute_offer",
		Source:    "civic.compute",
		Content:   string(payload),
		Timestamp: time.Now().UTC(),
	}
	Chains["civicL4"].Mint(context.Background(), input)

	Log("📡 Offered Civic Compute — %.2f%% CPU available", cpuFree)
}

func RunAllChains(cog *Cognition) {
	now := time.Now()

	// Civic mesh
	for _, chain := range []*Chain{cog.Civic.L1, cog.Civic.L4, cog.Civic.L5, cog.Civic.L6} {
		if chain == nil || len(chain.Entries) == 0 {
			continue
		}
		latest := chain.Entries[len(chain.Entries)-1].Timestamp
		if now.Sub(latest) > 5*time.Second {
			continue
		}
		RunCortexPass(cog, chain.Name, chain, chain)
	}

	// Cognition mesh
	for _, chain := range []*Chain{cog.Cog.L1, cog.Cog.L4, cog.Cog.L5, cog.Cog.L6} {
		if chain == nil || len(chain.Entries) == 0 {
			continue
		}
		latest := chain.Entries[len(chain.Entries)-1].Timestamp
		if now.Sub(latest) > 5*time.Second {
			continue
		}
		RunCortexPass(cog, chain.Name, chain, chain)
	}

	// Presences
	for id, p := range ActivePresences {
		if len(p.L2.Entries) == 0 {
			continue
		}
		latest := p.L2.Entries[len(p.L2.Entries)-1].Timestamp
		if now.Sub(latest) > 5*time.Second {
			continue
		}
		RunCortexPass(cog, id, p.L2, p.L3)
	}

	// Agents
	for id, a := range ActiveAgents {
		if len(a.L2.Entries) == 0 {
			continue
		}
		latest := a.L2.Entries[len(a.L2.Entries)-1].Timestamp
		if now.Sub(latest) > 5*time.Second {
			continue
		}
		RunCortexPass(cog, id, a.L2, a.L3)
	}
}

type ComputeRequest struct {
	TargetChain string `json:"target_chain"`
	EntryRange  int    `json:"entry_range"` // optional for batching
}

func HandleMeshComputeRequest(req ComputeRequest) {
	chain := Chains[req.TargetChain]
	if chain == nil {
		Log("❌ Unknown chain: %s", req.TargetChain)
		return
	}

	Log("🧠 Running remote ComputePass on chain: %s", req.TargetChain)
	RunCortexPass(cognition, req.TargetChain, chain, chain)
}
