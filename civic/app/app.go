package civic
import (
    "os"

    abci "github.com/tendermint/tendermint/abci/types"
    sdk  "github.com/cosmos/cosmos-sdk/types"
    "github.com/spf13/cobra"

    vm       "zeam/x/civic/vm"
    entrycli "zeam/x/civic/entry/client/cli"
)

type App struct {
    LLM *vm.Keeper
}

func NewApp() *App {
    // instantiate your on-chain LLM keeper
    return &App{LLM: vm.NewKeeper()}
}

func (app *App) EndBlocker(ctx sdk.Context, _ abci.RequestEndBlock) []abci.ValidatorUpdate {
    app.LLM.Tick(ctx)  // one call, every block
    return nil
}

func main() {
    // root CLI command
    rootCmd := &cobra.Command{
        Use:   "civicd",
        Short: "Civic daemon CLI",
    }

    // initialize the app
    app := NewApp()

    // wire in the entry module's tx commands
    rootCmd.AddCommand(entrycli.NewTxCmd())

    // execute
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
