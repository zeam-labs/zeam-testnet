package spawn

import (
    "fmt"
    sdk "github.com/cosmos/cosmos-sdk/types"
    "zeam/core/vault" // Assume vault.CreateVault is implemented
)

// Shared struct for both Agent and Presence spawns
type SpawnParams struct {
    ID       string   // Agent or Presence ID
    Role     string   // Optional: for agents
    Type     string   // "agent" or "presence"
    Anchors  []string // Chains to anchor to
    Mint     func(ctx sdk.Context, module, content string) error
}

// SpawnAgent initializes a new agent with an L2 chain and vault
func SpawnAgent(ctx sdk.Context, params SpawnParams) error {
    chainID := fmt.Sprintf("cog-L2-%s", params.ID)

    // 1. Create vault
    if err := vault.CreateVault(ctx, params.ID); err != nil {
        return fmt.Errorf("vault creation failed: %w", err)
    }

    // 2. Anchor chain (placeholder)
    fmt.Printf("[ANCHOR] %s anchored to: %v\n", chainID, params.Anchors)

    // 3. Mint spawn record
    if params.Mint != nil {
        return params.Mint(ctx, "spawn", fmt.Sprintf("Spawned agent %s (role: %s) at %s", params.ID, params.Role, chainID))
    }

    return nil
}

// SpawnPresence initializes a new presence with a vault and L2 memory
func SpawnPresence(ctx sdk.Context, params SpawnParams) error {
    chainID := fmt.Sprintf("civic-L2-%s", params.ID)

    if err := vault.CreateVault(ctx, params.ID); err != nil {
        return fmt.Errorf("vault creation failed: %w", err)
    }

    fmt.Printf("[ANCHOR] %s anchored to: %v\n", chainID, params.Anchors)

    if params.Mint != nil {
        return params.Mint(ctx, "spawn", fmt.Sprintf("Spawned presence %s at %s", params.ID, chainID))
    }

    return nil
}
