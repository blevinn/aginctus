package gateway

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
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
func VerifyNativeGatewayPostgresReadyWithClient(ctx context.Context, descriptor environment.Descriptor, observed environment.Binding, client GatewayPostgresProbeClient) error {
	return verifyNativeGatewayGuestReady(ctx, descriptor, observed, client, "postgres", []string{
		"env", "-i", "PATH=/usr/local/bin:/usr/bin:/bin", "PGPASSFILE=/run/aginctus/gateway/postgres.pgpass",
		// libpq 17: a successful query against trust authentication is not readiness.
		// Pass it explicitly so a libpq too old to support it fails closed.
		"psql", "-X", "-w", "-d", "host=127.0.0.1 port=5432 user=litellm dbname=litellm require_auth=scram-sha-256 connect_timeout=3", "-tAc", "SELECT 1",
	})
}

func verifyNativeGatewayGuestReady(ctx context.Context, descriptor environment.Descriptor, observed environment.Binding, client GatewayPostgresProbeClient, role string, command []string) error {
	identity, err := NativeGatewayIdentity(descriptor, observed)
	if err != nil {
		return fmt.Errorf("gateway readiness requires verified environment binding")
	}
	project := identity.Project
	environmentID := identity.EnvironmentID
	instanceName := identity.PostgresInstance
	if role == "litellm" {
		instanceName = identity.LiteLLMInstance
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := client.GetConnectionInfo()
	if err != nil || info == nil || info.Protocol != "incus" || !filepath.IsAbs(info.SocketPath) || info.Project != project {
		return fmt.Errorf("gateway readiness selected-project transport unverified")
	}
	observe := func() (*api.Instance, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		inst, _, err := client.GetInstance(instanceName)
		if err != nil || inst == nil || inst.Name != instanceName || inst.Project != project ||
			inst.Status != "Running" || string(inst.Type) != "container" || inst.Config["user.aginctus.managed"] != "true" ||
			inst.Config["user.aginctus.resource"] != "gateway-instance" ||
			inst.Config["user.aginctus.role"] != role ||
			inst.Config["user.aginctus.deployment"] != project ||
			inst.Config["user.aginctus.environment-id"] != environmentID {
			return nil, fmt.Errorf("gateway readiness owned running guest unverified")
		}
		return inst, nil
	}
	before, err := observe()
	if err != nil {
		return err
	}
	done := make(chan bool)
	request := api.InstanceExecPost{
		Command:   command,
		WaitForWS: true,
	}
	op, err := client.ExecGuest(instanceName, request, &incusclient.InstanceExecArgs{
		Stdout: io.Discard, Stderr: io.Discard, DataDone: done,
	})
	if err != nil || op == nil {
		return fmt.Errorf("gateway authentication probe submission failed")
	}
	if err := op.WaitContext(ctx); err != nil {
		return fmt.Errorf("gateway authentication probe incomplete")
	}
	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("gateway authentication probe output incomplete")
	}
	meta := op.Get()
	code, ok := meta.Metadata["return"].(float64)
	if !ok || code != 0 {
		return fmt.Errorf("gateway authenticated readiness unverified")
	}
	after, err := observe()
	if err != nil || !after.CreatedAt.Equal(before.CreatedAt) || after.Config["volatile.base_image"] != before.Config["volatile.base_image"] {
		return fmt.Errorf("gateway readiness identity changed during probe")
	}
	return ctx.Err()
}
