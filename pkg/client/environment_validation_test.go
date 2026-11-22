package client

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

type environmentProjectionFixture struct {
	list   api.EnvironmentList
	detail api.EnvironmentDetail
	err    error
	calls  int
}

func (r *environmentProjectionFixture) ListEnvironments(context.Context, string) (api.EnvironmentList, error) {
	r.calls++
	return r.list, r.err
}
func (r *environmentProjectionFixture) GetEnvironment(context.Context, string, string) (api.EnvironmentDetail, error) {
	r.calls++
	return r.detail, r.err
}

func environmentValidationFixture() *environmentProjectionFixture {
	const id = "11111111-1111-4111-8111-111111111111"
	env := api.Environment{ID: id, Name: "dev", Binding: api.EnvironmentBinding{ServerID: "verified-server", Project: "team"}}
	return &environmentProjectionFixture{
		list:   api.EnvironmentList{TypeMeta: api.NewTypeMeta("EnvironmentList"), Items: []api.Environment{env}},
		detail: api.EnvironmentDetail{TypeMeta: api.NewTypeMeta("Environment"), Environment: env},
	}
}

func TestEnvironmentClientRejectsForeignAndCorruptInventory(t *testing.T) {
	req := ListEnvironmentsRequest{Project: "team"}
	for _, change := range []func(*environmentProjectionFixture){
		func(f *environmentProjectionFixture) { f.list.Kind = "PluginList" },
		func(f *environmentProjectionFixture) { f.list.Items = nil },
		func(f *environmentProjectionFixture) { f.list.Items[0].Binding.Project = "foreign" },
		func(f *environmentProjectionFixture) { f.list.Items[0].Binding.ServerID = "" },
		func(f *environmentProjectionFixture) { f.list.Items[0].ID = "not-a-uuid" },
		func(f *environmentProjectionFixture) { f.list.Items[0].Name = "" },
		func(f *environmentProjectionFixture) { f.list.Items[0].Name = "Invalid Name" },
		func(f *environmentProjectionFixture) {
			f.list.Items[0].Binding.ServerID = "bad" + string(rune('\n')) + "server"
		},
		func(f *environmentProjectionFixture) { f.list.Items[0].ID = "11111111-1111-1111-1111-111111111111" },
		func(f *environmentProjectionFixture) { f.list.Items = append(f.list.Items, f.list.Items[0]) },
	} {
		r := environmentValidationFixture()
		change(r)
		got, err := New(Options{EnvironmentReader: r}).Environments.List(context.Background(), req)
		if err == nil || got.Kind != "" || len(got.Items) > 0 {
			t.Fatalf("unsafe inventory: %+v err=%v", got, err)
		}
	}
}
func TestEnvironmentClientRejectsForeignDetailsAndBackendDisclosure(t *testing.T) {
	req := GetEnvironmentRequest{Project: "team", ID: "11111111-1111-4111-8111-111111111111"}
	for _, change := range []func(*environmentProjectionFixture){
		func(f *environmentProjectionFixture) { f.detail.Kind = "Workload" },
		func(f *environmentProjectionFixture) { f.detail.ID = "22222222-2222-4222-8222-222222222222" },
		func(f *environmentProjectionFixture) { f.detail.Binding.Project = "foreign" },
		func(f *environmentProjectionFixture) { f.detail.Binding.ServerID = "" },
	} {
		r := environmentValidationFixture()
		change(r)
		got, err := New(Options{EnvironmentReader: r}).Environments.Get(context.Background(), req)
		if err == nil || got.ID != "" {
			t.Fatalf("foreign detail escaped: %+v err=%v", got, err)
		}
	}
	r := environmentValidationFixture()
	r.err = errors.New("private state filesystem path")
	s := New(Options{EnvironmentReader: r}).Environments
	if _, err := s.List(context.Background(), ListEnvironmentsRequest{Project: "team"}); err == nil || strings.Contains(err.Error(), "filesystem") {
		t.Fatalf("list leak: %v", err)
	}
	if _, err := s.Get(context.Background(), req); err == nil || strings.Contains(err.Error(), "filesystem") {
		t.Fatalf("get leak: %v", err)
	}
	r.calls = 0
	if _, err := s.Get(context.Background(), GetEnvironmentRequest{Project: "team", ID: "dev"}); err == nil || r.calls != 0 {
		t.Fatalf("name used as immutable ID: %v calls=%d", err, r.calls)
	}
}
