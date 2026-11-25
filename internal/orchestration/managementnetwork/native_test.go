package managementnetwork

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/native"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

type hostBridgeFixture struct {
	t       *testing.T
	network *api.Network
	writes  int
}

func (b *hostBridgeFixture) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	return &incus.ConnectionInfo{Protocol: "incus", Project: "default", SocketPath: "/fixture/socket"}, nil
}
func (b *hostBridgeFixture) GetNetwork(name string) (*api.Network, string, error) {
	if b.network == nil || name != b.network.Name {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "absent")
	}
	return b.network, "bridge-etag", nil
}
func (b *hostBridgeFixture) CreateNetwork(post api.NetworksPost) error {
	b.writes++
	b.network = &api.Network{Name: post.Name, Type: post.Type, Project: "default", Managed: true, NetworkPut: post.NetworkPut}
	return nil
}
func (b *hostBridgeFixture) UpdateNetwork(name string, put api.NetworkPut, etag string) error {
	if b.network == nil || b.network.Name != name || etag != "bridge-etag" || put.Config["operator.setting"] != "keep" || put.Description != "operator" {
		b.t.Fatal("bridge update lost scope, ETag, or operator-owned fields")
	}
	b.writes++
	b.network.NetworkPut = put
	return nil
}
func (b *hostBridgeFixture) DeleteNetwork(name string) error {
	if b.network == nil || b.network.Name != name {
		b.t.Fatal("unexpected bridge deletion")
	}
	b.writes++
	b.network = nil
	return nil
}

func TestNativeHostBridgeReconcilesOnlyAnApprovedEffect(t *testing.T) {
	b := &hostBridgeFixture{t: t}
	s := Spec{Name: "management", IPv4Address: "auto", IPv6Address: "none"}
	ctx := t.Context()
	if r, err := s.ExecuteNativeHost(ctx, Options{DryRun: true}, b, nil); err != nil || r.Action != native.Create || b.writes != 0 {
		t.Fatalf("host dry-run: %+v %v", r, err)
	}
	if _, err := s.ExecuteNativeHost(ctx, Options{}, b, nil); err == nil {
		t.Fatal("host write lacked independent authority")
	}
	guard := func(want native.Action) func(context.Context, native.NetworkIntent, native.Action) error {
		return func(_ context.Context, in native.NetworkIntent, action native.Action) error {
			if in.Name != s.Name || in.Scope.Project() != "default" || in.Config[native.NetworkPolicyKey] != "none" || action != want {
				t.Fatalf("guard saw wrong intent or action: %+v %s", in, action)
			}
			// A callback receiving its own intent cannot change the actual write.
			in.Config[native.NetworkPolicyKey] = "unexpected"
			return nil
		}
	}
	if r, err := s.ExecuteNativeHost(ctx, Options{}, b, guard(native.Create)); err != nil || !r.Verified || b.writes != 1 {
		t.Fatalf("host create: %+v %v", r, err)
	}
	b.network.Config["operator.setting"] = "keep"
	b.network.Description = "operator"
	s.IPv4Routing = true
	if r, err := s.ExecuteNativeHost(ctx, Options{}, b, guard(native.Update)); err != nil || !r.Verified || b.writes != 2 {
		t.Fatalf("host update: %+v %v", r, err)
	}
	b.network.UsedBy = []string{"/1.0/instances/workload?project=team"}
	if _, err := s.ExecuteNativeHost(ctx, Options{Operation: "delete"}, b, guard(native.Delete)); err == nil || b.writes != 2 {
		t.Fatal("deleted an attached bridge")
	}
	b.network.UsedBy = nil
	denied := errors.New("host authority denied this effect")
	if _, err := s.ExecuteNativeHost(ctx, Options{Operation: "delete"}, b, func(context.Context, native.NetworkIntent, native.Action) error { return denied }); !errors.Is(err, denied) || b.writes != 2 {
		t.Fatal("host effect bypassed authority")
	}
	if r, err := s.ExecuteNativeHost(ctx, Options{Operation: "delete"}, b, guard(native.Delete)); err != nil || !r.Verified || b.network != nil || b.writes != 3 {
		t.Fatalf("host remove: %+v %v", r, err)
	}
}

func TestNativeHostBridgeDoesNotSwitchUpdateToCreateAfterGuard(t *testing.T) {
	b := &hostBridgeFixture{t: t}
	s := Spec{Name: "management", IPv4Address: "auto", IPv6Address: "none"}
	if _, err := s.ExecuteNativeHost(t.Context(), Options{}, b, func(context.Context, native.NetworkIntent, native.Action) error { return nil }); err != nil {
		t.Fatal(err)
	}
	s.IPv4Routing = true
	_, err := s.ExecuteNativeHost(t.Context(), Options{}, b, func(_ context.Context, _ native.NetworkIntent, action native.Action) error {
		if action != native.Update {
			t.Fatal("expected an update at the authority boundary")
		}
		b.network = nil
		return nil
	})
	if err == nil || b.writes != 1 {
		t.Fatalf("unapproved create was not blocked: writes=%d err=%v", b.writes, err)
	}
}
