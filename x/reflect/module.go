package reflect

import (
    "fmt"
    sdk "github.com/cosmos/cosmos-sdk/types"
)

const ModuleName = "reflect"

func Execute(ctx sdk.Context, content string, mint func(sdk.Context, string, string) error) error {
    reflection := fmt.Sprintf("Reflection: %s", content)
    return mint(ctx, ModuleName, reflection)
}
