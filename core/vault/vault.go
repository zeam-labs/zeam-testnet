package vault

import (
    "fmt"
    sdk "github.com/cosmos/cosmos-sdk/types"
)

func CreateVault(ctx sdk.Context, id string) error {
    fmt.Printf("[VAULT] Created vault for: %s\n", id)
    return nil
}
