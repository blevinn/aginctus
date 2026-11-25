package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

type pgProbeOperation struct {
	op  *api.Operation
	err error
}

func (p pgProbeOperation) WaitContext(context.Context) error { return p.err }
func (p pgProbeOperation) Get() api.Operation {
	if p.op == nil {
		return api.Operation{}
	}
	return *p.op
}

type fakePGProbeClient struct {
	info     *incusclient.ConnectionInfo
	instance *api.Instance
	calls    int
	code     float64
	waitErr  error
}

func (f *fakePGProbeClient) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return f.info, nil
}
func (f *fakePGProbeClient) GetInstance(string) (*api.Instance, string, error) {
	return f.instance, "etag", nil
}
func (f *fakePGProbeClient) ExecGuest(name string, req api.InstanceExecPost, args *incusclient.InstanceExecArgs) (GatewayGuestExecOperation, error) {
	f.calls++
	if name != f.instance.Name || !req.WaitForWS || len(req.Command) != 11 ||
		req.Command[0] != "env" || req.Command[3] != "PGPASSFILE=/run/aginctus/gateway/postgres.pgpass" ||
		!strings.Contains(req.Command[8], "require_auth=scram-sha-256") || !strings.Contains(req.Command[8], "host=127.0.0.1") ||
		strings.Contains(strings.Join(req.Command, " "), "synthetic-secret") {
		return nil, errors.New("unsafe exec request")
	}
	close(args.DataDone)
	return pgProbeOperation{op: &api.Operation{Metadata: map[string]any{"return": f.code}}, err: f.waitErr}, nil
}
func pgProbeFixture(t *testing.T) (environment.Descriptor, *fakePGProbeClient) {
	t.Helper()
	desc := gatewayTestEnvironment("123e4567-e89b-42d3-a456-426614174000", "gateway-test")
	id, err := NativeGatewayIdentity(desc, desc.Binding)
	if err != nil {
		t.Fatal(err)
	}
	return desc, &fakePGProbeClient{
		info: &incusclient.ConnectionInfo{Project: desc.Binding.Project, Protocol: "incus", SocketPath: "/run/incus/unix.socket"},
		instance: &api.Instance{
			Name: id.PostgresInstance, Project: id.Project, Status: "Running", Type: "container",
			InstancePut: api.InstancePut{Config: map[string]string{
				"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-instance",
				"user.aginctus.role": "postgres", "user.aginctus.deployment": id.Project,
				"user.aginctus.environment-id": id.EnvironmentID,
				"volatile.base_image":          strings.Repeat("a", 64),
			}},
		},
	}
}
func TestPostgresReadinessAuthenticatedGuestTCPQuery(t *testing.T) {
	d, f := pgProbeFixture(t)
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), d, d.Binding, f); err != nil || f.calls != 1 {
		t.Fatalf("expected authenticated guest query: %v calls=%d", err, f.calls)
	}
	f.code = 1
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), d, d.Binding, f); err == nil {
		t.Fatal("failed authentication accepted")
	}
}
func TestPostgresReadinessRejectsForeignAndUntrustedTransport(t *testing.T) {
	d, f := pgProbeFixture(t)
	f.instance.Config["user.aginctus.resource"] = "compose-instance"
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), d, d.Binding, f); err == nil || f.calls != 0 {
		t.Fatal("foreign instance exec permitted")
	}
	d, f = pgProbeFixture(t)
	f.info.Project = "default"
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), d, d.Binding, f); err == nil || f.calls != 0 {
		t.Fatal("wrong project exec permitted")
	}
}
func TestPostgresReadinessRejectsCrossEnvironmentAndLegacyGuest(t *testing.T) {
	d, f := pgProbeFixture(t)
	f.instance.Config["user.aginctus.environment-id"] = "123e4567-e89b-42d3-a456-426614174001"
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), d, d.Binding, f); err == nil || f.calls != 0 {
		t.Fatal("foreign environment guest accepted")
	}
	d, f = pgProbeFixture(t)
	f.instance.Name = "postgres-1"
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), d, d.Binding, f); err == nil || f.calls != 0 {
		t.Fatal("legacy project-wide guest accepted")
	}
	d, f = pgProbeFixture(t)
	bad := d.Binding
	bad.ServerID = "different-server"
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), d, bad, f); err == nil || f.calls != 0 {
		t.Fatal("cross-server binding accepted")
	}
	d, f = pgProbeFixture(t)
	bad = d.Binding
	bad.Project = "foreign"
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), d, bad, f); err == nil || f.calls != 0 {
		t.Fatal("cross-project binding accepted")
	}
}
