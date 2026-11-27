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

func runProtectedGatewayProvision(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: aginctus internal gateway-protected-provision GRANT OPERATOR_CONFIG")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	result, id, err := gateway.ProvisionLocalProtectedGateway(ctx, args[0], args[1])
	if id != "" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"operationId": id, "result": result})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "protected gateway provisioning failed; inspect operation before retry")
		return 1
	}
	return 0
}

func runProtectedGatewayPublish(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: aginctus internal gateway-protected-publish GRANT OPERATOR_CONFIG")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	receipt, id, err := gateway.PublishLocalProtectedGatewayCredentials(ctx, args[0], args[1])
	if id != "" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"operationId": id, "receipt": receipt})
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "protected gateway credential publication failed; inspect operation before retry: %v\n", err)
		return 1
	}
	return 0
}

func runProtectedGatewayStart(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: aginctus internal gateway-protected-start GRANT OPERATOR_CONFIG")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	result, id, err := gateway.StartLocalProtectedGateway(ctx, args[0], args[1])
	if id != "" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"operationId": id, "result": result})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "protected gateway startup failed; inspect operation before retry")
		return 1
	}
	return 0
}
