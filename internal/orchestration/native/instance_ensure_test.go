package native

import (
	"context"
	"errors"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type ensureFake struct {
	*fakeCreateClient
	updates, starts int
	failStart       bool
}

func (f *ensureFake) UpdateInstance(_ string, put api.InstancePut, etag string) (InstanceOperation, error) {
	f.updates++
	if etag != "created-etag" {
		return nil, errors.New("wrong ETag")
	}
	f.instance.InstancePut = put
	return &fakeInstanceOperation{}, nil
}

func (f *ensureFake) UpdateInstanceState(_ string, put api.InstanceStatePut, etag string) (InstanceOperation, error) {
	f.starts++
	if etag != "created-etag" || put.Action != "start" {
		return nil, errors.New("invalid transition")
	}
	if f.failStart {
		return nil, errors.New("sensitive daemon failure")
	}
	f.instance.Status = "Running"
	return &fakeInstanceOperation{}, nil
}

func TestEnsureCreateUpdateStartAndDryRun(t *testing.T) {
	intent, _ := instanceFixture()
	f := &ensureFake{fakeCreateClient: &fakeCreateClient{intent: intent}}
	preview, err := EnsureInstanceWithClient(context.Background(), intent, true, f)
	if err != nil || preview.Plan.Action != Create || preview.Verified || f.creates+f.updates+f.starts != 0 {
		t.Fatalf("unsafe preview: %+v %v", preview, err)
	}
	first, err := EnsureInstanceWithClient(context.Background(), intent, false, f)
	if err != nil || !first.Verified || !first.Created || !first.Started {
		t.Fatalf("create/start: %+v %v", first, err)
	}
	intent.Description = "updated"
	preview, err = EnsureInstanceWithClient(context.Background(), intent, true, f)
	if err != nil || preview.Plan.Action != Update || f.updates != 0 {
		t.Fatalf("update preview: %+v %v", preview, err)
	}
	second, err := EnsureInstanceWithClient(context.Background(), intent, false, f)
	if err != nil || !second.Verified || !second.Updated || second.Created || second.Started {
		t.Fatalf("update: %+v %v", second, err)
	}
	third, err := EnsureInstanceWithClient(context.Background(), intent, false, f)
	if err != nil || !third.Verified || third.Plan.Action != Unchanged || f.creates != 1 || f.updates != 1 || f.starts != 1 {
		t.Fatalf("idempotence: %+v %v", third, err)
	}
}

func TestEnsureRetainsPartialCompletionAndNeverStartsAfterUnsafePlan(t *testing.T) {
	intent, _ := instanceFixture()
	f := &ensureFake{fakeCreateClient: &fakeCreateClient{intent: intent}, failStart: true}
	result, err := EnsureInstanceWithClient(context.Background(), intent, false, f)
	var failure *ReconcileError
	if !errors.As(err, &failure) || !failure.Uncertain || !result.Created || result.Verified || f.creates != 1 || f.starts != 1 {
		t.Fatalf("partial failure: %+v %v", result, err)
	}
	f.failStart = false
	intent.Devices["root"]["pool"] = "foreign-pool"
	result, err = EnsureInstanceWithClient(context.Background(), intent, false, f)
	if err == nil || result.Verified || f.starts != 1 || f.updates != 0 {
		t.Fatalf("unsafe replacement: %+v %v", result, err)
	}
}
