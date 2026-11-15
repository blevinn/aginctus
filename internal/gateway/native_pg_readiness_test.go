package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	if name != "postgres-1" || !req.WaitForWS || len(req.Command) < 10 ||
		req.Command[0] != "env" || req.Command[1] != "PGPASSFILE=/run/aginctus/gateway/postgres.pgpass" ||
		req.Command[4] != "127.0.0.1" || strings.Contains(strings.Join(req.Command, " "), "synthetic-secret") {
		return nil, errors.New("unsafe exec request")
	}
	close(args.DataDone)
	return pgProbeOperation{op: &api.Operation{Metadata: map[string]any{"return": f.code}}, err: f.waitErr}, nil
}
func pgProbeFixture() *fakePGProbeClient {
	project := "gateway-test"
	return &fakePGProbeClient{
		info: &incusclient.ConnectionInfo{Project: project, Protocol: "incus", SocketPath: "/run/incus/unix.socket"},
		instance: &api.Instance{
			Name: "postgres-1", Project: project, Status: "Running", Type: "container",
			InstancePut: api.InstancePut{Config: map[string]string{
				"user.aginctus.managed": "true", "user.aginctus.resource": "gateway-instance",
				"user.aginctus.role": "postgres", "user.aginctus.deployment": project,
				"volatile.base_image": strings.Repeat("a", 64),
			}},
		},
	}
}
func TestPostgresReadinessAuthenticatedGuestTCPQuery(t *testing.T) {
	f := pgProbeFixture()
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), "gateway-test", f); err != nil || f.calls != 1 {
		t.Fatalf("expected guest auth probe: %v calls=%d", err, f.calls)
	}
	f.code = 1
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), "gateway-test", f); err == nil {
		t.Fatal("failed authentication accepted")
	}
}
func TestPostgresReadinessRejectsForeignAndUntrustedTransport(t *testing.T) {
	f := pgProbeFixture()
	f.instance.Config["user.aginctus.resource"] = "compose-instance"
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), "gateway-test", f); err == nil || f.calls != 0 {
		t.Fatal("foreign instance exec permitted")
	}
	f = pgProbeFixture()
	f.info.Project = "default"
	if err := VerifyNativeGatewayPostgresReadyWithClient(context.Background(), "gateway-test", f); err == nil || f.calls != 0 {
		t.Fatal("wrong project exec permitted")
	}
}
