package gateway

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

func gatewayLauncher(spec NativeGatewayInstanceSpec, pg string) map[string]string {
	entry := "/usr/bin/env POSTGRES_USER=litellm POSTGRES_DB=litellm POSTGRES_PASSWORD_FILE=/run/aginctus/gateway/postgres-password POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256 /usr/local/bin/docker-entrypoint.sh postgres"
	cwd := "/"
	if spec.Role == "litellm" {
		entry = "/app/.venv/bin/python -I -S /run/aginctus/gateway/litellm-bootstrap.py " + pg
		cwd = "/app"
	}
	return map[string]string{"oci.entrypoint": entry, "oci.cwd": cwd, "oci.uid": "0", "oci.gid": "0"}
}

func gatewayLauncherConfigSafe(config, want map[string]string, configured bool) bool {
	for k, v := range config {
		if strings.HasPrefix(k, "raw.") || strings.HasPrefix(k, "security.") || strings.HasPrefix(k, "environment.") {
			return false
		}
		if strings.HasPrefix(k, "oci.") {
			if !configured || want[k] != v || v == "" {
				return false
			}
		}
	}
	if configured {
		for k, v := range want {
			if config[k] != v {
				return false
			}
		}
	}
	return true
}
func gatewayLauncherMatches(i *api.Instance, spec NativeGatewayInstanceSpec, pg string) bool {
	want := gatewayLauncher(spec, pg)
	return i != nil && gatewayLauncherConfigSafe(i.Config, want, true) && gatewayLauncherConfigSafe(i.ExpandedConfig, want, true)
}

// For the initial runtime, require a dedicated unrouted IPv4 management
// bridge with managed DNS, no other attached instances/profiles, no egress
// NIC, and no other nondefault project permitted to attach. The pinned daemon
// administrator remains trusted. Shared gateway access is a separate policy.
func (e *protectedExecution) verifyPrivateGatewayNetwork(ctx context.Context) error {
	if e.guard(ctx) != nil || e.plan.top.EgressNetwork != "" {
		return errProtectedGateway
	}
	name := e.plan.top.ManagementNetwork
	n, _, err := e.sdk.GetNetwork(name)
	if err != nil || n == nil || !n.Managed || n.Type != "bridge" || n.Config["ipv4.nat"] != "false" ||
		n.Config["ipv4.routing"] != "false" || n.Config["ipv6.address"] != "none" ||
		n.Config["ipv4.dhcp"] == "false" || (n.Config["dns.mode"] != "" && n.Config["dns.mode"] != "managed") {
		return errProtectedGateway
	}
	want := []string{e.plan.pair.Postgres.Name, e.plan.pair.LiteLLM.Name}
	seen := map[string]bool{}
	for _, ref := range n.UsedBy {
		u, err := url.Parse(ref)
		if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" {
			return errProtectedGateway
		}
		guest, ok := strings.CutPrefix(u.Path, "/1.0/instances/")
		q, err := url.ParseQuery(u.RawQuery)
		if !ok || err != nil || len(q) != 1 || len(q["project"]) != 1 || q.Get("project") != e.grant.ProtectedProject ||
			!slices.Contains(want, guest) || seen[guest] {
			return errProtectedGateway
		}
		seen[guest] = true
	}
	if len(seen) != 2 {
		return errProtectedGateway
	}
	projects, err := e.sdk.GetProjects()
	if err != nil {
		return errProtectedGateway
	}
	for _, p := range projects {
		if p.Name == "default" || p.Name == e.grant.ProtectedProject {
			continue
		}
		// Do not assume denial from an absent/wildcard allowlist.
		access := p.Config["restricted.networks.access"]
		if p.Config["restricted"] != "true" || p.Config["restricted.devices.nic"] != "managed" || access == "" {
			return errProtectedGateway
		}
		for _, item := range strings.Split(access, ",") {
			item = strings.TrimSpace(item)
			if item == "" || item == name || strings.ContainsAny(item, "*?[]") {
				return errProtectedGateway
			}
		}
	}
	return ctx.Err()
}

