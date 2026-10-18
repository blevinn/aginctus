package compose

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/lxc/incus-compose/iclient"
)

// TestExecuteComposePropagatesConnectionFactoryError verifies the
// hermetic factory injection: when the connection factory fails,
// executeCompose surfaces a documented diagnostic that wraps the
// original error.
func TestExecuteComposePropagatesConnectionFactoryError(t *testing.T) {
	compose := []byte(`services:
  app:
    image: docker.io/library/alpine:3.21
    command: ["sleep", "3600"]
`)
	want := errors.New("fake connection unavailable")
	err := executeCompose(
		context.Background(),
		Config{Project: "aginctus-test", Compose: compose},
		nil,
		func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
			return nil, want
		},
	)
	if err == nil {
		t.Fatal("expected error from connection factory")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrap of %v", err, want)
	}
	if !strings.Contains(err.Error(), "construct local Incus connection") {
		t.Fatalf("error = %v, want message about compose connection", err)
	}
}

// TestExecuteComposeReportsUnavailableSocket verifies that, with a
// factory returning a real Connection pointed at a missing socket,
// executeCompose reaches the connect stage and returns a documented
// error class instead of panicking or returning a zero error.
func TestExecuteComposeReportsUnavailableSocket(t *testing.T) {
	tmp := t.TempDir()
	socket := tmp + "/incus.sock"

	compose := []byte(`services:
  app:
    image: docker.io/library/alpine:3.21
    command: ["sleep", "3600"]
`)
	err := executeCompose(
		context.Background(),
		Config{Project: "aginctus-test", Compose: compose},
		nil,
		func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
			return iclient.NewConnection(&iclient.ConfigRemoteInfo{
				Name:     "local",
				Addrs:    []string{"unix://" + socket},
				Protocol: "incus",
			})
		},
	)
	if err == nil {
		t.Fatal("expected error from missing Incus socket")
	}
	if !strings.Contains(err.Error(), "connect") {
		t.Fatalf("error = %v, want connect-stage diagnostic", err)
	}
}

// TestExecuteComposeSequencingBeforeConnection verifies that
// writeCompose and loadProject run before the connection factory is
// even consulted. A factory that records its calls lets the test
// confirm the early steps executed.
func TestExecuteComposeSequencingBeforeConnection(t *testing.T) {
	compose := []byte(`services:
  app:
    image: docker.io/library/alpine:3.21
    command: ["sleep", "3600"]
`)
	var (
		mu         sync.Mutex
		factoryHit bool
	)
	err := executeCompose(
		context.Background(),
		Config{Project: "aginctus-test", Compose: compose},
		nil,
		func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
			mu.Lock()
			factoryHit = true
			mu.Unlock()
			return nil, errors.New("connection unavailable")
		},
	)
	if err == nil {
		t.Fatal("expected error from connection factory")
	}
	mu.Lock()
	defer mu.Unlock()
	if !factoryHit {
		t.Fatal("connection factory was not invoked")
	}
}

// TestDriverWithConnectionFactoryUsesFactory confirms the
// constructor wires the supplied factory into the driver's
// execute closure by inspecting the live Driver.execute field.
func TestDriverWithConnectionFactoryUsesFactory(t *testing.T) {
	var hits int
	factory := func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
		hits++
		return nil, errors.New("unavailable")
	}
	d := NewWithConnectionFactory(nil, factory)
	if d.execute == nil {
		t.Fatal("driver.execute is nil")
	}
	compose := []byte(`services:
  app:
    image: docker.io/library/alpine:3.21
    command: ["sleep", "3600"]
`)
	err := d.execute(context.Background(),
		Config{Project: "aginctus-test", Compose: compose}, nil)
	if err == nil {
		t.Fatal("expected error from wired factory")
	}
	if hits != 1 {
		t.Fatalf("factory hits = %d, want 1", hits)
	}
}
