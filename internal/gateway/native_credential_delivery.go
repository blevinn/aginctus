package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const credentialDirectory = "/run/aginctus/gateway"
const reviewedPostgresEntrypoint = "48647812e68165e2943abe82a1a7cb8364d6e9f8b6b6aefc179868b66a327c46"

//go:embed guest/litellm_bootstrap.py
var gatewayBootstrap string

var credentialToken = regexp.MustCompile(`^[A-Za-z0-9_-]{32,255}$`)
var errCredentialDelivery = errors.New("native gateway credential delivery unverified; inspect stopped guests before retry")

// GatewayCredentialMaterial is intentionally opaque and has no JSON fields.
// The independently authorized caller must obtain it from its protected
// secret source. These shape checks do not establish entropy or provenance.
type GatewayCredentialMaterial struct {
	version  string
	password string
	master   string
	salt     string
}

func NewGatewayCredentialMaterial(version, password, master, salt string) (GatewayCredentialMaterial, error) {
	if !gatewaySecretVersion.MatchString(version) || !credentialToken.MatchString(password) ||
		!credentialToken.MatchString(master) || !credentialToken.MatchString(salt) ||
		password == master || password == salt || master == salt {
		return GatewayCredentialMaterial{}, errCredentialDelivery
	}
	return GatewayCredentialMaterial{version, password, master, salt}, nil
}

func (GatewayCredentialMaterial) String() string   { return "[redacted gateway credential material]" }
func (GatewayCredentialMaterial) GoString() string { return "[redacted gateway credential material]" }

// GatewayCredentialDeliveryReceipt is nonsecret readback evidence for one
// stopped incarnation. It is neither a startup grant nor proof of in-guest
// consumption. It intentionally excludes credential hashes and bytes.
type GatewayCredentialDeliveryReceipt struct {
	Revision          environment.AcceptedRevision `json:"revision"`
	PhysicalProject   string                       `json:"physicalProject"`
	Version           string                       `json:"version"`
	PostgresCreatedAt time.Time                    `json:"postgresCreatedAt"`
	LiteLLMCreatedAt  time.Time                    `json:"litellmCreatedAt"`
	Verified          bool                         `json:"verified"`
}

