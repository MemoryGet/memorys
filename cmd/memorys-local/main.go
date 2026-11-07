package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/MemoryGet/memorys/pkg/localcli"
)

func webCommand(args []string) bool {
	return len(args) > 0 && args[0] == "web"
}

func main() {
	if webCommand(os.Args[1:]) {
		configureWebParallelism()
		// This interactive local service should not be treated as background
		// work on Windows. Unsupported systems keep their default scheduling.
		requestWebHighQoS()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := localcli.Run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
