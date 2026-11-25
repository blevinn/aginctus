package gateway

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	check "github.com/blevinn/aginctus/pkg/orchestration"
)

type AcceptedGatewayClient interface {
	NativeGatewayStoppedCreateClient
	GatewayVolumeClient
	native.GatewayImageAcquireClient
	native.GatewayAliasCreateClient
}

// AcceptedGatewayDependencies are explicitly supplied by the trusted fixture
// composer. ExistingImageEvidence must establish prior verified acquisition
// independently of the accepted candidate, alias, and daemon layer cache.
type AcceptedGatewayDependencies struct {
	Client                AcceptedGatewayClient
	Resolve               func(context.Context, nativeoci.Source) (nativeoci.ResolvedImage, error)
	ExistingImageEvidence func(context.Context, environment.AcceptedRevision, string) (native.GatewayImageEvidence, error)
}

// BindAcceptedGateway is the fixture composer for LocalNativeExecution.Bind.
// It assembles closures only. Runtime stages are refused during binding even
// if the accepted candidate requests running; the infrastructure prefix may
// be selected independently and its postcondition is stopped, never ready.
func BindAcceptedGateway(c environment.Candidate, plan orchestration.NativePlan, deps AcceptedGatewayDependencies) (map[string]orchestration.NativeResourceBinding, error) {
	if c.Gateway == nil || plan.Revision.ValidateAgainst(c) != nil || plan.Validate() != nil || deps.Client == nil || deps.Resolve == nil || deps.ExistingImageEvidence == nil {
		return nil, errors.New("accepted gateway requires a pinned candidate and trusted fixture dependencies")
	}
	kinds := map[string]orchestration.NativeResourceKind{
		orchestration.GatewayImageStage:    orchestration.NativeGatewayImage,
		orchestration.GatewayVolumeStage:   orchestration.NativeGatewayVolume,
		orchestration.GatewayInstanceStage: orchestration.NativeGatewayInstance,
	}
	for _, r := range plan.Resources {
		if r.Kind == orchestration.NativeGatewayRuntime {
			return nil, errors.New("native gateway runtime requires approved credential delivery")
		}
		if kind, ok := kinds[r.ID]; !ok || kind != r.Kind ||
			(r.Kind == orchestration.NativeGatewayVolume && !slices.Contains(r.DependsOn, orchestration.GatewayImageStage)) ||
			(r.Kind == orchestration.NativeGatewayInstance && !slices.Contains(r.DependsOn, orchestration.GatewayVolumeStage)) {
			return nil, errors.New("invalid accepted gateway stage or prerequisite")
		}
	}
	g := *c.Gateway
	ref := func(i environment.GatewayImageSpec) string {
		return i.Registry + "/" + i.Repository + "@" + i.ManifestDigest
	}
	s := Spec{ID: c.Environment.ID, Project: c.Environment.Binding.Project, Network: g.ManagementNetwork, EgressNetwork: g.EgressNetwork, EgressIPv4Address: g.EgressIPv4, PostgresImage: ref(g.Postgres), LiteLLMImage: ref(g.LiteLLM)}
	top, err := s.NativeTopology()
	if err != nil {
		return nil, errors.New("invalid accepted gateway topology")
	}
	top, err = BindNativeGatewayTopology(top, c.Environment, c.Environment.Binding)
	if err != nil {
		return nil, err
	}
	devices, err := PlanNativeGatewayDevices(top, g.RootPool, g.DatabasePool)
	if err != nil {
		return nil, err
	}
	pair, err := PlanNativeGatewayInstances(top, devices, g.Postgres.Alias, g.LiteLLM.Alias)
	if err != nil {
		return nil, err
	}
	if g.Postgres.IncusFingerprint == g.LiteLLM.IncusFingerprint {
		return nil, errors.New("gateway services cannot share a layer-only image identity")
	}
	b := acceptedGatewayBinding{deps: deps, revision: plan.Revision, desc: c.Environment, top: top, devices: devices, pair: pair, specs: [2]environment.GatewayImageSpec{g.Postgres, g.LiteLLM}}
	out := make(map[string]orchestration.NativeResourceBinding, len(plan.Resources))
	for _, r := range plan.Resources {
		switch r.Kind {
		case orchestration.NativeGatewayImage:
			out[r.ID] = b.binding(r.ID, orchestration.GatewayPostgresTarget, b.previewImages, b.applyImages)
		case orchestration.NativeGatewayVolume:
			out[r.ID] = b.binding(r.ID, orchestration.GatewayDatabaseTarget, b.previewVolume, b.applyVolume)
		case orchestration.NativeGatewayInstance:
			out[r.ID] = b.binding(r.ID, orchestration.GatewayPostgresTarget, b.previewInstances, b.applyInstances)
		}
	}
	return out, nil
}

