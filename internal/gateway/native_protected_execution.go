package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/blevinn/aginctus/internal/filestate"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	check "github.com/blevinn/aginctus/pkg/orchestration"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const protectedGatewayRoot = "/var/lib/aginctus/protected-gateway"

type protectedExecution struct {
	grant      environment.ProtectedGatewayGrant
	revision   environment.AcceptedRevision
	candidate  environment.Candidate
	plan       protectedGatewayPlan
	sdk        incus.InstanceServer
	session    *environment.ProjectExecutionSession
	provenance *LocalGatewayImageProvenance
	guard      func(context.Context) error
	close      func()
}

// trustedGatewayPath is for direct administrator invocation, never candidate
// paths. Every ancestor must be root-owned and non-writable by other users.
func trustedGatewayPath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for dir := path; ; dir = filepath.Dir(dir) {
		st, err := os.Lstat(dir)
		if err != nil || !st.IsDir() || st.Mode().Perm()&0o022 != 0 {
			return false
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != 0 {
			return false
		}
		if dir == "/" {
			return true
		}
	}
}

func openProtectedExecution(ctx context.Context, grantPath, operatorPath, operation string) (*protectedExecution, error) {
	if ctx == nil || ctx.Err() != nil || os.Getuid() != 0 || os.Geteuid() != 0 ||
		!trustedGatewayPath(protectedGatewayRoot) || !trustedGatewayPath(filepath.Dir(operatorPath)) {
		return nil, errProtectedGateway
	}
	g, err := environment.ReadProtectedGatewayGrant(grantPath)
	if err != nil {
		return nil, errProtectedGateway
	}
	operator, err := environment.ReadRestrictedOperatorReadConfig(operatorPath)
	if err != nil || operator.Project != g.Project || "sha256:"+operator.TrustedServerSHA256 != g.ServerID {
		return nil, errProtectedGateway
	}
	// One cooperating administrator lock across physical gateway projects on
	// this daemon protects the daemon-wide layer cache acquisition check.
	lock, err := filestate.LockRegistry(filepath.Join(protectedGatewayRoot, "native-execution"))
	if err != nil {
		return nil, errProtectedGateway
	}
	ok := false
	defer func() {
		if !ok {
			_ = lock.Close()
		}
	}()
	dir := filepath.Join(protectedGatewayRoot, g.ProtectedProject, g.EnvironmentID)
	if !trustedGatewayPath(dir) {
		return nil, errProtectedGateway
	}
	provenance, err := OpenLocalGatewayImageProvenance(dir)
	if err != nil {
		return nil, errProtectedGateway
	}
	provenance.protected = &g
	server, err := incus.ConnectIncusUnixWithContext(ctx, protectedGatewaySocket, &incus.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return nil, errProtectedGateway
	}
	defer func() {
		if !ok {
			server.Disconnect()
		}
	}()
	sdk := server.UseProject(g.ProtectedProject)
	session, candidate, closeReads, err := environment.OpenProjectExecution(ctx, operator, g.Project, g.EnvironmentID, g.RevisionID, "gateway-credentials", operation)
	if err != nil {
		return nil, errProtectedGateway
	}
	defer func() {
		if !ok {
			finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = session.Finish(finish, false)
			closeReads()
		}
	}()
	r := session.Revision()
	plan, err := planProtectedGateway(g, r, candidate)
	if err != nil {
		return nil, errProtectedGateway
	}
	guard := func(ctx context.Context) error {
		current, err := environment.ReadProtectedGatewayGrant(grantPath)
		held, lockErr := lock.Stat()
		named, nameErr := os.Lstat(filepath.Join(protectedGatewayRoot, "native-execution.flock"))
		if err != nil || current != g || lockErr != nil || nameErr != nil || !os.SameFile(held, named) ||
			verifyProtectedGatewayProject(ctx, g, candidate, sdk) != nil || session.Check(ctx) != nil {
			return errProtectedGateway
		}
		return ctx.Err()
	}
	if err := guard(ctx); err != nil {
		return nil, err
	}
	ok = true
	return &protectedExecution{g, r, candidate, plan, sdk, session, provenance, guard, func() {
		closeReads()
		server.Disconnect()
		_ = lock.Close()
	}}, nil
}

func (e *protectedExecution) finish(ctx context.Context, success bool) error {
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	defer e.close()
	if e.session.Finish(finish, success) != nil {
		return errors.New("protected gateway coordination outcome uncertain; inspect operation")
	}
	return nil
}

// protectedProvisionSDK adapts the existing native binding to the installed
// coordinator, checking the grant immediately at actual SDK submission.
type protectedProvisionSDK struct {
	incus.InstanceServer
	ctx  context.Context
	exec *protectedExecution
}

func (s protectedProvisionSDK) CreateImage(p api.ImagesPost) (native.GatewayImageAcquireOperation, error) {
	if err := s.exec.guard(s.ctx); err != nil {
		return nil, err
	}
	op, err := s.InstanceServer.CreateImage(p, nil)
	if err != nil || op == nil {
		return nil, errProtectedGateway
	}
	return protectedTrackedOperation{op, s.exec.session}, nil
}
func (s protectedProvisionSDK) CreateInstance(p api.InstancesPost) (native.InstanceCreateOperation, error) {
	if err := s.exec.guard(s.ctx); err != nil {
		return nil, err
	}
	op, err := s.InstanceServer.CreateInstance(p)
	if err != nil || op == nil {
		return nil, errProtectedGateway
	}
	return protectedTrackedOperation{op, s.exec.session}, nil
}
func (s protectedProvisionSDK) CreateImageAlias(p api.ImageAliasesPost) error {
	if err := s.exec.guard(s.ctx); err != nil {
		return err
	}
	return s.InstanceServer.CreateImageAlias(p)
}
func (s protectedProvisionSDK) CreateStoragePoolVolume(pool string, p api.StorageVolumesPost) error {
	if err := s.exec.guard(s.ctx); err != nil {
		return err
	}
	return s.InstanceServer.CreateStoragePoolVolume(pool, p)
}

