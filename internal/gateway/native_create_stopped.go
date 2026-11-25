package gateway

import (
	"context"
	"fmt"
	"maps"
	"slices"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeGatewayStoppedCreateClient is the minimum SDK surface required to
// create, but never start, update or delete, gateway service containers.
type NativeGatewayStoppedCreateClient interface {
	NativeGatewayInstanceReader
	NativeGatewayAttachmentReader
	CreateInstance(api.InstancesPost) (native.InstanceCreateOperation, error)
}

type nativeGatewayStoppedSDK struct{ server incusclient.InstanceServer }

func (c nativeGatewayStoppedSDK) GetConnectionInfo() (*incusclient.ConnectionInfo, error) {
	return c.server.GetConnectionInfo()
}
func (c nativeGatewayStoppedSDK) GetProject(s string) (*api.Project, string, error) {
	return c.server.GetProject(s)
}
func (c nativeGatewayStoppedSDK) GetInstance(s string) (*api.Instance, string, error) {
	return c.server.GetInstance(s)
}
func (c nativeGatewayStoppedSDK) GetImageAlias(s string) (*api.ImageAliasesEntry, string, error) {
	return c.server.GetImageAlias(s)
}
func (c nativeGatewayStoppedSDK) GetImage(s string) (*api.Image, string, error) {
	return c.server.GetImage(s)
}
func (c nativeGatewayStoppedSDK) GetNetwork(s string) (*api.Network, string, error) {
	return c.server.GetNetwork(s)
}
func (c nativeGatewayStoppedSDK) GetStoragePool(s string) (*api.StoragePool, string, error) {
	return c.server.GetStoragePool(s)
}
func (c nativeGatewayStoppedSDK) GetStoragePoolVolume(p, k, n string) (*api.StorageVolume, string, error) {
	return c.server.GetStoragePoolVolume(p, k, n)
}
func (c nativeGatewayStoppedSDK) CreateInstance(p api.InstancesPost) (native.InstanceCreateOperation, error) {
	return c.server.CreateInstance(p)
}

// NativeGatewayStoppedResult preserves completed, verified service transitions
// when a subsequent operation fails. A submitted create may have succeeded even
// if its response or verification fails. No automatic rollback is attempted.
type NativeGatewayStoppedResult struct {
	Project  string                  `json:"project"`
	Plan     NativeGatewayCreatePlan `json:"plan"`
	Created  []string                `json:"created"`
	Verified []string                `json:"verified"`
}

type NativeGatewayStoppedError struct {
	Stage     string
	Service   string
	Attempted bool
	Uncertain bool
	cause     error
}

func (e *NativeGatewayStoppedError) Error() string {
	return "native gateway stopped creation failed at " + e.Stage + " for " + e.Service + "; inspect exact project resources before retry; no automatic cleanup"
}
func (e *NativeGatewayStoppedError) Unwrap() error { return e.cause }

// NativeGatewayCreateImage pairs the intended immutable identity with
// independent reuse evidence. The caller must verify the evidence's provenance
// and bind it to the accepted revision; these values do not convey authority.
type NativeGatewayCreateImage struct {
	Expected native.GatewayOCIIdentity
	Evidence native.GatewayImageEvidence
}

// NativeGatewayCreateImages supplies separate immutable pins for both services.
type NativeGatewayCreateImages struct {
	Postgres NativeGatewayCreateImage
	LiteLLM  NativeGatewayCreateImage
}

func observePinnedGatewayInstances(ctx context.Context, top NativeGatewayTopology, pair NativeGatewayInstancePair, images NativeGatewayCreateImages, reader NativeGatewayInstanceReader) ([]NativeGatewayInstanceObservation, error) {
	pins := [2]NativeGatewayCreateImage{images.Postgres, images.LiteLLM}
	return observeNativeGatewayInstances(ctx, pair, reader, func(i int, spec NativeGatewayInstanceSpec) (string, error) {
		pin := pins[i]
		if len(top.Services) != len(pins) || top.Services[i].ImageReference != pin.Expected.Registry+"/"+pin.Expected.Repository+"@"+pin.Expected.ManifestDigest {
			return "", fmt.Errorf("expected OCI image does not match pinned topology")
		}
		want := native.GatewayImageAcquireIntent{Project: spec.Project, Image: pin.Expected}
		if err := native.ObserveGatewayImageReuseWithClient(ctx, want, spec.ImageAlias, pin.Evidence, reader); err != nil {
			return "", fmt.Errorf("native gateway image identity unverified")
		}
		return pin.Expected.IncusFingerprint, nil
	})
}

// CreateStoppedGatewayWithClient intentionally has no local-connection
// wrapper or CLI activation. The caller must separately enforce accepted-state
// authority and hold exclusive writer fencing across observation and submission
// for project and host resources, including images and aliases. The guard checks
// that authority and fencing still hold immediately before each write; a nil
// guard forbids execution. Observations are not an atomic Incus precondition.
//
// No credentials, OCI commands or runtime environment are configured here.
// Each expected registry/repository@manifest must match the topology pin.
// Newly created services must be STOPPED. Retained services are left unchanged.
func CreateStoppedGatewayWithClient(ctx context.Context, top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair, images NativeGatewayCreateImages, client NativeGatewayStoppedCreateClient, guard func(context.Context) error) (NativeGatewayStoppedResult, error) {
	if guard == nil {
		return NativeGatewayStoppedResult{Project: top.Project}, &NativeGatewayStoppedError{Stage: "authority", Service: "gateway"}
	}
	return createStoppedGateway(ctx, top, devices, pair, images, client, func(ctx context.Context, _ string) error { return guard(ctx) })
}

func createStoppedGateway(ctx context.Context, top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair, images NativeGatewayCreateImages, client NativeGatewayStoppedCreateClient, guard func(context.Context, string) error) (NativeGatewayStoppedResult, error) {
	result := NativeGatewayStoppedResult{Project: top.Project}
	fail := func(stage, service string, attempted bool, cause error) (NativeGatewayStoppedResult, error) {
		return result, &NativeGatewayStoppedError{Stage: stage, Service: service, Attempted: attempted, Uncertain: attempted, cause: cause}
	}
	// Rebuild the pair rather than trusting caller-supplied names or attachments.
	original, err := PlanNativeGatewayInstances(top, devices, pair.Postgres.ImageAlias, pair.LiteLLM.ImageAlias)
	if err != nil || !sameGatewayPair(pair, original) {
		return fail("intent", "gateway", false, fmt.Errorf("native gateway pair not derived from validated topology"))
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", "gateway", false, err)
	}
	specs := [2]NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM}
	pins := [2]NativeGatewayCreateImage{images.Postgres, images.LiteLLM}
	for i, pin := range pins {
		expected := pin.Expected
		if top.Services[i].ImageReference != expected.Registry+"/"+expected.Repository+"@"+expected.ManifestDigest {
			return fail("intent", specs[i].Name, false, fmt.Errorf("expected OCI image does not match pinned topology"))
		}
	}
	observe := func() ([]NativeGatewayInstanceObservation, error) {
		return observePinnedGatewayInstances(ctx, top, pair, images, client)
	}
	if err := ObserveNativeGatewayAttachmentsWithReader(ctx, top, devices, client); err != nil {
		return fail("attachments", "gateway", false, err)
	}
	observed, err := observe()
	if err != nil {
		return fail("instances", "gateway", false, err)
	}
	plan, err := PlanNativeGatewayCreate(pair, observed)
	if err != nil {
		return fail("plan", "gateway", false, err)
	}
	result.Plan = plan
	for index, spec := range specs {
		step := plan.Steps[index]
		if step.Action == NativeGatewayRetainExisting {
			result.Verified = append(result.Verified, spec.Name)
			continue
		}
		if err := ObserveNativeGatewayAttachmentsWithReader(ctx, top, devices, client); err != nil {
			return fail("mutation-boundary-attachments", spec.Name, false, err)
		}
		current, err := observe()
		if err != nil {
			return fail("mutation-boundary-instances", spec.Name, false, err)
		}
		replanned, err := PlanNativeGatewayCreate(pair, current)
		if err != nil {
			return fail("mutation-boundary", spec.Name, false, err)
		}
		for j, currentStep := range replanned.Steps {
			want := plan.Steps[j].Action
			if j < index {
				want = NativeGatewayRetainExisting
			}
			if currentStep.Action != want {
				return fail("mutation-boundary", spec.Name, false, fmt.Errorf("gateway instance state changed before creation"))
			}
		}
		fingerprint := pins[index].Expected.IncusFingerprint
		// A service-specific marker keeps this resource distinct from
		// workload and historical Compose instances.
		request := api.InstancesPost{
			Name: spec.Name, Type: api.InstanceTypeContainer,
			Source: api.InstanceSource{Type: "image", Fingerprint: fingerprint},
			InstancePut: api.InstancePut{
				Description: "Aginctus native gateway " + spec.Role,
				Profiles:    []string{},
				Config: map[string]string{
					"user.aginctus.managed":        "true",
					"user.aginctus.resource":       "gateway-instance",
					"user.aginctus.role":           spec.Role,
					"user.aginctus.deployment":     spec.Project,
					"user.aginctus.environment-id": spec.EnvironmentID,
				},
				Devices: cloneGatewayDevices(spec.Devices),
			},
		}
		if err := ctx.Err(); err != nil {
			return fail("cancellation", spec.Name, false, err)
		}
		if err := guard(ctx, spec.Name); err != nil {
			return fail("authority", spec.Name, false, err)
		}
		if err := ctx.Err(); err != nil {
			return fail("cancellation", spec.Name, false, err)
		}
		op, err := client.CreateInstance(request)
		if err != nil {
			return fail("create", spec.Name, true, err)
		}
		result.Created = append(result.Created, spec.Name)
		if op == nil {
			return fail("operation", spec.Name, true, fmt.Errorf("instance operation missing"))
		}
		if err := op.WaitContext(ctx); err != nil {
			return fail("operation", spec.Name, true, err)
		}
		inst, _, err := client.GetInstance(spec.Name)
		if err != nil || !matchesNativeGatewayInstance(inst, spec, fingerprint) || inst.Status != "Stopped" {
			return fail("verification", spec.Name, true, fmt.Errorf("created gateway service postcondition unverified"))
		}
		if err := ctx.Err(); err != nil {
			return fail("verification", spec.Name, true, err)
		}
		result.Verified = append(result.Verified, spec.Name)
	}
	return result, nil
}

func cloneGatewayDevices(source map[string]map[string]string) map[string]map[string]string {
	output := make(map[string]map[string]string, len(source))
	for k, v := range source {
		output[k] = maps.Clone(v)
	}
	return output
}
func sameGatewayPair(a, b NativeGatewayInstancePair) bool {
	eq := func(x, y NativeGatewayInstanceSpec) bool {
		return x.Project == y.Project && x.EnvironmentID == y.EnvironmentID && x.Name == y.Name && x.Role == y.Role &&
			x.ImageAlias == y.ImageAlias && slices.Equal(x.DependsOn, y.DependsOn) &&
			slices.Equal(x.SecretEnvironment, y.SecretEnvironment) &&
			equalNativeDevices(x.Devices, y.Devices)
	}
	return eq(a.Postgres, b.Postgres) && eq(a.LiteLLM, b.LiteLLM)
}
