package gateway

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

// NativeGatewayRemovalClient can delete a guest, but cannot stop it or delete
// its custom database volume, project, networks, or images.
type NativeGatewayRemovalClient interface {
	NativeGatewayStartupReader
	GetServer() (*api.Server, string, error)
	GetInstanceSnapshotNames(string) ([]string, error)
	GetInstanceBackupNames(string) ([]string, error)
	DeleteInstance(string) (native.InstanceOperation, error)
}

// NativeGatewayRemoval selects an exact stopped guest incarnation. The caller
// must establish that discarding its root filesystem is authorized, including
// any application/OAuth state; neither an empty disk list nor this intent proves it.
type NativeGatewayRemoval struct {
	Revision          environment.AcceptedRevision
	Instance          string
	InstanceUUID      string
	InstanceCreatedAt time.Time
}

type NativeGatewayRemovalResult struct {
	Instance               string `json:"instance"`
	Absent                 bool   `json:"absent"`
	DatabaseVolumeRetained bool   `json:"databaseVolumeRetained"`
}

type NativeGatewayRemovalError struct {
	Stage     string
	Attempted bool
	Uncertain bool
}

func (e *NativeGatewayRemovalError) Error() string {
	return "native gateway removal failed at " + e.Stage + "; inspect the selected guest and retained database volume before retry"
}

// RemoveNativeEnvironmentGatewayServiceWithClient deletes only the explicitly
// selected stopped guest. LiteLLM must be removed first. It does not accept a
// missing target as a successful retry. The trusted guard checks the accepted
// pin, exact target, root filesystem discard and exclusive writer authority.
// Incus delete has no ETag precondition: observations cannot fence other admins.
func RemoveNativeEnvironmentGatewayServiceWithClient(ctx context.Context, descriptor environment.Descriptor, selected environment.Binding,
	top NativeGatewayTopology, devices NativeGatewayDevices, pair NativeGatewayInstancePair, images NativeGatewayCreateImages,
	intent NativeGatewayRemoval, client NativeGatewayRemovalClient, guard func(context.Context, NativeGatewayRemoval) error,
) (NativeGatewayRemovalResult, error) {
	result := NativeGatewayRemovalResult{}
	fail := func(stage string, attempted bool) (NativeGatewayRemovalResult, error) {
		return result, &NativeGatewayRemovalError{Stage: stage, Attempted: attempted, Uncertain: attempted}
	}
	if guard == nil {
		return fail("authority", false)
	}
	id, err := NativeGatewayIdentity(descriptor, selected)
	if err != nil || top.Project != id.Project || top.EnvironmentID != id.EnvironmentID || top.DatabaseVolume != id.DatabaseVolume ||
		!gatewayRevisionMatches(intent.Revision, descriptor, selected) || !gatewayEnvironmentIDPattern.MatchString(intent.InstanceUUID) || intent.InstanceCreatedAt.IsZero() {
		return fail("intent", false)
	}
	expected, err := PlanNativeGatewayInstances(top, devices, pair.Postgres.ImageAlias, pair.LiteLLM.ImageAlias)
	if err != nil || !sameGatewayPair(pair, expected) {
		return fail("intent", false)
	}
	var target int
	switch intent.Instance {
	case id.PostgresInstance:
		target = 0
	case id.LiteLLMInstance:
		target = 1
	default:
		return fail("intent", false)
	}
	result.Instance = intent.Instance
	specs := [2]NativeGatewayInstanceSpec{pair.Postgres, pair.LiteLLM}
	pins := [2]NativeGatewayCreateImage{images.Postgres, images.LiteLLM}
	type inventory struct {
		guests [2]*api.Instance
		etags  [2]string
		volume *api.StorageVolume
	}
	inspect := func() (inventory, bool) {
		var inv inventory
		if ctx.Err() != nil || !gatewayServerMatches(client, selected) || ObserveNativeGatewayAttachmentsWithReader(ctx, top, devices, client) != nil {
			return inv, false
		}
		if _, err := observePinnedGatewayInstances(ctx, top, pair, images, client); err != nil {
			return inv, false
		}
		for i, spec := range specs {
			guest, etag, err := client.GetInstance(spec.Name)
			if api.StatusErrorCheck(err, http.StatusNotFound) {
				continue
			}
			if err != nil || etag == "" || !matchesNativeGatewayInstance(guest, spec, pins[i].Expected.IncusFingerprint) ||
				guest.Status != "Stopped" || guest.CreatedAt.IsZero() || !gatewayEnvironmentIDPattern.MatchString(guest.Config["volatile.uuid"]) || guest.Stateful ||
				(guest.ExpandedDevices != nil && !equalNativeDevices(guest.ExpandedDevices, spec.Devices)) {
				return inv, false
			}
			inv.guests[i], inv.etags[i] = guest, etag
		}
		v, _, err := client.GetStoragePoolVolume(specs[0].Devices["database"]["pool"], "custom", id.DatabaseVolume)
		if err != nil || verifyGatewayVolume(v, id.Project, id.EnvironmentID, id.DatabaseVolume, id.PostgresInstance) != nil || v.CreatedAt.IsZero() ||
			((inv.guests[0] != nil) != (len(v.UsedBy) == 1)) {
			return inv, false
		}
		copy := *v
		copy.Config = maps.Clone(v.Config)
		inv.volume = &copy
		return inv, ctx.Err() == nil
	}
	before, ok := inspect()
	if !ok || before.guests[target] == nil || !before.guests[target].CreatedAt.Equal(intent.InstanceCreatedAt) ||
		before.guests[target].Config["volatile.uuid"] != intent.InstanceUUID ||
		(target == 0 && before.guests[1] != nil) || (target == 1 && before.guests[0] == nil) ||
		!gatewayRootDiscardableConfig(before.guests[target]) {
		return fail("preflight", false)
	}
	if snapshots, err := client.GetInstanceSnapshotNames(intent.Instance); err != nil || len(snapshots) != 0 {
		return fail("snapshots", false)
	}
	if backups, err := client.GetInstanceBackupNames(intent.Instance); err != nil || len(backups) != 0 {
		return fail("backups", false)
	}
	sameGuest := func(first, second inventory, i int) bool {
		a, b := first.guests[i], second.guests[i]
		if a == nil || b == nil {
			return a == nil && b == nil
		}
		return a.CreatedAt.Equal(b.CreatedAt) && a.Config["volatile.uuid"] == b.Config["volatile.uuid"] && first.etags[i] == second.etags[i]
	}
	current, ok := inspect()
	if !ok || !sameGuest(before, current, 0) || !sameGuest(before, current, 1) ||
		!sameGatewayDatabaseVolume(before.volume, current.volume) || !gatewayRootDiscardableConfig(current.guests[target]) {
		return fail("mutation-boundary", false)
	}
	if err := guard(ctx, intent); err != nil || ctx.Err() != nil {
		return fail("authority", false)
	}
	op, err := client.DeleteInstance(intent.Instance)
	if err != nil || op == nil || op.WaitContext(ctx) != nil {
		return fail("delete", true)
	}
	after, ok := inspect()
	if !ok || after.guests[target] != nil || !sameGuest(before, after, 1-target) || !sameGatewayDatabaseVolume(before.volume, after.volume) {
		return fail("verification", true)
	}
	result.Absent, result.DatabaseVolumeRetained = true, true
	return result, nil
}

