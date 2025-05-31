package anchor

import (
    "fmt"
    sdk "github.com/cosmos/cosmos-sdk/types"
)

const ModuleName = "anchor"

func Execute(ctx sdk.Context, target string, mint func(sdk.Context, string, string) error) error {
    anchoring := fmt.Sprintf("Anchored to: %s", target)
    return mint(ctx, ModuleName, anchoring)
}
