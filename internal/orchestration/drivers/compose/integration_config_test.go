package compose

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lxc/incus-compose/iclient"
)

// Validate changing environment and command on the fully rendered Compose
// model, including explicit fail-closed behavior and opt-in recreation.
func TestIncusComposeConfigurationIntegration(t *testing.T) {
	if os.Getenv("AGINCTUS_INCUS_INTEGRATION") != "1" {
		t.Skip("requires AGINCTUS_INCUS_INTEGRATION=1")
	}
	if os.Getenv("AGINCTUS_INCUS_DISPOSABLE") != "YES" {
		t.Fatal("set AGINCTUS_INCUS_DISPOSABLE=YES only on disposable Incus")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	name := fmt.Sprintf("aginctus-config-it-%d", time.Now().UnixNano())
	t.Logf("configuration test project %q", name)
	integrationProjectCleanup(t, name)
	doc := func(envValue, cmdValue string) []byte {
		return []byte(fmt.Sprintf(`services:
  app:
    image: docker.io/library/alpine:3.21
    environment:
      AGINCTUS_REVISION: %q
    command: ["sh", "-c", %q]
`, envValue, cmdValue))
	}
	run := func(envValue, cmdValue string, recreate bool) error {
		return executeCompose(ctx, Config{Project: name, Compose: doc(envValue, cmdValue), RecreateOnDrift: recreate}, nil, iclient.NewConnection)
	}
	firstCmd := "sleep 3600"
	secondCmd := "echo changed; sleep 3600"
	if err := run("first", firstCmd, false); err != nil {
		t.Fatalf("initial apply: %v", err)
	}
	if err := run("first", firstCmd, false); err != nil {
		t.Fatalf("unchanged apply: %v", err)
	}
	if err := run("second", firstCmd, false); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("environment change did not fail closed: %v", err)
	}
	if err := run("second", firstCmd, true); err != nil {
		t.Fatalf("recreate changed environment: %v", err)
	}
	if err := run("second", firstCmd, false); err != nil {
		t.Fatalf("idempotence after environment recreation: %v", err)
	}
	if err := run("second", secondCmd, false); err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("command change did not fail closed: %v", err)
	}
	if err := run("second", secondCmd, true); err != nil {
		t.Fatalf("recreate changed command: %v", err)
	}
	if err := run("second", secondCmd, false); err != nil {
		t.Fatalf("idempotence after command recreation: %v", err)
	}
	conn, err := iclient.NewConnection(&iclient.ConfigRemoteInfo{Name: "local", Addrs: []string{"unix://"}, Protocol: "incus"})
	if err != nil {
		t.Fatal(err)
	}
	names, err := conn.GetInstanceNames(ctx, name, nil)
	if err != nil || len(names) != 1 {
		t.Fatalf("expected one instance, got %v: %v", names, err)
	}
	inst, _, err := conn.GetInstance(ctx, name, names[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyComposeInstanceOwnership(inst.Config, name, inst.Name); err != nil {
		t.Fatal(err)
	}
}