func sameGatewayDatabaseVolume(before, after *api.StorageVolume) bool {
	return before != nil && after != nil && !before.CreatedAt.IsZero() && before.CreatedAt.Equal(after.CreatedAt) &&
		before.Description == after.Description && maps.Equal(before.Config, after.Config)
}

func gatewayRootDiscardableConfig(instance *api.Instance) bool {
	for _, config := range []map[string]string{instance.Config, instance.ExpandedConfig} {
		for key := range config {
			switch key {
			case "user.aginctus.managed", "user.aginctus.resource", "user.aginctus.role", "user.aginctus.deployment", "user.aginctus.environment-id",
				// Incus materializes these four settings when creating from OCI.
				// Removal never interprets them or executes the guest entrypoint.
				"oci.entrypoint", "oci.cwd", "oci.uid", "oci.gid":
			default:
				if !strings.HasPrefix(key, "volatile.") && !strings.HasPrefix(key, "image.") {
					return false
				}
			}
		}
	}
	return true
}

func gatewayRevisionMatches(r environment.AcceptedRevision, desc environment.Descriptor, selected environment.Binding) bool {
	return r.ValidateIdentity() == nil && r.EnvironmentID == desc.ID && r.ServerID == selected.ServerID && r.Project == selected.Project
}

func gatewayServerMatches(client interface {
	GetServer() (*api.Server, string, error)
}, selected environment.Binding) bool {
	server, _, err := client.GetServer()
	return err == nil && server != nil && !server.Environment.ServerClustered && server.Environment.Project == selected.Project &&
		gatewayServerFingerprint.MatchString(server.Environment.CertificateFingerprint) && "sha256:"+server.Environment.CertificateFingerprint == selected.ServerID
}