type acceptedGatewayBinding struct {
	deps     AcceptedGatewayDependencies
	revision environment.AcceptedRevision
	desc     environment.Descriptor
	top      NativeGatewayTopology
	devices  NativeGatewayDevices
	pair     NativeGatewayInstancePair
	specs    [2]environment.GatewayImageSpec
	fresh    [2]native.GatewayImageEvidence // verified acquisition + alias in this invocation only
}

var gatewayRoles = [2]string{orchestration.GatewayPostgresTarget, orchestration.GatewayLiteLLMTarget}
var gatewayAliasRoles = [2]string{orchestration.GatewayPostgresAliasTarget, orchestration.GatewayLiteLLMAliasTarget}

func (b *acceptedGatewayBinding) binding(id, noop string, preview func(context.Context) (orchestration.NativePreview, error), apply func(context.Context, orchestration.RunNativeEffect) error) orchestration.NativeResourceBinding {
	return orchestration.NativeResourceBinding{
		Validate: func() error { return nil },
		Preview: func(ctx context.Context) (orchestration.NativePreview, error) {
			p, err := preview(ctx)
			if err == nil && !p.Deferred && len(p.Actions) == 0 {
				p.Actions = []orchestration.NativeEffect{{Action: native.Unchanged, Target: noop}}
			}
			return p, err
		}, Apply: apply,
		Conditions: orchestration.StepConditions{Scope: orchestration.TargetScope{Deployment: b.desc.ID, Instance: id, Target: id}, Postconditions: []orchestration.BoundCheck{{
			Check:            check.Check{ID: "verify-" + id, Claim: "verify accepted gateway infrastructure stage", Scope: "selected Incus project and environment", Effect: check.ReadOnly, Access: []check.Access{check.IncusAccess}, Required: true, Timeout: 15 * time.Second},
			AcceptedRevision: b.revision.ID,
			Evaluate: func(ctx context.Context) (check.Finding, error) {
				var p orchestration.NativePreview
				var err error
				if id == orchestration.GatewayImageStage {
					var pending [2]bool
					_, pending, err = b.observeImages(ctx)
					p.Deferred = pending[0] || pending[1]
				} else {
					p, err = preview(ctx)
				}
				f := check.Finding{TargetID: id, CheckID: "verify-" + id, ObservedAt: time.Now(), AcceptedRevision: b.revision.ID, Outcome: check.Unknown, Code: "gateway-state-unverified"}
				if err == nil && !p.Deferred && len(p.Actions) == 0 {
					f.Outcome = check.Passed
					f.Code = "gateway-state-verified"
				}
				return f, nil
			},
		}}},
	}
}

func gatewayCreateEffect(target string) orchestration.NativeEffect {
	return orchestration.NativeEffect{Action: native.Create, Target: target}
}

func (b *acceptedGatewayBinding) verifyServer(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := b.deps.Client.GetConnectionInfo()
	if err != nil || info == nil || info.Protocol != "incus" || !filepath.IsAbs(info.SocketPath) || info.Project != b.top.Project || !gatewayServerMatches(b.deps.Client, b.desc.Binding) {
		return errors.New("accepted gateway Incus server or project unverified")
	}
	scope, _ := incusscope.NewProjectScope(b.top.Project)
	project, _, err := b.deps.Client.GetProject(scope.Name)
	if err != nil || project == nil || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
		return errors.New("accepted gateway project policy unverified")
	}
	return ctx.Err()
}

func (b *acceptedGatewayBinding) imageIntent(i int) native.GatewayImageAcquireIntent {
	s := b.specs[i]
	return native.GatewayImageAcquireIntent{Project: b.top.Project, Image: native.GatewayOCIIdentity{Registry: s.Registry, Repository: s.Repository, ManifestDigest: s.ManifestDigest, ConfigDigest: s.ConfigDigest, IncusFingerprint: s.IncusFingerprint}}
}