type protectedTrackedOperation struct {
	incus.Operation
	session *environment.ProjectExecutionSession
}

func (o protectedTrackedOperation) WaitContext(ctx context.Context) error {
	if o.session.RecordDaemonOperation(ctx, o.Get().ID) != nil {
		return errProtectedGateway
	}
	return o.Operation.WaitContext(ctx)
}

func protectedProvisionAction(resource string, effect orchestration.NativeEffect) string {
	if effect.Action != native.Create {
		return ""
	}
	switch resource {
	case orchestration.GatewayImageStage:
		switch effect.Target {
		case orchestration.GatewayPostgresTarget:
			return "image-postgres"
		case orchestration.GatewayLiteLLMTarget:
			return "image-litellm"
		case orchestration.GatewayPostgresAliasTarget:
			return "alias-postgres"
		case orchestration.GatewayLiteLLMAliasTarget:
			return "alias-litellm"
		}
	case orchestration.GatewayVolumeStage:
		if effect.Target == orchestration.GatewayDatabaseTarget {
			return "volume-database"
		}
	case orchestration.GatewayInstanceStage:
		switch effect.Target {
		case orchestration.GatewayPostgresTarget:
			return "instance-postgres"
		case orchestration.GatewayLiteLLMTarget:
			return "instance-litellm"
		}
	}
	return ""
}

// ProvisionLocalProtectedGateway runs the actual native infrastructure prefix
// with logical revision unchanged and independently granted physical placement.
// It never starts a service, configures the host, or consumes credentials.
func ProvisionLocalProtectedGateway(ctx context.Context, grantPath, operatorPath string) (result orchestration.Result, operationID string, err error) {
	e, err := openProtectedExecution(ctx, grantPath, operatorPath, "gateway-provision")
	if err != nil {
		return result, "", err
	}
	operationID = e.session.ID()
	defer func() { err = errors.Join(err, e.finish(ctx, err == nil)) }()
	preview, err := orchestration.PreviewAcceptedIntent(e.revision, e.candidate, orchestration.AcceptedIntentSelection{Gateway: true})
	if err != nil {
		return result, operationID, errProtectedGateway
	}
	// Physical descriptor is used solely by domain SDK identity checks. It is
	// never promoted or substituted for the actual accepted logical revision.
	physical := e.candidate.Environment
	physical.Binding.Project = e.grant.ProtectedProject
	b := acceptedGatewayBinding{deps: AcceptedGatewayDependencies{
		Client: protectedProvisionSDK{e.sdk, ctx, e}, Resolve: nativeoci.ResolvePublic,
		ExistingImageEvidence: e.provenance.ExistingImageEvidence, LocalImageProvenance: e.provenance},
		revision: e.revision, desc: physical, top: e.plan.top, devices: e.plan.devices, pair: e.plan.pair,
		specs: [2]environment.GatewayImageSpec{e.candidate.Gateway.Postgres, e.candidate.Gateway.LiteLLM}}
	resources := map[string]orchestration.NativeResourceBinding{
		orchestration.GatewayImageStage:    b.binding(orchestration.GatewayImageStage, orchestration.GatewayPostgresTarget, b.previewImages, b.applyImages),
		orchestration.GatewayVolumeStage:   b.binding(orchestration.GatewayVolumeStage, orchestration.GatewayDatabaseTarget, b.previewVolume, b.applyVolume),
		orchestration.GatewayInstanceStage: b.binding(orchestration.GatewayInstanceStage, orchestration.GatewayPostgresTarget, b.previewInstances, b.applyInstances),
	}
	for id, bound := range resources {
		apply := bound.Apply
		bound.Apply = func(ctx context.Context, run orchestration.RunNativeEffect) error {
			return apply(ctx, func(effect orchestration.NativeEffect, submit func(context.Context) error) error {
				if submit == nil {
					return run(effect, nil)
				}
				return run(effect, func(ctx context.Context) error {
					action := protectedProvisionAction(id, effect)
					if action == "" || e.guard(ctx) != nil || e.session.BeginEffect(ctx, action) != nil {
						return errProtectedGateway
					}
					if submit(ctx) != nil || e.guard(ctx) != nil || e.session.MarkApplied(ctx) != nil {
						return errProtectedGateway
					}
					return nil
				})
			})
		}
		resources[id] = bound
	}
	result, err = orchestration.NewEngine(nil).ExecuteNative(ctx, preview.Selection, orchestration.NativeBindings{
		Resources: resources, CheckPolicy: orchestration.CheckPolicy{Mode: orchestration.ReadOnlyChecks,
			Authorize: func(ctx context.Context, scope orchestration.TargetScope, c check.Check) error {
				if scope.Deployment != e.grant.EnvironmentID || c.Effect != check.ReadOnly {
					return errProtectedGateway
				}
				return e.guard(ctx)
			}},
		VerifyPlan: func(ctx context.Context, p orchestration.NativePlan) error {
			if !reflect.DeepEqual(p, preview.Selection) {
				return errProtectedGateway
			}
			return e.guard(ctx)
		}, BeforeEffect: func(ctx context.Context, r environment.AcceptedRevision, resource orchestration.NativeResource, effect orchestration.NativeEffect) error {
			if r != e.revision || protectedProvisionAction(resource.ID, effect) == "" {
				return errProtectedGateway
			}
			return e.guard(ctx)
		},
	}, orchestration.ExecuteOptions{})
	return result, operationID, err
}
