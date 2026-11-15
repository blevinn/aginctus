package gateway

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"time"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// GatewayGuestExecOperation is the bounded Incus operation observation needed
// to establish an authenticated PostgreSQL query exit status.
type GatewayGuestExecOperation interface {
	WaitContext(context.Context) error
	Get() api.Operation
}

// GatewayPostgresProbeClient exposes project-scoped reads and a guest exec.
// No host shell, environment secret, file upload, or instance update is used.
type GatewayPostgresProbeClient interface {
	GetConnectionInfo() (*incusclient.ConnectionInfo, error)
	GetInstance(string) (*api.Instance, string, error)
	ExecGuest(string, api.InstanceExecPost, *incusclient.InstanceExecArgs) (GatewayGuestExecOperation, error)
}

// VerifyNativeGatewayPostgresReadyWithClient tests a real TCP password-auth
// connection as the expected litellm database principal. Using 127.0.0.1
// deliberately avoids Postgres's potentially trust-authenticated local Unix
// socket. PGPASSFILE is a fixed in-guest path: the separately trusted delivery
// authority must prepare it with restrictive permissions and version binding.
//
// The caller must provide the exact environment-owned guest identity and a
// separately authenticated, restricted project transport. This helper never
// treats project selection as permission or verifies credential provenance.
// No passwords, host paths, tokens or arbitrary commands are accepted here.
// pg_isready alone is insufficient; psql -w SELECT 1 fails closed if password
// auth fails. This probe does not establish the credential-file's provenance.
var gatewayOwnerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func VerifyNativeGatewayPostgresReadyWithClient(ctx context.Context, project, environmentID, instanceName string, client GatewayPostgresProbeClient) error {
	if _, err := incusscope.NewProjectScope(project); err != nil {
		return fmt.Errorf("gateway PostgreSQL readiness requires selected project")
	}
	if !gatewayOwnerID.MatchString(environmentID) || !gatewayOwnerID.MatchString(instanceName) || instanceName == "postgres-1" {
		return fmt.Errorf("gateway PostgreSQL readiness requires scoped environment and guest identity")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := client.GetConnectionInfo()
	if err != nil || info == nil || info.Protocol != "incus" || !filepath.IsAbs(info.SocketPath) || info.Project != project {
		return fmt.Errorf("gateway PostgreSQL readiness selected-project transport unverified")
	}
	observe := func() (*api.Instance, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		inst, _, err := client.GetInstance(instanceName)
		if err != nil || inst == nil || inst.Name != instanceName || inst.Project != project ||
			inst.Status != "Running" || string(inst.Type) != "container" || inst.Config["user.aginctus.managed"] != "true" ||
			inst.Config["user.aginctus.resource"] != "gateway-instance" ||
			inst.Config["user.aginctus.role"] != "postgres" ||
			inst.Config["user.aginctus.deployment"] != project ||
			inst.Config["user.aginctus.environment-id"] != environmentID {
			return nil, fmt.Errorf("gateway PostgreSQL readiness owned running guest unverified")
		}
		return inst, nil
	}
	before, err := observe()
	if err != nil {
		return err
	}
	done := make(chan bool)
	request := api.InstanceExecPost{
		Command: []string{"env", "PGPASSFILE=/run/aginctus/gateway/postgres.pgpass",
			"psql", "-h", "127.0.0.1", "-U", "litellm", "-d", "litellm", "-w", "-tAc", "SELECT 1"},
		WaitForWS: true,
	}
	op, err := client.ExecGuest(instanceName, request, &incusclient.InstanceExecArgs{
		Stdout: io.Discard, Stderr: io.Discard, DataDone: done,
	})
	if err != nil || op == nil {
		return fmt.Errorf("gateway PostgreSQL authentication probe submission failed")
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("gateway PostgreSQL authentication probe incomplete")
	}
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("gateway PostgreSQL authentication probe output incomplete")
	}
	meta := op.Get()
	code, ok := meta.Metadata["return"].(float64)
	if !ok || code != 0 {
		return fmt.Errorf("gateway PostgreSQL authenticated query failed")
	}
	after, err := observe()
	if err != nil || !after.CreatedAt.Equal(before.CreatedAt) || after.Config["volatile.base_image"] != before.Config["volatile.base_image"] {
		return fmt.Errorf("gateway PostgreSQL readiness identity changed during probe")
	}
	return ctx.Err()
}
