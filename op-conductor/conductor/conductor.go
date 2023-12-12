package conductor

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/ethereum-optimism/optimism/op-service/cliapp"
)

func Main() cliapp.LifecycleAction {
	return func(ctx *cli.Context, closeApp context.CancelCauseFunc) (cliapp.Lifecycle, error) {
		fmt.Println("Hello, world!")
		return nil, nil
	}
}
