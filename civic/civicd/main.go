package main

import (
    "os"

    "github.com/cosmos/cosmos-sdk/server"
    "github.com/cosmos/cosmos-sdk/server/config"
    "github.com/spf13/cobra"
    sdk "github.com/cosmos/cosmos-sdk/types"
    "github.com/spf13/viper"

    civicapp "zeam/app/civic"
)

func main() {
    // Ensure SDK config is loaded
    config := sdk.GetConfig()
    config.SetBech32PrefixForAccount("zeam", "zeampub")
    // ... any other SDK config tweaks ...

    rootCmd := &cobra.Command{
        Use:   "civicd",
        Short: "Civic mesh daemon",
        PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
            // set up server context
            return server.PersistentPreRunEFn(cmd)
        },
    }

    // Add the standard server commands: init, start, keys, etc.
    server.AddCommands(
        rootCmd,
        civicapp.NewCivicApp,
        civicapp.DefaultNodeHome, // e.g. ~/.civicd
        server.MakeEncodingConfig(civicapp.ModuleBasics),
    )

    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
