package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lxc/incus-compose/iclient"
	"github.com/lxc/incus/v7/shared/api"
)

// Exercises the real stop/delete boundary, then deliberately injects a failed
// create callback. Only run on an explicitly acknowledged disposable host.
func TestIncusComposeRecoveryIntegration(t *testing.T) {
	if os.Getenv("AGINCTUS_INCUS_INTEGRATION") != "1" {
		t.Skip("requires AGINCTUS_INCUS_INTEGRATION=1")
	}
	if os.Getenv("AGINCTUS_INCUS_DISPOSABLE") != "YES" {
		t.Fatal("set AGINCTUS_INCUS_DISPOSABLE=YES only on disposable Incus")
	}
	if _, err := exec.LookPath("incus"); err != nil {
		t.Skipf("incus CLI required for failure-boundary integration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	project := fmt.Sprintf("aginctus-recovery-it-%d", time.Now().UnixNano())
	t.Logf("retaining test project %q for manual inspection and cleanup", project)
	compose := []byte(`services:
  app:
    image: docker.io/library/alpine:3.21
    command: ["sh", "-c", "test \"$(cat /data/sentinel)\" = recovery-sentinel && sleep 3600"]
    volumes:
      - data:/data
volumes:
  data:
`)
	first := []byte(strings.Replace(string(compose), "test \"$(cat /data/sentinel)\" = recovery-sentinel && sleep 3600", "echo recovery-sentinel > /data/sentinel; sleep 3600", 1))
	run := func(doc []byte) error {
		return executeCompose(ctx, Config{Project: project, Compose: doc}, nil)
	}
	if err := run(first); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	select {
	case <-ctx.Done(): t.Fatal(ctx.Err())
	case <-time.After(2 * time.Second):
	}
	conn, err := iclient.NewConnection(&iclient.ConfigRemoteInfo{Name:"local", Addrs:[]string{"unix://"},Protocol:"incus"})
	if err != nil {t.Fatal(err)}
	names, err := conn.GetInstanceNames(ctx, project, nil)
	if err != nil || len(names)!=1 {t.Fatalf("expected one instance: names=%v err=%v",names,err)}
	name:=names[0]
	fetchOwned := func(ctx context.Context) error {
		inst,_,err:=conn.GetInstance(ctx,project,name,nil)
		if err!=nil {return err}
		return verifyComposeInstanceOwnership(inst.Config,project,name)
	}
	inst,_,err:=conn.GetInstance(ctx,project,name,nil)
	if err!=nil {t.Fatal(err)}
	runIncus := func(args ...string) func(context.Context) error {
		return func(ctx context.Context) error {
			cmd:=exec.CommandContext(ctx,"incus",args...)
			out,err:=cmd.CombinedOutput()
			if err!=nil {return fmt.Errorf("incus command failed: %w (%s)",err,strings.TrimSpace(string(out)))}
			return nil
		}
	}
	injected := errors.New("intentional replacement create failure")
	err = recreateWithRetainedVolumes(ctx, inst.Devices, inst.Devices, fetchOwned,
		runIncus("stop","--project",project,name),
		runIncus("delete","--project",project,name),
		func(context.Context) error {return injected},
	)
	var recovery *ComposeRecreationRecoveryError
	if !errors.As(err,&recovery) || !errors.Is(err,injected) {
		t.Fatalf("expected typed post-delete recovery: %v",err)
	}
	if _,_,err:=conn.GetInstance(ctx,project,name,nil);err==nil {
		t.Fatal("old instance still present after injected create failure")
	}
	// Reapply desired state: the instance is missing, but its named volume
	// must survive and the replacement command checks the sentinel bytes.
	if err := run(compose); err != nil { t.Fatalf("reapply after injected failure: %v",err) }
	select {
	case <-ctx.Done(): t.Fatal(ctx.Err())
	case <-time.After(2 * time.Second):
	}
	replaced,_,err:=conn.GetInstance(ctx,project,name,nil)
	if err!=nil {t.Fatal(err)}
	if err:=verifyComposeInstanceOwnership(replaced.Config,project,name);err!=nil {t.Fatal(err)}
	if replaced.StatusCode!=api.Running {
		t.Fatalf("replacement not running (%s): persistent sentinel could not be verified; project=%s",replaced.Status,project)
	}
	if err:=run(compose);err!=nil {t.Fatalf("post-recovery idempotence: %v",err)}
}
