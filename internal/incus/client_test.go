package incus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	output []byte
	err    error
	name   string
	args   []string
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return r.output, r.err
}

func TestVersion(t *testing.T) {
	runner := &fakeRunner{output: []byte("6.0.0\n")}
	client := NewClientWithRunner(runner)

	version, err := client.Version(context.Background())
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if version != "6.0.0" {
		t.Fatalf("Version() = %q, want %q", version, "6.0.0")
	}
	if runner.name != "incus" || len(runner.args) != 1 || runner.args[0] != "version" {
		t.Fatalf("runner called with %q %v", runner.name, runner.args)
	}
}

func TestCheckDaemonWrapsError(t *testing.T) {
	runner := &fakeRunner{output: []byte("connection refused"), err: errors.New("exit status 1")}
	client := NewClientWithRunner(runner)

	err := client.CheckDaemon(context.Background())
	if err == nil {
		t.Fatal("CheckDaemon() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("CheckDaemon() error = %q, want command output", err)
	}
}
