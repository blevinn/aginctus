package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/herdrclient"
	"github.com/blevinn/aginctus/internal/managementnetwork"
	"github.com/blevinn/aginctus/internal/workload"
)

func TestAggregateRemoveDependencyOrder(t *testing.T) {
	previousGateway := removeAggregateGateway
	t.Cleanup(func() { removeAggregateGateway = previousGateway })
	var calls []string
	stubWorkload(t, func(_ context.Context, s workload.Spec, o workload.Options) error {
		if o.Operation != "delete" || o.Force || o.DryRun {
			t.Fatalf("workload options: %#v", o)
		}
		calls = append(calls, "workload-"+s.ID)
		return nil
	})
	stubHerdrClient(t, func(_ context.Context, _ herdrclient.Spec, o herdrclient.Options) error {
		if o.Operation != "delete" || o.Force {
			t.Fatalf("Herdr options: %#v", o)
		}
		calls = append(calls, "herdr")
		return nil
	})
	removeAggregateGateway = func(_ context.Context, _ gateway.Spec) error { calls = append(calls, "gateway"); return nil }
	stubManagementNetwork(t, func(_ context.Context, _ managementnetwork.Spec, o managementnetwork.Options) error {
		if o.Operation != "delete" || o.Force {
			t.Fatalf("network options: %#v", o)
		}
		calls = append(calls, "network")
		return nil
	})
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var out, stderr bytes.Buffer
	args := []string{
		"--config=workloads.alpha.instance.name=aginctus-alpha",
		"--config=workloads.alpha.runtime.type=opencode",
		"--config=workloads.alpha.isolation=container",
		"--config=workloads.alpha.image.alias=aginctus-opencode-workload",
		"--config=workloads.alpha.storage.pool=default",
		"remove",
	}
	code := Run(context.Background(), args, &out, &stderr, &fakeIncusClient{}, loader)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	want := []string{"workload-dev", "workload-alpha", "herdr", "gateway", "network"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v, want %v", calls, want)
	}
}
func TestAggregateRemoveStopsBeforeNetworkOnGatewayFailure(t *testing.T) {
	previousGateway := removeAggregateGateway
	t.Cleanup(func() { removeAggregateGateway = previousGateway })
	stubWorkload(t, func(context.Context, workload.Spec, workload.Options) error { return nil })
	stubHerdrClient(t, func(context.Context, herdrclient.Spec, herdrclient.Options) error { return nil })
	removeAggregateGateway = func(context.Context, gateway.Spec) error { return errors.New("ownership mismatch") }
	stubManagementNetwork(t, func(context.Context, managementnetwork.Spec, managementnetwork.Options) error {
		t.Fatal("network should not be deleted")
		return nil
	})
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"remove"}, &out, &stderr, &fakeIncusClient{}, loader)
	if code != 1 || !strings.Contains(stderr.String(), "ownership mismatch") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}
func TestAggregateRemoveRejectsFlags(t *testing.T) {
	var out, stderr bytes.Buffer
	code := runAggregateRemove(context.Background(), []string{"--force"}, &out, &stderr, nil)
	if code != 2 || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestAggregateRemoveTwice(t *testing.T) {
 previousGateway:=removeAggregateGateway
 t.Cleanup(func(){removeAggregateGateway=previousGateway})
 var count int
 stubWorkload(t,func(_ context.Context,_ workload.Spec,o workload.Options)error{
  if o.Operation!="delete" || o.Force {t.Fatalf("unexpected workload options: %#v",o)}
  count++;return nil
 })
 stubHerdrClient(t,func(_ context.Context,_ herdrclient.Spec,o herdrclient.Options)error{
  if o.Operation!="delete" || o.Force {t.Fatalf("unexpected Herdr options: %#v",o)}
  count++;return nil
 })
 removeAggregateGateway=func(context.Context,gateway.Spec)error{count++;return nil}
 stubManagementNetwork(t,func(_ context.Context,_ managementnetwork.Spec,o managementnetwork.Options)error{
  if o.Operation!="delete" || o.Force {t.Fatalf("unexpected network options: %#v",o)}
  count++;return nil
 })
 loader:=config.NewLoader();loader.SystemPath="";loader.UserPath="";loader.Environment=nil
 for i:=0;i<2;i++ {
  var out,stderr bytes.Buffer
  if code:=Run(context.Background(),[]string{"remove"},&out,&stderr,&fakeIncusClient{},loader);code!=0 {
   t.Fatalf("remove attempt %d: code=%d stderr=%q",i,code,stderr.String())
  }
 }
 if count!=8 {t.Fatalf("expected each of four stages to run twice, count=%d",count)}
}