func (b *acceptedGatewayBinding) observeImages(ctx context.Context) (NativeGatewayCreateImages, [2]bool, error) {
	var pending [2]bool
	var pins [2]NativeGatewayCreateImage
	if err := b.verifyServer(ctx); err != nil {
		return NativeGatewayCreateImages{}, pending, err
	}
	for i, s := range b.specs {
		want := b.imageIntent(i)
		if want.Validate() != nil {
			return NativeGatewayCreateImages{}, pending, errors.New("invalid gateway image identity")
		}
		alias, _, err := b.deps.Client.GetImageAlias(s.Alias)
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			if _, _, err := b.deps.Client.GetImage(s.IncusFingerprint); !api.StatusErrorCheck(err, http.StatusNotFound) {
				return NativeGatewayCreateImages{}, pending, errors.New("gateway image absent alias has existing or unverified fingerprint")
			}
			all, err := b.deps.Client.GetImagesAllProjectsWithFilter(nil)
			if err != nil || slices.ContainsFunc(all, func(img api.Image) bool { return img.Fingerprint == s.IncusFingerprint }) {
				return NativeGatewayCreateImages{}, pending, errors.New("gateway image daemon-wide absence unverified")
			}
			pending[i] = true
			continue
		}
		if err != nil || alias == nil || alias.Name != s.Alias || alias.Target != s.IncusFingerprint {
			return NativeGatewayCreateImages{}, pending, errors.New("gateway image alias is foreign or unverified")
		}
		evidence := b.fresh[i]
		if evidence == (native.GatewayImageEvidence{}) {
			evidence, err = b.deps.ExistingImageEvidence(ctx, b.revision, gatewayRoles[i])
			if err != nil {
				return NativeGatewayCreateImages{}, pending, errors.New("gateway prior image acquisition evidence unavailable")
			}
		}
		if err := native.ObserveGatewayImageReuseWithClient(ctx, want, s.Alias, evidence, b.deps.Client); err != nil {
			return NativeGatewayCreateImages{}, pending, err
		}
		pins[i] = NativeGatewayCreateImage{Expected: want.Image, Evidence: evidence}
	}
	return NativeGatewayCreateImages{Postgres: pins[0], LiteLLM: pins[1]}, pending, ctx.Err()
}

func (b *acceptedGatewayBinding) previewImages(ctx context.Context) (orchestration.NativePreview, error) {
	_, pending, err := b.observeImages(ctx)
	var p orchestration.NativePreview
	if err != nil {
		return p, err
	}
	for i, missing := range pending {
		if missing {
			if err := b.verifyRegistryImage(ctx, i); err != nil {
				return orchestration.NativePreview{}, err
			}
			p.Actions = append(p.Actions, gatewayCreateEffect(gatewayRoles[i]), gatewayCreateEffect(gatewayAliasRoles[i]))
		}
	}
	return p, ctx.Err()
}

func (b *acceptedGatewayBinding) verifyRegistryImage(ctx context.Context, i int) error {
	r, err := b.deps.Resolve(ctx, b.specs[i].Source())
	want := b.imageIntent(i).Image
	if err != nil || r.Registry != want.Registry || r.Repository != want.Repository || r.ManifestDigest != want.ManifestDigest || r.ConfigDigest != want.ConfigDigest || r.IncusFingerprint != want.IncusFingerprint {
		return errors.New("gateway registry metadata differs from accepted image")
	}
	return ctx.Err()
}

func (b *acceptedGatewayBinding) applyImages(ctx context.Context, run orchestration.RunNativeEffect) error {
	_, pending, err := b.observeImages(ctx)
	if err != nil {
		return err
	}
	for i, missing := range pending {
		if !missing {
			continue
		}
		if err := b.verifyRegistryImage(ctx, i); err != nil {
			return err
		}
		want := b.imageIntent(i)
		var acquired native.GatewayImageAcquireResult
		if err := run(gatewayCreateEffect(gatewayRoles[i]), func(ctx context.Context) error {
			if err := b.verifyServer(ctx); err != nil {
				return err
			}
			if _, _, err := b.deps.Client.GetImageAlias(b.specs[i].Alias); !api.StatusErrorCheck(err, http.StatusNotFound) {
				return errors.New("gateway acquisition alias no longer absent")
			}
			var err error
			acquired, err = native.AcquireGatewayImageWithClient(ctx, want, b.deps.Client)
			if err != nil || !acquired.Verified {
				return errors.New("gateway image acquisition unverified")
			}
			return nil
		}); err != nil {
			return err
		}
		alias := b.specs[i].Alias
		if err := run(gatewayCreateEffect(gatewayAliasRoles[i]), func(ctx context.Context) error {
			if err := b.verifyServer(ctx); err != nil {
				return err
			}
			result, err := native.CreateGatewayImageAliasWithClient(ctx, native.GatewayAliasCreateIntent{Project: want.Project, Image: want.Image, Alias: alias, Acquisition: acquired}, b.deps.Client)
			if err != nil || !result.Verified {
				return errors.New("gateway image alias creation unverified")
			}
			return nil
		}); err != nil {
			return err
		}
		b.fresh[i] = native.GatewayImageEvidence{Project: want.Project, Alias: alias, Identity: want.Image}
	}
	return ctx.Err()
}

