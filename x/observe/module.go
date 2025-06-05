package observe

import (
    "fmt"
    sdk "github.com/cosmos/cosmos-sdk/types"
)

const ModuleName = "observe"

func Execute(ctx sdk.Context, content string, mint func(sdk.Context, string, string) error) error {
    noted := fmt.Sprintf("Observed: %s", content)
    return mint(ctx, ModuleName, noted)
}
