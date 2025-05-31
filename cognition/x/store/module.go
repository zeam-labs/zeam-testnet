package store

import (
    "fmt"
    sdk "github.com/cosmos/cosmos-sdk/types"
)

const ModuleName = "store"

func Execute(ctx sdk.Context, content string, mint func(sdk.Context, string, string) error) error {
    stored := fmt.Sprintf("Stored: %s", content)
    return mint(ctx, ModuleName, stored)
}
