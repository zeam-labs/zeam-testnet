package affirm

import (
    "fmt"
    sdk "github.com/cosmos/cosmos-sdk/types"
)

const ModuleName = "affirm"

func Execute(ctx sdk.Context, content string, mint func(sdk.Context, string, string) error) error {
    affirmation := fmt.Sprintf("Affirmed: %s", content)
    return mint(ctx, ModuleName, affirmation)
}
