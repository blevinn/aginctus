package compose

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lxc/incus-compose/iclient"
	"github.com/lxc/incus/v7/shared/api"
)

// TestIncusComposePersistentVolumeIntegration requires a disposable Incus
// daemon, using a uniquely named project cleaned up unless --retain.
// It proves a sentinel written by an old container can be read by its
// replacement, not merely that a volume name survives in the device map.
func TestIncusComposePersistentVolumeIntegration(t *testing.T) {
	if os.Getenv("AGINCTUS_INCUS_INTEGRATION") != "1" {
		t.Skip("requires AGINCTUS_INCUS_INTEGRATION=1")
	}
	if os.Getenv("AGINCTUS_INCUS_DISPOSABLE") != "YES" {
		t.Fatal("set AGINCTUS_INCUS_DISPOSABLE=YES only on a disposable Incus host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	name := fmt.Sprintf("aginctus-volume-it-%d", time.Now().UnixNano())
	t.Logf("persistent-volume test project %q", name)
	integrationProjectCleanup(t, name)

	doc := func(image, command string) []byte {
		return []byte(fmt.Sprintf(`services:
  app:
    image: %s
    command: ["sh", "-c", %q]
    volumes:
      - data:/data
volumes:
  data:
`, image, command))
	}
	initial := doc("docker.io/library/alpine:3.21", "echo aginctus-volume-sentinel > /data/sentinel; sleep 3600")
	replacement := doc("docker.io/library/alpine:3.22", "test \"$(cat /data/sentinel)\" = aginctus-volume-sentinel && sleep 3600")
	run := func(contents []byte, recreate bool) error {
		return executeCompose(ctx, Config{Project: name, Compose: contents, RecreateOnDrift: recreate}, nil)
	}
	if err := run(initial, false); err != nil {
		t.Fatalf("create initial volume-bearing instance: %v", err)
	}
	// Allow the first container to write its sentinel before replacement.
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(2 * time.Second):
	}
	if err := run(replacement, true); err != nil {
		t.Fatalf("recreate with retained named volume: %v", err)
	}
	conn, err := iclient.NewConnection(&iclient.ConfigRemoteInfo{Name: "local", Addrs: []string{"unix://"}, Protocol: "incus"})
	if err != nil {
		t.Fatal(err)
	}
	names, err := conn.GetInstanceNames(ctx, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Fatalf("wanted exactly one replacement instance, got %v", names)
	}
	// The replacement command exits immediately if the persistent sentinel
	// is missing or its bytes differ. Wait for it to settle before checking.
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(2 * time.Second):
	}
	instance, _, err := conn.GetInstance(ctx, name, names[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyComposeInstanceOwnership(instance.Config, name, instance.Name); err != nil {
		t.Fatal(err)
	}
	if instance.StatusCode != api.Running {
		t.Fatalf("replacement is %s; sentinel check may have failed (inspect project %q)", instance.Status, name)
	}
	if err := run(replacement, false); err != nil {
		t.Fatalf("unchanged replacement not idempotent: %v", err)
	}
}