type protectedRuntimeSDK struct {
	incus.InstanceServer
	exec  *protectedExecution
	ctx   context.Context
	guard func(context.Context) error
}

func (s protectedRuntimeSDK) ExecGuest(name string, req api.InstanceExecPost, args *incus.InstanceExecArgs) (GatewayGuestExecOperation, error) {
	if s.guard(s.ctx) != nil {
		return nil, errProtectedGateway
	}
	return s.ExecInstance(name, req, args)
}
func (s protectedRuntimeSDK) UpdateInstanceState(name string, req api.InstanceStatePut, etag string) (native.InstanceOperation, error) {
	role := ""
	switch name {
	case s.exec.plan.pair.Postgres.Name:
		role = "postgres"
	case s.exec.plan.pair.LiteLLM.Name:
		role = "litellm"
	}
	if role == "" || req.Action != "start" || s.guard(s.ctx) != nil || s.exec.session.BeginEffect(s.ctx, "start-"+role) != nil || s.guard(s.ctx) != nil {
		return nil, errProtectedGateway
	}
	op, err := s.InstanceServer.UpdateInstanceState(name, req, etag)
	if err != nil || op == nil {
		return nil, errProtectedGateway
	}
	return runtimeTrackedOperation{op, s.exec.session, s.guard}, nil
}

type runtimeTrackedOperation struct {
	incus.Operation
	session *environment.ProjectExecutionSession
	guard   func(context.Context) error
}

func (o runtimeTrackedOperation) WaitContext(ctx context.Context) error {
	if o.session.RecordDaemonOperation(ctx, o.Get().ID) != nil || o.Operation.WaitContext(ctx) != nil ||
		o.guard(ctx) != nil || o.session.MarkApplied(ctx) != nil {
		return errProtectedGateway
	}
	return nil
}

type protectedDeliveryVerifier func(context.Context, NativeGatewayRuntimeBinding) error

func (f protectedDeliveryVerifier) VerifyGatewayDelivery(ctx context.Context, b NativeGatewayRuntimeBinding) error {
	return f(ctx, b)
}

type protectedReadiness func(context.Context, NativeGatewayResourceIdentity, string) error

func (f protectedReadiness) VerifyGatewayServiceReady(ctx context.Context, id NativeGatewayResourceIdentity, role string) error {
	return f(ctx, id, role)
}

