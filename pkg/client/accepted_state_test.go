package client

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

const acceptedTestEnvID = "11111111-1111-4111-8111-111111111111"

type acceptedStateTestReader struct {
	calls  int
	result api.AcceptedStateStatus
	err    error
}

func (r *acceptedStateTestReader) GetAcceptedState(_ context.Context, _, _ string) (api.AcceptedStateStatus, error) {
	r.calls++
	return r.result, r.err
}

func goodAcceptedStatus() api.AcceptedStateStatus {
	return api.AcceptedStateStatus{
		TypeMeta: api.NewTypeMeta("AcceptedStateStatus"),
		Environment: api.Environment{
			ID: acceptedTestEnvID, Name: "dev",
			Binding: api.EnvironmentBinding{ServerID: "actual-incus-server", Project: "team"},
		},
		Layer: "accepted", HasAccepted: false,
	}
}

func TestAcceptedClientRejectsUnknownAuthorityAndCrossScope(t *testing.T) {
	ctx := context.Background()
	r := &acceptedStateTestReader{result: goodAcceptedStatus()}
	s := New(Options{AcceptedStateReader: r}).AcceptedStates
	for _, request := range []GetAcceptedStateRequest{
		{}, {Project: "team"}, {EnvironmentID: acceptedTestEnvID},
	} {
		if _, err := s.Get(ctx, request); err == nil || r.calls != 0 {
			t.Fatalf("unverified selector was used: %+v %v", request, err)
		}
	}
	request := GetAcceptedStateRequest{Project: "team", EnvironmentID: acceptedTestEnvID}
	status, err := s.Get(ctx, request)
	if err != nil || status.HasAccepted || status.Revision != nil || r.calls != 1 {
		t.Fatalf("empty authorized state: %+v %v", status, err)
	}
	for _, change := range []func(*api.AcceptedStateStatus){
		func(v *api.AcceptedStateStatus) { v.Environment.ID = "22222222-2222-4222-8222-222222222222" },
		func(v *api.AcceptedStateStatus) { v.Environment.Binding.Project = "default" },
		func(v *api.AcceptedStateStatus) { v.Environment.Binding.ServerID = "" },
		func(v *api.AcceptedStateStatus) { v.Layer = "configured" },
		func(v *api.AcceptedStateStatus) { v.Kind = "WorkloadList" },
		func(v *api.AcceptedStateStatus) { v.HasAccepted = true },
	} {
		r.result = goodAcceptedStatus()
		change(&r.result)
		if _, err := s.Get(ctx, request); err == nil {
			t.Fatalf("mismatched read result was trusted: %+v", r.result)
		}
	}
	r.result = goodAcceptedStatus()
	r.err = errors.New("sensitive-permission-failure")
	if _, err := s.Get(ctx, request); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("state service error became absent state: %v", err)
	}
	empty := New(Options{}).AcceptedStates
	if _, err := empty.Get(ctx, request); err == nil {
		t.Fatal("missing authenticated authority reported empty state")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	calls := r.calls
	if _, err := s.Get(cancelled, request); !errors.Is(err, context.Canceled) || r.calls != calls {
		t.Fatalf("canceled read called backend: %v", err)
	}
}

func TestAcceptedClientRetainsOpaqueRevisionWithoutAuthority(t *testing.T) {
	v := goodAcceptedStatus()
	v.HasAccepted = true
	v.Revision = &api.AcceptedRevisionIdentity{
		ID: strings.Repeat("a", 64), CandidateDigest: strings.Repeat("b", 64), Generation: 3,
		ParentID: strings.Repeat("c", 64),
	}
	r := &acceptedStateTestReader{result: v}
	got, err := New(Options{AcceptedStateReader: r}).AcceptedStates.Get(
		context.Background(), GetAcceptedStateRequest{Project: "team", EnvironmentID: acceptedTestEnvID},
	)
	if err != nil || got.Revision == nil || got.Revision.Generation != 3 {
		t.Fatalf("typed accepted revision lost: %+v %v", got, err)
	}
}
