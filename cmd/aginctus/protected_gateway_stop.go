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

func runProtectedGatewayStop(args []string) int {
	if len(args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: aginctus internal gateway-protected-stop GRANT OPERATOR_CONFIG POSTGRES_CREATED_AT LITELLM_CREATED_AT")
		return 2
	}
	pg, err := time.Parse(time.RFC3339Nano, args[2])
	if err != nil || pg.IsZero() {
		fmt.Fprintln(os.Stderr, "invalid PostgreSQL creation timestamp")
		return 2
	}
	llm, err := time.Parse(time.RFC3339Nano, args[3])
	if err != nil || llm.IsZero() {
		fmt.Fprintln(os.Stderr, "invalid LiteLLM creation timestamp")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 10*time.Minute)
	defer timeout()
	result, id, err := gateway.StopLocalProtectedGateway(ctx, args[0], args[1], pg, llm)
	if id != "" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"operationId": id, "result": result})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "protected gateway stop failed; inspect operation before retry")
		return 1
	}
	return 0
}
