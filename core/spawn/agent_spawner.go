// agent_spawner.go
package agent_spawner

import (
	"context"
	"fmt"
	"zeam/civic/app"
)

// AgentParams defines required metadata for a new Agent
type AgentParams struct {
	AgentID       string
	AssignedName  string
	AssignedTo    string // PresenceID
	ShardHash     string
	DeclaredRole  string // e.g., "llm_host", "cat_video_sorter"
	Mint          func(ctx context.Context, layer, content string) error
}

func SpawnAgent(ctx context.Context, params AgentParams) error {
	fmt.Printf("[SPAWN] Initializing Agent: %s (Assigned to: %s)\n", params.AgentID, params.AssignedTo)

	// === Step 1: Initialize L2 chain for Agent ===
	agentL2 := app.NewChain(fmt.Sprintf("agent-%s-L2", params.AgentID))

	// === Step 2: Declare assignment and role
	agentL2.Mint(ctx, fmt.Sprintf("assigned_to:%s", params.AssignedTo))
	agentL2.Mint(ctx, fmt.Sprintf("agent_role:%s", params.DeclaredRole))

	// === Step 3: Accept shard or file custody
	if params.ShardHash != "" {
		agentL2.Mint(ctx, fmt.Sprintf("assigned_shard:%s", params.ShardHash))
		agentL2.Mint(ctx, fmt.Sprintf("shard_accept:%s", params.ShardHash))
	}

	// === Step 4: Declare Traits and initialize private L3s
	traits := []string{"Audit", "Ethics", "Resilience", "Health", "Culture", "RedTeam", "Archival", "Finance"}
	for _, trait := range traits {
		agentL2.Mint(ctx, fmt.Sprintf("trait_declared:%s", trait))

		l3 := app.NewChain(fmt.Sprintf("agent-%s-L3-%s", params.AgentID, trait))
		l3.Mint(ctx, fmt.Sprintf("anchor:root for %s", trait))
	}

	// === Step 5: Anchor Cognition Mesh roots into Agent L2
	agentL2.Mint(ctx, "anchor:cognition-L1->root")
	agentL2.Mint(ctx, "anchor:cognition-L4->root")
	agentL2.Mint(ctx, "anchor:cognition-L5->root")
	agentL2.Mint(ctx, "anchor:cognition-L6->root")

	fmt.Printf("[SPAWN COMPLETE] Agent %s is now live.\n", params.AgentID)
	return nil
}