// StageStoppedGatewayCredentialFiles uses a concrete Incus file transport.
// The caller must independently verify the accepted revision, physical binding,
// image provenance, secret source and sole-writer fence; guard rechecks those
// capabilities immediately before each submission. This first delivery
// candidate is exercised only by the disposable OCI scenario. It is not an
// installed privileged endpoint, cannot start guests, and refuses existing
// destinations rather than overwriting uncertain partial delivery.
//
// OCI metadata must be independently resolved for both immutable image pins.
// Root-run LiteLLM is the first supported bootstrap contract; other image
// runtime identities require a separate reviewed UID/GID binding.
func StageStoppedGatewayCredentialFiles(ctx context.Context, revision environment.AcceptedRevision, candidate environment.Candidate,
	top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair,
	images NativeGatewayCreateImages, pg, llm nativeoci.ResolvedImage,
	material GatewayCredentialMaterial, sdk incus.InstanceServer, guard func(context.Context) error,
) (GatewayCredentialDeliveryReceipt, error) {
	var out GatewayCredentialDeliveryReceipt
	fail := func(stage string) (GatewayCredentialDeliveryReceipt, error) {
		return out, fmt.Errorf("%s: %w", stage, errCredentialDelivery)
	}
	if ctx == nil || ctx.Err() != nil || sdk == nil || guard == nil ||
		revision.ValidateAgainst(candidate) != nil || candidate.Gateway == nil || revision.EnvironmentID != top.EnvironmentID ||
		candidate.Gateway.CredentialVersion != material.version || candidate.Gateway.ManagementNetwork != top.ManagementNetwork ||
		candidate.Gateway.EgressNetwork != top.EgressNetwork || candidate.Gateway.RootPool != devices.Postgres["root"]["pool"] ||
		candidate.Gateway.DatabasePool != devices.Postgres["database"]["pool"] ||
		!gatewaySecretVersion.MatchString(material.version) || !credentialToken.MatchString(material.password) ||
		!credentialToken.MatchString(material.master) || !credentialToken.MatchString(material.salt) {
		return fail("accepted input or writer guard")
	}
	expected, err := PlanNativeGatewayInstances(top, devices, pair.Postgres.ImageAlias, pair.LiteLLM.ImageAlias)
	if err != nil || !sameGatewayPair(pair, expected) {
		return fail("stopped pair plan")
	}
	for i, resolved := range []nativeoci.ResolvedImage{pg, llm} {
		want := []NativeGatewayCreateImage{images.Postgres, images.LiteLLM}[i].Expected
		accepted := []environment.GatewayImageSpec{candidate.Gateway.Postgres, candidate.Gateway.LiteLLM}[i]
		if accepted.Registry != want.Registry || accepted.Repository != want.Repository || accepted.ManifestDigest != want.ManifestDigest ||
			accepted.ConfigDigest != want.ConfigDigest || accepted.IncusFingerprint != want.IncusFingerprint ||
			accepted.Alias != []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM}[i].ImageAlias {
			return fail("accepted image pin")
		}
		if resolved.Registry != want.Registry || resolved.Repository != want.Repository ||
			resolved.ManifestDigest != want.ManifestDigest || resolved.ConfigDigest != want.ConfigDigest ||
			resolved.IncusFingerprint != want.IncusFingerprint {
			return fail("resolved image pin")
		}
	}
	if !slices.Equal(pg.Config.Entrypoint, []string{"docker-entrypoint.sh"}) ||
		!slices.Equal(pg.Config.Cmd, []string{"postgres"}) || pg.Config.User != "" ||
		!slices.Contains(pg.Config.Env, "PG_MAJOR=17") ||
		!slices.Contains(pg.Config.Env, "PGDATA=/var/lib/postgresql/data") {
		return fail("PostgreSQL image contract")
	}
	if llm.Config.User != "" && llm.Config.User != "0" && llm.Config.User != "0:0" {
		return fail("LiteLLM image contract")
	}
	specs := []NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM}
	pins := []NativeGatewayCreateImage{images.Postgres, images.LiteLLM}
	var incarnations [2]time.Time
	verify := func() error {
		if ctx.Err() != nil || guard(ctx) != nil ||
			ObserveNativeGatewayAttachmentsWithReader(ctx, top, devices, sdk) != nil {
			return errCredentialDelivery
		}
		server, _, err := sdk.GetServer()
		if err != nil || server == nil || server.Auth != "trusted" || server.Environment.ServerClustered ||
			server.Environment.Project != top.Project || "sha256:"+server.Environment.CertificateFingerprint != revision.ServerID {
			return errCredentialDelivery
		}
		observed, err := observePinnedGatewayInstances(ctx, top, pair, images, sdk)
		if err != nil || len(observed) != 2 || observed[0].Status != NativeGatewayCompatible || observed[1].Status != NativeGatewayCompatible {
			return errCredentialDelivery
		}
		for i, spec := range specs {
			instance, _, err := sdk.GetInstance(spec.Name)
			if err != nil || !matchesNativeGatewayInstance(instance, spec, pins[i].Expected.IncusFingerprint) ||
				instance.StatusCode != api.Stopped || instance.Stateful || instance.CreatedAt.IsZero() ||
				!equalNativeDevices(instance.ExpandedDevices, spec.Devices) {
				return errCredentialDelivery
			}
			for _, config := range []map[string]string{instance.Config, instance.ExpandedConfig} {
				for key := range config {
					if strings.HasPrefix(key, "environment.") || strings.HasPrefix(key, "raw.") || strings.HasPrefix(key, "security.") {
						return errCredentialDelivery
					}
				}
			}
			if incarnations[i].IsZero() {
				incarnations[i] = instance.CreatedAt
			} else if !incarnations[i].Equal(instance.CreatedAt) {
				return errCredentialDelivery
			}
		}
		return ctx.Err()
	}
	if verify() != nil {
		return fail("stopped guest preflight")
	}
	// Verify actual converted entrypoint bytes, not just a tag or image label.
	script, _, err := sdk.GetInstanceFile(pair.Postgres.Name, "/usr/local/bin/docker-entrypoint.sh")
	if err != nil || script == nil {
		return fail("PostgreSQL entrypoint observation")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(script, (32<<10)+1))
	closeErr := script.Close()
	if err != nil || closeErr != nil || n > 32<<10 || hex.EncodeToString(h.Sum(nil)) != reviewedPostgresEntrypoint {
		return fail("PostgreSQL entrypoint digest")
	}
	type entry struct {
		guest, path, kind, data string
		uid, gid                int64
		mode                    int
	}
	entries := []entry{
		{pair.Postgres.Name, "/run/aginctus", "directory", "", 0, 0, 0o755},
		{pair.Postgres.Name, credentialDirectory, "directory", "", 70, 70, 0o700},
		{pair.Postgres.Name, credentialDirectory + "/postgres-password", "file", material.password + "\n", 70, 70, 0o600},
		{pair.Postgres.Name, credentialDirectory + "/postgres.pgpass", "file", "127.0.0.1:5432:litellm:litellm:" + material.password + "\n", 70, 70, 0o600},
		{pair.LiteLLM.Name, "/run/aginctus", "directory", "", 0, 0, 0o755},
		{pair.LiteLLM.Name, credentialDirectory, "directory", "", 0, 0, 0o700},
		{pair.LiteLLM.Name, credentialDirectory + "/postgres-password", "file", material.password + "\n", 0, 0, 0o600},
		{pair.LiteLLM.Name, credentialDirectory + "/master-key", "file", material.master + "\n", 0, 0, 0o600},
		{pair.LiteLLM.Name, credentialDirectory + "/salt-key", "file", material.salt + "\n", 0, 0, 0o600},
		{pair.LiteLLM.Name, credentialDirectory + "/litellm-bootstrap.py", "file", gatewayBootstrap, 0, 0, 0o400},
	}
	absent := func(guest, path string) bool {
		body, _, err := sdk.GetInstanceFile(guest, path)
		if body != nil {
			_ = body.Close()
		}
		return api.StatusErrorCheck(err, http.StatusNotFound)
	}
	// Check the fixed parent on both stopped guests before writing anything.
	for _, spec := range specs {
		body, meta, err := sdk.GetInstanceFile(spec.Name, "/run")
		if body != nil {
			_ = body.Close()
		}
		if err != nil || meta == nil || meta.Type != "directory" || meta.UID != 0 || meta.Mode&0o022 != 0 ||
			!absent(spec.Name, "/run/aginctus") {
			return fail("guest parent preflight")
		}
	}
	readback := func(e entry) bool {
		body, meta, err := sdk.GetInstanceFile(e.guest, e.path)
		if body != nil {
			defer body.Close()
		}
		if err != nil || meta == nil || meta.Type != e.kind || int64(meta.UID) != e.uid ||
			int64(meta.GID) != e.gid || int(meta.Mode) != e.mode {
			return false
		}
		if e.kind == "directory" {
			return true
		}
		if body == nil {
			return false
		}
		data, err := io.ReadAll(io.LimitReader(body, int64(len(e.data))+1))
		return err == nil && subtle.ConstantTimeCompare(data, []byte(e.data)) == 1
	}
	for _, e := range entries {
		if verify() != nil || !absent(e.guest, e.path) {
			return fail("before guest file submission")
		}
		args := incus.InstanceFileArgs{UID: e.uid, GID: e.gid, Mode: e.mode, Type: e.kind, WriteMode: "overwrite"}
		if e.kind == "file" {
			args.Content = strings.NewReader(e.data)
		}
		if err := sdk.CreateInstanceFile(e.guest, e.path, args); err != nil {
			return fail("guest file submission")
		}
		if verify() != nil || !readback(e) {
			return fail("guest file readback")
		}
	}
	// Detect changes across the multi-file handoff. A failed partial stage is
	// not overwritten or automatically deleted on retry.
	for _, e := range entries {
		if !readback(e) {
			return fail("final guest file readback")
		}
	}
	if verify() != nil {
		return fail("final stopped guest preflight")
	}
	return GatewayCredentialDeliveryReceipt{Revision: revision, PhysicalProject: top.Project,
		Version: material.version, PostgresCreatedAt: incarnations[0], LiteLLMCreatedAt: incarnations[1], Verified: true}, nil
}