// StartLocalProtectedGateway configures fixed OCI commands and invokes the
// existing ordered native start primitive under the installed accepted pin.
// Only private gateway networking is supported in this first reviewed path.
func StartLocalProtectedGateway(ctx context.Context, grantPath, operatorPath string) (result NativeEnvironmentGatewayStartResult, id string, err error) {
	e, err := openProtectedExecution(ctx, grantPath, operatorPath, "gateway-start")
	if err != nil {
		return result, "", err
	}
	id = e.session.ID()
	defer func() { err = errors.Join(err, e.finish(ctx, err == nil)) }()
	if !e.candidate.Gateway.EnsureRunning || e.verifyPrivateGatewayNetwork(ctx) != nil {
		return result, id, errProtectedGateway
	}
	material, err := e.credentialMaterial()
	if err != nil {
		return result, id, errCredentialDelivery
	}
	receipt, err := e.deliveryReceipt()
	if err != nil {
		return result, id, errCredentialDelivery
	}
	images, _, err := e.pinnedImages(ctx)
	if err != nil {
		return result, id, errProtectedGateway
	}
	guard := func(ctx context.Context) error {
		if e.verifyPrivateGatewayNetwork(ctx) != nil || e.verifyDelivery(ctx, receipt, material, images) != nil {
			return errProtectedGateway
		}
		return ctx.Err()
	}
	if guard(ctx) != nil {
		return result, id, errProtectedGateway
	}
	specs := []NativeGatewayInstanceSpec{e.plan.pair.Postgres, e.plan.pair.LiteLLM}
	// Inspect both launchers before configuring either. A running guest
	// without the exact reviewed launcher is never modified or adopted.
	for _, spec := range specs {
		i, etag, err := e.sdk.GetInstance(spec.Name)
		if err != nil || i == nil || etag == "" {
			return result, id, errProtectedGateway
		}
		if gatewayLauncherMatches(i, spec, e.plan.pair.Postgres.Name) {
			continue
		}
		if i.Status != "Stopped" || !gatewayLauncherConfigSafe(i.Config, nil, false) || !gatewayLauncherConfigSafe(i.ExpandedConfig, nil, false) {
			return result, id, errProtectedGateway
		}
	}
	for _, spec := range specs {
		i, etag, err := e.sdk.GetInstance(spec.Name)
		if err != nil || i == nil || etag == "" {
			return result, id, errProtectedGateway
		}
		if gatewayLauncherMatches(i, spec, e.plan.pair.Postgres.Name) {
			continue
		}
		if i.Status != "Stopped" || !gatewayLauncherConfigSafe(i.Config, nil, false) || !gatewayLauncherConfigSafe(i.ExpandedConfig, nil, false) ||
			guard(ctx) != nil || e.session.BeginEffect(ctx, "configure-"+spec.Role) != nil || guard(ctx) != nil {
			return result, id, errProtectedGateway
		}
		put := i.Writable()
		put.Config = maps.Clone(put.Config)
		maps.Copy(put.Config, gatewayLauncher(spec, e.plan.pair.Postgres.Name))
		op, err := e.sdk.UpdateInstance(spec.Name, put, etag)
		if err != nil || op == nil || e.session.RecordDaemonOperation(ctx, op.Get().ID) != nil || op.WaitContext(ctx) != nil {
			return result, id, errProtectedGateway
		}
		got, _, err := e.sdk.GetInstance(spec.Name)
		if err != nil || !gatewayLauncherMatches(got, spec, e.plan.pair.Postgres.Name) || guard(ctx) != nil || e.session.MarkApplied(ctx) != nil {
			return result, id, errProtectedGateway
		}
	}
	physical := e.candidate.Environment
	physical.Binding.Project = e.grant.ProtectedProject
	identity, err := NativeGatewayIdentity(physical, physical.Binding)
	if err != nil {
		return result, id, errProtectedGateway
	}
	b := NativeGatewayRuntimeBinding{Protected: &e.grant, Resources: identity, AcceptedEnvironmentRevision: e.revision,
		CredentialVersion: material.version, Images: images, RootPool: e.candidate.Gateway.RootPool, DatabasePool: e.candidate.Gateway.DatabasePool}
	sdk := protectedRuntimeSDK{e.sdk, e, ctx, guard}
	verify := protectedDeliveryVerifier(func(ctx context.Context, current NativeGatewayRuntimeBinding) error {
		if current != b {
			return errProtectedGateway
		}
		return guard(ctx)
	})
	ready := protectedReadiness(func(ctx context.Context, current NativeGatewayResourceIdentity, role string) error {
		if current != identity {
			return errProtectedGateway
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		for {
			if guard(ctx) != nil {
				return errProtectedGateway
			}
			var err error
			switch role {
			case "postgres":
				err = VerifyNativeGatewayPostgresReadyWithClient(ctx, physical, physical.Binding, sdk)
			case "litellm":
				err = VerifyNativeGatewayLiteLLMReadyWithClient(ctx, physical, physical.Binding, sdk)
			default:
				return errProtectedGateway
			}
			if err == nil {
				return guard(ctx)
			}
			select {
			case <-ctx.Done():
				return errProtectedGateway
			case <-time.After(time.Second):
			}
		}
	})
	result, err = StartNativeEnvironmentGatewayWithClient(ctx, e.candidate.Environment, physical.Binding, e.plan.top, e.plan.devices, e.plan.pair, b, sdk, guard, verify, ready)
	return result, id, err
}
