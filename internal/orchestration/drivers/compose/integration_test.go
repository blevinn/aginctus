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

// Run only with AGINCTUS_INCUS_INTEGRATION=1 against a disposable Incus host.
// A unique project is created for every test. The test never removes existing
// projects or instances; cleanup is disabled only with --retain.
func TestIncusComposeReconciliationIntegration(t *testing.T) {
	if os.Getenv("AGINCTUS_INCUS_INTEGRATION") != "1" {
		t.Skip("requires AGINCTUS_INCUS_INTEGRATION=1 and disposable Incus")
	}
	if os.Getenv("AGINCTUS_INCUS_DISPOSABLE") != "YES" {
		t.Fatal("set AGINCTUS_INCUS_DISPOSABLE=YES to acknowledge the isolated test host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	name := fmt.Sprintf("aginctus-it-%d", time.Now().UnixNano())
	t.Logf("test project %s", name)
	integrationProjectCleanup(t, name)
	composeDoc := func(image string) []byte {
		return []byte(fmt.Sprintf(`services:
  app:
    image: %s
    command: ["sleep", "3600"]
`, image))
	}
	run := func(image string, recreate bool) error {
		return executeCompose(ctx, Config{Project: name, Compose: composeDoc(image), RecreateOnDrift: recreate}, nil)
	}
	const first = "docker.io/library/alpine:3.21"
	const second = "docker.io/library/alpine:3.22"
	if err := run(first, false); err != nil {
		t.Fatalf("initial deployment: %v", err)
	}
	if err := run(first, false); err != nil {
		t.Fatalf("idempotent deployment: %v", err)
	}
	err := run(second, false)
	if err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("expected fail-closed image drift, got %v", err)
	}
	if err := run(second, true); err != nil {
		t.Fatalf("opt-in recreation: %v", err)
	}
	if err := run(second, false); err != nil {
		t.Fatalf("post-recreation idempotence: %v", err)
	}
	conn, err := iclient.NewConnection(&iclient.ConfigRemoteInfo{Name: "local", Addrs: []string{"unix://"}, Protocol: "incus"})
	if err != nil {
		t.Fatal(err)
	}
	instanceNames, err := conn.GetInstanceNames(ctx, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(instanceNames) != 1 {
		t.Fatalf("expected one instance, got %v", instanceNames)
	}
	inst, _, err := conn.GetInstance(ctx, name, instanceNames[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyComposeInstanceOwnership(inst.Config, name, inst.Name); err != nil {
		t.Fatal(err)
	}
	if inst.Config["user.image_alias"] == "" {
		t.Fatal("recreated instance is missing image alias")
	}
}
