package gateway

import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
)

const protectedGatewaySocket = "/var/lib/incus/unix.socket"

var errProtectedGateway = errors.New("protected gateway placement unverified")

// ProtectedGatewayPreview is a point-in-time administrative observation.
// It grants neither a writer pin nor credential delivery or runtime authority.
type ProtectedGatewayPreview struct {
	Revision environment.AcceptedRevision `json:"revision"`
	Physical environment.Binding `json:"physical"`
	Resources NativeGatewayResourceIdentity `json:"resources"`
	DatabasePresent bool `json:"databasePresent"`
	Instances []NativeGatewayInstanceObservation `json:"instances"`
	WriterPin bool `json:"writerPin"`
	ImageProvenance string `json:"imageProvenance"`
	Runtime string `json:"runtime"`
}

type protectedGatewayReader interface {
	NativeGatewayStartupReader
	GetServer() (*api.Server, string, error)
}

// protectedGatewayPlan retains both scopes without inventing a second accepted
// revision or an enrollment descriptor for the physical project.
type protectedGatewayPlan struct {
	top NativeGatewayTopology
	devices NativeGatewayDevices
	pair NativeGatewayInstancePair
}

func planProtectedGateway(g environment.ProtectedGatewayGrant, r environment.AcceptedRevision, c environment.Candidate) (protectedGatewayPlan, error) {
	if g.VerifyCandidate(r, c) != nil {
		return protectedGatewayPlan{}, errProtectedGateway
	}
	spec := c.Gateway
	ref := func(i environment.GatewayImageSpec) string { return i.Registry + "/" + i.Repository + "@" + i.ManifestDigest }
	s := Spec{ID: c.Environment.ID, Project: g.ProtectedProject, Network: spec.ManagementNetwork,
		EgressNetwork: spec.EgressNetwork, EgressIPv4Address: spec.EgressIPv4, PostgresImage: ref(spec.Postgres), LiteLLMImage: ref(spec.LiteLLM)}
	top, err := s.NativeTopology()
	if err != nil {
		return protectedGatewayPlan{}, errProtectedGateway
	}
	_, _, volume := environment.GatewayResourceNames(c.Environment.ID)
	top.EnvironmentID, top.DatabaseVolume = c.Environment.ID, volume
	top.Services[0].Volume = volume
	devices, err := PlanNativeGatewayDevices(top, spec.RootPool, spec.DatabasePool)
	if err != nil {
		return protectedGatewayPlan{}, errProtectedGateway
	}
	pair, err := PlanNativeGatewayInstances(top, devices, spec.Postgres.Alias, spec.LiteLLM.Alias)
	if err != nil {
		return protectedGatewayPlan{}, errProtectedGateway
	}
	return protectedGatewayPlan{top, devices, pair}, nil
}

func verifyProtectedGatewayProject(ctx context.Context, g environment.ProtectedGatewayGrant, c environment.Candidate, sdk protectedGatewayReader) error {
	if ctx == nil || ctx.Err() != nil || sdk == nil || c.Gateway == nil {
		return errProtectedGateway
	}
	conn, err := sdk.GetConnectionInfo()
	if err != nil || conn == nil || conn.Protocol != "incus" || conn.SocketPath != protectedGatewaySocket || conn.Project != g.ProtectedProject {
		return errProtectedGateway
	}
	server, _, err := sdk.GetServer()
	if err != nil || server == nil || server.Auth != "trusted" || server.Environment.ServerClustered ||
		server.Environment.Project != g.ProtectedProject || "sha256:"+server.Environment.CertificateFingerprint != g.ServerID {
		return errProtectedGateway
	}
	for _, name := range []string{g.Project, g.ProtectedProject} {
		p, etag, err := sdk.GetProject(name)
		scope, scopeErr := incusscope.NewProjectScope(name)
		if err != nil || p == nil || etag == "" || scopeErr != nil || p.Config["restricted"] != "true" ||
			scope.AssessProject(p.Name, p.Config) != incusscope.ProjectCompatible {
			return errProtectedGateway
		}
		if name == g.ProtectedProject {
			// A separate gateway-only project avoids handing a gateway principal
			// the state-service backing volume through project membership.
			if p.Config["user.aginctus.logical-project"] != g.Project ||
				p.Config["user.aginctus.protected-purpose"] != "gateway" ||
				p.Config["user.aginctus.environment-id"] != g.EnvironmentID ||
				p.Config["restricted.devices.disk"] != "managed" || p.Config["restricted.devices.nic"] != "managed" {
				return errProtectedGateway
			}
			networks := []string{c.Gateway.ManagementNetwork}
			if c.Gateway.EgressNetwork != "" {
				networks = append(networks, c.Gateway.EgressNetwork)
			}
			if !exactGatewayAllowlist(p.Config["restricted.networks.access"], networks) ||
				!exactGatewayAllowlist(p.Config["restricted.storage-pools.access"], []string{c.Gateway.RootPool, c.Gateway.DatabasePool}) {
				return errProtectedGateway
			}
		}
	}
	return ctx.Err()
}

