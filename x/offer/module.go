package offer

import (
    "fmt"
    sdk "github.com/cosmos/cosmos-sdk/types"
)

const ModuleName = "offer"

func Execute(ctx sdk.Context, content string, mint func(sdk.Context, string, string) error) error {
    offered := fmt.Sprintf("Offered: %s", content)
    return mint(ctx, ModuleName, offered)
}