func (b *acceptedGatewayBinding) inspectStopped(ctx context.Context) (NativeGatewayCreateImages, bool, [2]bool, bool, error) {
	var absent [2]bool
	images, pending, err := b.observeImages(ctx)
	if err != nil {
		return images, false, absent, false, err
	}
	volume, err := observeNativeGatewayAttachments(ctx, b.top, b.devices, b.deps.Client, true)
	if err != nil {
		return images, false, absent, false, err
	}
	for i, s := range [2]NativeGatewayInstanceSpec{b.pair.Postgres, b.pair.LiteLLM} {
		g, _, err := b.deps.Client.GetInstance(s.Name)
		if api.StatusErrorCheck(err, http.StatusNotFound) {
			absent[i] = true
			continue
		}
		if err != nil || pending[i] || !matchesNativeGatewayInstance(g, s, b.specs[i].IncusFingerprint) || g.Status != "Stopped" || g.Stateful ||
			(g.ExpandedDevices != nil && !equalNativeDevices(g.ExpandedDevices, s.Devices)) || i == 0 && !volume {
			return images, false, absent, false, errors.New("accepted stopped gateway guest or storage differs from observed state")
		}
		for _, config := range []map[string]string{g.Config, g.ExpandedConfig} {
			for key := range config {
				if strings.HasPrefix(key, "raw.") || strings.HasPrefix(key, "security.") {
					return images, false, absent, false, errors.New("accepted stopped gateway has unapproved instance options")
				}
			}
			for _, requirement := range NativeGatewayRuntimeRequirements() {
				if _, found := config["environment."+requirement.Key]; requirement.Sensitive && found {
					return images, false, absent, false, errors.New("accepted stopped gateway has credential-shaped instance configuration")
				}
			}
		}
	}
	return images, volume, absent, pending[0] || pending[1], ctx.Err()
}

func (b *acceptedGatewayBinding) previewVolume(ctx context.Context) (orchestration.NativePreview, error) {
	_, exists, _, pending, err := b.inspectStopped(ctx)
	p := orchestration.NativePreview{Deferred: pending}
	if err == nil && !exists {
		p.Actions = []orchestration.NativeEffect{gatewayCreateEffect(orchestration.GatewayDatabaseTarget)}
	}
	return p, err
}

func (b *acceptedGatewayBinding) applyVolume(ctx context.Context, run orchestration.RunNativeEffect) error {
	p, err := b.previewVolume(ctx)
	if err != nil {
		return err
	}
	if p.Deferred {
		return errors.New("gateway volume image prerequisite unverified")
	}
	if len(p.Actions) == 0 {
		return nil
	}
	return run(p.Actions[0], func(ctx context.Context) error {
		if p, err := b.previewVolume(ctx); err != nil || p.Deferred || len(p.Actions) != 1 {
			return errors.New("gateway volume creation precondition changed")
		}
		result, err := EnsureNativeEnvironmentGatewayVolumeWithClient(ctx, b.desc, b.desc.Binding, b.devices.Postgres["database"]["pool"], b.deps.Client, context.Context.Err)
		if err != nil || !result.Verified || !result.Created {
			return errors.New("gateway volume creation unverified")
		}
		return nil
	})
}

func (b *acceptedGatewayBinding) previewInstances(ctx context.Context) (orchestration.NativePreview, error) {
	_, volume, absent, pending, err := b.inspectStopped(ctx)
	p := orchestration.NativePreview{Deferred: pending || !volume}
	if err != nil {
		return orchestration.NativePreview{}, err
	}
	for i, missing := range absent {
		if missing {
			p.Actions = append(p.Actions, gatewayCreateEffect(gatewayRoles[i]))
		}
	}
	return p, nil
}

func (b *acceptedGatewayBinding) applyInstances(ctx context.Context, run orchestration.RunNativeEffect) error {
	images, volume, _, pending, err := b.inspectStopped(ctx)
	if err != nil {
		return err
	}
	if !volume || pending {
		return errors.New("gateway instance prerequisites unverified")
	}
	_, err = createStoppedGatewayEffects(ctx, b.top, b.devices, b.pair, images, b.deps.Client, func(name string, submit func(context.Context) error) error {
		i := 0
		switch name {
		case b.pair.Postgres.Name:
		case b.pair.LiteLLM.Name:
			i = 1
		default:
			return errors.New("unexpected gateway instance target")
		}
		return run(gatewayCreateEffect(gatewayRoles[i]), func(ctx context.Context) error {
			_, volume, absent, pending, err := b.inspectStopped(ctx)
			if err != nil || !volume || pending || !absent[i] || i == 1 && absent[0] {
				return errors.New("gateway instance submission prerequisites changed")
			}
			if err := submit(ctx); err != nil {
				return errors.New("gateway stopped instance submission unverified")
			}
			return nil
		})
	})
	return err
}
