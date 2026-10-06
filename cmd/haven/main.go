// Command haven finds and connects to your servers.
package main

import (
	"context"
	"os"

	"github.com/ProductionPanic/haven/v2/internal/cli"
)

func main() {
	if err := cli.Execute(context.Background()); err != nil {
		os.Exit(1)
	}
}
