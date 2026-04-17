package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sapihav/tavily-cli/cmd"
	"github.com/sapihav/tavily-cli/internal/client"
)

func main() {
	// Propagate SIGINT/SIGTERM into command contexts so in-flight HTTP requests
	// can abort cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx))
}

func run(ctx context.Context) int {
	err := cmd.ExecuteContext(ctx)
	if err == nil {
		return 0
	}

	// Map error class -> exit code. Stderr prints a single human-readable line;
	// no wrapping JSON envelope yet (YAGNI for M1).
	fmt.Fprintf(os.Stderr, "error: %v\n", err)

	switch {
	case cmd.IsUserError(err), errors.Is(err, client.ErrMissingAPIKey):
		return client.ExitUserError
	}

	var netErr *client.NetworkError
	if errors.As(err, &netErr) {
		return client.ExitNetworkError
	}

	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return client.ExitAPIError
	}

	return client.ExitAPIError
}
