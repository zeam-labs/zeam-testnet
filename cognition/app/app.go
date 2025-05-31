package app

import (
    "fmt"
    "strings"

    sdk "github.com/cosmos/cosmos-sdk/types"

    "zeam/cognition/x/reflect"
    "zeam/cognition/x/store"
    "zeam/cognition/x/affirm"
    "zeam/cognition/x/anchor"
    "zeam/cognition/x/observe"
    "zeam/cognition/x/offer"
    "zeam/core/spawn"
)

type App struct {
    traitMemory map[string][]string
}

func NewApp() *App {
    return &App{
        traitMemory: map[string][]string{},
    }
}

// CLI + Presence input → routed here
func (a *App) Execute(ctx sdk.Context, action string, content string) error {
    switch action {
    case "reflect":
        a.LogTraitMemory("Ethics", content)
        if err := reflect.Execute(ctx, content, a.Mint); err != nil {
            return err
        }
        return a.RunCognitionLoop(ctx, "Ethics")

    case "store":
        a.LogTraitMemory("Archival", content)
        return store.Execute(ctx, content, a.Mint)

    case "affirm":
        a.LogTraitMemory("Audit", content)
        return affirm.Execute(ctx, content, a.Mint)

    case "anchor":
        a.LogTraitMemory("Culture", content)
        return anchor.Execute(ctx, content, a.Mint)

    case "observe":
        a.LogTraitMemory("RedTeam", content)
        return observe.Execute(ctx, content, a.Mint)

    case "offer":
        a.LogTraitMemory("Finance", content)
        return offer.Execute(ctx, content, a.Mint)

    case "spawn-agent":
        return spawn.SpawnAgent(ctx, spawn.SpawnParams{
            ID:      content,
            Role:    "Generic",
            Type:    "agent",
            Anchors: []string{"cog-L2", "cog-L1"},
            Mint:    a.Mint,
        })

    case "query":
        a.LogTraitMemory("Culture", content)
        return a.RunCognitionLoop(ctx, "Culture")

    default:
        return fmt.Errorf("unknown action: %s", action)
    }
}

// Passive loop, can be called manually or per block
func (a *App) Tick(ctx sdk.Context) {
    for trait, memory := range a.traitMemory {
        if a.ShouldReflectOnSilence(trait, memory) {
            fmt.Printf("[TENSION] Trait: %s feels unresolved\n", trait)
            output, err := a.RunCognition(trait, memory)
            if err == nil {
                a.Mint(ctx, "llm", output)
            }
        }
    }
}

// Accumulate trait-aligned memory inputs (from actions or queries)
func (a *App) LogTraitMemory(trait string, content string) {
    a.traitMemory[trait] = append(a.traitMemory[trait], content)
    fmt.Printf("[MEMORY] Trait: %s → \"%s\"\n", trait, content)
}

// Interprets memory as semantic resonance → may trigger reflection
func (a *App) RunCognitionLoop(ctx sdk.Context, trait string) error {
    memory := a.traitMemory[trait]
    output, err := a.RunCognition(trait, memory)
    if err != nil {
        return err
    }
    return a.Mint(ctx, "llm", output)
}

// Cognition shell (LLM interpreter shim)
func (a *App) RunCognition(trait string, memory []string) (string, error) {
    response := fmt.Sprintf(
        "Reflection on trait '%s':\n- %s",
        trait,
        strings.Join(memory, "\n- "),
    )
    return response, nil
}

// Heuristic only: triggers silent reflection if memory accumulates
func (a *App) ShouldReflectOnSilence(trait string, memory []string) bool {
    return len(memory) >= 5 // adjust or replace with memory pattern logic
}

// Universal civic memory write
func (a *App) Mint(ctx sdk.Context, module string, content string) error {
    fmt.Printf("[MINT] module: %s | content: %s\n", module, content)
    return nil
}
