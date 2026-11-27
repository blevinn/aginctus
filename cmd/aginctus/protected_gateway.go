package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/blevinn/aginctus/internal/gateway"
)

func runProtectedGatewayPreview(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: aginctus internal gateway-protected-preview GRANT OPERATOR_CONFIG")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result, err := gateway.PreviewLocalProtectedGateway(ctx, args[0], args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "protected gateway observation failed")
		return 1
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		return 1
	}
	return 0
}
