package main

import (
	"context"
	"fmt"
	"os"

	"github.com/stack-research/substrate/internal/cli"
	"github.com/stack-research/substrate/internal/lifecycle"
)

func main() {
	ctx, cancel := lifecycle.SignalContext(context.Background())
	defer cancel()
	if err := cli.New().Root().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
