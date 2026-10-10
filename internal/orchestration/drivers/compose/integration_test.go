package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lxc/incus-compose/iclient"
	"github.com/lxc/incus/v7/shared/api"
)

// Run only with AGINCTUS_INCUS_INTEGRATION=1 against a disposable Incus host.
// A unique project is created for every test. The test never removes existing
// projects or instances, and deliberately leaves artifacts for inspection.
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
	t.Logf("test project %s (retained for manual inspection/cleanup)", name)
	composeDoc := func(marker string) []byte {
		// Use a pinned tag for repeatability; changing an extension exercises
		// drift without assuming anything about mutable registry tags.
		return []byte(fmt.Sprintf("services:\n  app:\n    image: docker.io/library/alpine:3.21\n    command: [\"sleep\", \"3600\"]\n    x-incus-compose:\n      config:\n        user.aginctus.test-revision: %q\n", marker))
	}
	run := func(marker string, recreate bool) error {
		raw, err := json.Marshal(Config{Project:name, Compose:json.RawMessage("null"), RecreateOnDrift:recreate})
		_ = raw // JSON driver config is not required for the runtime entry point.
		if err != nil { return err }
		return executeCompose(ctx, Config{Project:name, Compose:composeDoc(marker), RecreateOnDrift:recreate},nil)
	}
	if err := run("v1", false); err != nil { t.Fatalf("initial deployment: %v",err) }
	if err := run("v1", false); err != nil { t.Fatalf("idempotent deployment: %v",err) }
	err := run("v2", false)
	if err == nil || !strings.Contains(err.Error(),"drift") {
		t.Fatalf("expected fail-closed drift, got %v",err)
	}
	if err := run("v2", true); err != nil { t.Fatalf("opt-in recreation: %v",err) }
	if err := run("v2", false); err != nil { t.Fatalf("post-recreation idempotence: %v",err) }
	conn,err:=iclient.NewConnection(&iclient.ConfigRemoteInfo{Name:"local",Addrs:[]string{"unix://"},Protocol:"incus"})
	if err != nil {t.Fatal(err)}
	instanceNames,err:=conn.GetInstanceNames(ctx,name,nil)
	if err != nil {t.Fatal(err)}
	if len(instanceNames)!=1 {t.Fatalf("expected one instance, got %v",instanceNames)}
	inst,_,err:=conn.GetInstance(ctx,name,instanceNames[0],nil)
	if err!=nil {t.Fatal(err)}
	if err:=verifyComposeInstanceOwnership(inst.Config,name,inst.Name);err!=nil{t.Fatal(err)}
	if inst.Config["user.aginctus.test-revision"]!="v2" {
		t.Fatalf("recreated revision did not converge")
	}
	_ = api.InstanceTypeContainer
}
