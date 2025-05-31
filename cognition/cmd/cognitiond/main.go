package main

import (
    "fmt"
    "os"

    "github.com/spf13/cobra"
    sdk "github.com/cosmos/cosmos-sdk/types"

    "zeam/cognition/app"
)

func main() {
    a := app.NewApp()

    rootCmd := &cobra.Command{
        Use:   "cognitiond",
        Short: "ZEAM Cognition Mesh CLI",
    }

    // Register civic action commands
    rootCmd.AddCommand(newActionCmd(a, "reflect"))
    rootCmd.AddCommand(newActionCmd(a, "store"))
    rootCmd.AddCommand(newActionCmd(a, "affirm"))
    rootCmd.AddCommand(newActionCmd(a, "anchor"))
    rootCmd.AddCommand(newActionCmd(a, "observe"))
    rootCmd.AddCommand(newActionCmd(a, "offer"))

    // Register spawn-agent
    rootCmd.AddCommand(newSpawnAgentCmd(a))

    // ✅ Register tick command (must be outside the Execute block)
    rootCmd.AddCommand(&cobra.Command{
        Use:   "tick",
        Short: "Manually trigger the cognition loop to evaluate unresolved memory",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := sdk.Context{}
            a.Tick(ctx)
            return nil
        },
    })

    // Run CLI
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}

// Wrap civic actions
func newActionCmd(a *app.App, action string) *cobra.Command {
    return &cobra.Command{
        Use:   fmt.Sprintf("%s [content]", action),
        Short: fmt.Sprintf("Execute %s action", action),
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := sdk.Context{}
            return a.Execute(ctx, action, args[0])
        },
    }
}

// Wrap agent spawning
func newSpawnAgentCmd(a *app.App) *cobra.Command {
    return &cobra.Command{
        Use:   "spawn-agent [id]",
        Short: "Spawn a new agent with an L2 and Vault",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := sdk.Context{}
            return a.Execute(ctx, "spawn-agent", args[0])
        },
    }
}
