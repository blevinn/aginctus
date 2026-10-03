package incus

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Client struct {
	runner Runner
}

func NewClient() *Client {
	return &Client{runner: execRunner{}}
}

func NewClientWithRunner(runner Runner) *Client {
	return &Client{runner: runner}
}

func (c *Client) Version(ctx context.Context) (string, error) {
	output, err := c.runner.Run(ctx, "incus", "version")
	if err != nil {
		return "", fmt.Errorf("run incus version: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return strings.TrimSpace(string(output)), nil
}

func (c *Client) CheckDaemon(ctx context.Context) error {
	output, err := c.runner.Run(ctx, "incus", "info")
	if err != nil {
		return fmt.Errorf("run incus info: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}