func exactGatewayAllowlist(raw string, expected []string) bool {
	got := strings.Split(raw, ",")
	for i := range got {
		got[i] = strings.TrimSpace(got[i])
	}
	slices.Sort(got)
	want := slices.Clone(expected)
	slices.Sort(want)
	want = slices.Compact(want)
	return slices.Equal(got, want)
}

func observeProtectedGateway(ctx context.Context, g environment.ProtectedGatewayGrant, r environment.AcceptedRevision, c environment.Candidate, sdk protectedGatewayReader) (ProtectedGatewayPreview, error) {
	plan, err := planProtectedGateway(g, r, c)
	if err != nil || verifyProtectedGatewayProject(ctx, g, c, sdk) != nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	present, err := observeNativeGatewayAttachments(ctx, plan.top, plan.devices, sdk, true)
	if err != nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	preview := ProtectedGatewayPreview{Revision: r, Physical: environment.Binding{ServerID: g.ServerID, Project: g.ProtectedProject},
		Resources: NativeGatewayResourceIdentity{Project: g.ProtectedProject, EnvironmentID: g.EnvironmentID,
			PostgresInstance: plan.pair.Postgres.Name, LiteLLMInstance: plan.pair.LiteLLM.Name, DatabaseVolume: plan.top.DatabaseVolume},
		DatabasePresent: present, ImageProvenance: "not-verified", Runtime: "gated"}
	for i, instance := range []NativeGatewayInstanceSpec{plan.pair.Postgres, plan.pair.LiteLLM} {
		image := []environment.GatewayImageSpec{c.Gateway.Postgres, c.Gateway.LiteLLM}[i]
		got, _, err := sdk.GetInstance(instance.Name)
		state := NativeGatewayAbsent
		if !api.StatusErrorCheck(err, http.StatusNotFound) {
			if err != nil || !matchesNativeGatewayInstance(got, instance, image.IncusFingerprint) || got.CreatedAt.IsZero() ||
				got.Stateful || (got.Status != "Stopped" && got.Status != "Running") ||
				!equalNativeDevices(got.ExpandedDevices, instance.Devices) || i == 0 && !present {
				return ProtectedGatewayPreview{}, errProtectedGateway
			}
			for _, config := range []map[string]string{got.Config, got.ExpandedConfig} {
				for key := range config {
					if strings.HasPrefix(key, "raw.") || strings.HasPrefix(key, "security.") || strings.HasPrefix(key, "environment.") {
						return ProtectedGatewayPreview{}, errProtectedGateway
					}
				}
			}
			state = NativeGatewayCompatible
		}
		preview.Instances = append(preview.Instances, NativeGatewayInstanceObservation{Name: instance.Name, Project: g.ProtectedProject, Status: state})
	}
	if verifyProtectedGatewayProject(ctx, g, c, sdk) != nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	return preview, ctx.Err()
}

// PreviewLocalProtectedGateway is a direct root-only observation of the
// installed grant, actual authenticated logical snapshot and local daemon.
// Never expose it via an ordinary project endpoint or privileged proxy.
func PreviewLocalProtectedGateway(ctx context.Context, grantPath, operatorPath string) (ProtectedGatewayPreview, error) {
	if ctx == nil || ctx.Err() != nil || os.Getuid() != 0 || os.Geteuid() != 0 {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	grant, err := environment.ReadProtectedGatewayGrant(grantPath)
	if err != nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	reads, closeReads, err := environment.OpenProductionOperatorReads(ctx, operatorPath)
	if err != nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	defer closeReads()
	if reads.AcceptedSnapshot == nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	r, c, err := reads.AcceptedSnapshot.ReadAcceptedSnapshot(ctx, grant.Project, grant.EnvironmentID)
	if err != nil || grant.VerifyCandidate(r, c) != nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, protectedGatewaySocket, &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	defer server.Disconnect()
	result, err := observeProtectedGateway(ctx, grant, r, c, server.UseProject(grant.ProtectedProject))
	if err != nil {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	again, _, err := reads.AcceptedSnapshot.ReadAcceptedSnapshot(ctx, grant.Project, grant.EnvironmentID)
	current, grantErr := environment.ReadProtectedGatewayGrant(grantPath)
	if err != nil || again != r || grantErr != nil || current != grant {
		return ProtectedGatewayPreview{}, errProtectedGateway
	}
	return result, ctx.Err()
}
