// Command x is a personal CLI for reading X (Twitter) free public surfaces.
//
// It derives from Nitter (AGPL-3.0-only); see the NOTICE and LICENSE files.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/x-cli/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(kit.Run(ctx, cli.New()))
}
