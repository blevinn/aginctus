package gateway

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
	"github.com/lxc/incus/v7/shared/api"
)

type removalFixtureClient struct {
	*environmentStartClient
	deletions            []string
	snapshots, backups   []string
	backupErr, deleteErr error
	afterDelete          func()
}

func (c *removalFixtureClient) GetInstanceSnapshotNames(string) ([]string, error) {
	return c.snapshots, nil
}
func (c *removalFixtureClient) GetInstanceBackupNames(string) ([]string, error) {
	return c.backups, c.backupErr
}
func (c *removalFixtureClient) DeleteInstance(name string) (native.InstanceOperation, error) {
	c.deletions = append(c.deletions, name)
	if c.deleteErr != nil {
		return nil, c.deleteErr
	}
	if c.instances[name].Config["user.aginctus.role"] == "postgres" {
		c.volume.UsedBy = nil
	}
	delete(c.instances, name)
	if c.afterDelete != nil {
		c.afterDelete()
	}
	return successfulGatewayCreateOperation{}, nil
}

func gatewayRemovalFixture(t *testing.T) (NativeGatewayTopology, NativeGatewayDevices, NativeGatewayInstancePair, *removalFixtureClient, NativeGatewayRemoval) {
	t.Helper()
	top, devices, pair, start, binding := environmentStartFixture(t)
	now := time.Unix(100, 0)
	start.instances[pair.Postgres.Name].CreatedAt = now
	start.instances[pair.Postgres.Name].Config["volatile.uuid"] = "123e4567-e89b-42d3-a456-426614174010"
	start.instances[pair.LiteLLM.Name].CreatedAt = now.Add(time.Second)
	start.instances[pair.LiteLLM.Name].Config["volatile.uuid"] = "123e4567-e89b-42d3-a456-426614174011"
	start.volume.CreatedAt = now.Add(-time.Second)
	start.volume.UsedBy = []string{"/1.0/instances/" + pair.Postgres.Name + "?project=" + top.Project}
	start.created = nil
	return top, devices, pair, &removalFixtureClient{environmentStartClient: start}, NativeGatewayRemoval{
		Revision: binding.AcceptedEnvironmentRevision, Instance: pair.LiteLLM.Name, InstanceCreatedAt: start.instances[pair.LiteLLM.Name].CreatedAt,
		InstanceUUID: start.instances[pair.LiteLLM.Name].Config["volatile.uuid"],
	}
}

func TestNativeGatewayRemovalRetainsVolumeAndEnforcesOrder(t *testing.T) {
	top, devices, pair, c, intent := gatewayRemovalFixture(t)
	desc := startFixtureDescriptor(top)
	guarded := []string{}
	remove := func(intent NativeGatewayRemoval) (NativeGatewayRemovalResult, error) {
		return RemoveNativeEnvironmentGatewayServiceWithClient(t.Context(), desc, desc.Binding, top, devices, pair, c.imagesForCreate, intent, c,
			func(_ context.Context, target NativeGatewayRemoval) error {
				guarded = append(guarded, target.Instance)
				return nil
			})
	}
	pg := intent
	pg.Instance, pg.InstanceCreatedAt = pair.Postgres.Name, c.instances[pair.Postgres.Name].CreatedAt
	pg.InstanceUUID = c.instances[pair.Postgres.Name].Config["volatile.uuid"]
	if _, err := remove(pg); err == nil || len(guarded) != 0 {
		t.Fatal("PostgreSQL removal permitted before LiteLLM")
	}
	for _, target := range []NativeGatewayRemoval{intent, pg} {
		got, err := remove(target)
		if err != nil || !got.Absent || !got.DatabaseVolumeRetained {
			t.Fatalf("remove %s: %+v %v", target.Instance, got, err)
		}
	}
	if _, err := remove(intent); err == nil {
		t.Fatal("blind retry reported success")
	}
	want := []string{intent.Instance, pg.Instance}
	if !slices.Equal(c.deletions, want) || !slices.Equal(guarded, want) || len(c.volume.UsedBy) != 0 {
		t.Fatalf("unexpected removal inventory: %v", c.deletions)
	}
}

func TestNativeGatewayRemovalRefusesUnsafeOrUnobservableState(t *testing.T) {
	for _, tc := range []string{"running", "snapshot", "backup", "unknown backups", "foreign", "protection", "instance changed", "volume attached elsewhere", "volume replaced", "rejected guard", "uncertain delete"} {
		t.Run(tc, func(t *testing.T) {
			top, devices, pair, c, intent := gatewayRemovalFixture(t)
			desc := startFixtureDescriptor(top)
			guest := c.instances[intent.Instance]
			secret := errors.New("synthetic opaque diagnostics")
			switch tc {
			case "running":
				guest.Status = "Running"
			case "snapshot":
				c.snapshots = []string{"untouchable"}
			case "backup":
				c.backups = []string{"untouchable"}
			case "unknown backups":
				c.backupErr = secret
			case "foreign":
				guest.Config["user.aginctus.environment-id"] = "123e4567-e89b-42d3-a456-426614174099"
			case "protection":
				guest.ExpandedConfig = map[string]string{"security.protection.delete": "true"}
			case "instance changed":
				guest.Config["volatile.uuid"] = "123e4567-e89b-42d3-a456-426614174012"
			case "volume attached elsewhere":
				c.volume.UsedBy = append(c.volume.UsedBy, "/1.0/profiles/default?project="+top.Project)
			case "volume replaced":
				c.afterDelete = func() { c.volume.CreatedAt = c.volume.CreatedAt.Add(time.Second) }
			case "uncertain delete":
				c.deleteErr = secret
			}
			got, err := RemoveNativeEnvironmentGatewayServiceWithClient(t.Context(), desc, desc.Binding, top, devices, pair, c.imagesForCreate, intent, c,
				func(context.Context, NativeGatewayRemoval) error {
					if tc == "rejected guard" {
						return secret
					}
					return nil
				})
			attempted := tc == "uncertain delete" || tc == "volume replaced"
			var failure *NativeGatewayRemovalError
			if !errors.As(err, &failure) || failure.Attempted != attempted || failure.Uncertain != attempted || got.Absent || got.DatabaseVolumeRetained ||
				(len(c.deletions) > 0) != attempted || strings.Contains(err.Error(), secret.Error()) {
				t.Fatalf("unsafe or misleading result: %+v %+v", got, failure)
			}
		})
	}
}

func TestNativeGatewayLiteLLMRecreationKeepsPostgresAndReportsInterruption(t *testing.T) {
	for _, tc := range []string{"replace", "bad replacement", "revoked after delete"} {
		t.Run(tc, func(t *testing.T) {
			top, devices, pair, c, intent := gatewayRemovalFixture(t)
			desc := startFixtureDescriptor(top)
			prior := environment.Candidate{APIVersion: environment.CandidateAPIVersion, Kind: environment.CandidateKind, Environment: desc,
				SelectedWorkloads: []string{}, Workloads: []acceptedstate.WorkloadSpec{}}
			candidate := prior
			candidate.Environment.Name = "successor-fixture"
			next, err := environment.BuildAcceptedRevision(&intent.Revision, &prior, candidate)
			if err != nil {
				t.Fatal(err)
			}
			replacement := c.imagesForCreate.LiteLLM
			replacement.Expected.ManifestDigest = "sha256:" + strings.Repeat("e", 64)
			replacement.Expected.IncusFingerprint = strings.Repeat("e", 64)
			replacement.Evidence.Identity = replacement.Expected
			replacement.Evidence.Alias = "new-llm-image"
			if tc != "bad replacement" {
				c.aliases[replacement.Evidence.Alias] = &api.ImageAliasesEntry{Name: replacement.Evidence.Alias, ImageAliasesEntryPut: api.ImageAliasesEntryPut{Target: replacement.Expected.IncusFingerprint}}
				c.images[replacement.Expected.IncusFingerprint] = &api.Image{Fingerprint: replacement.Expected.IncusFingerprint, Type: "container", Project: top.Project}
			}
			pg := c.instances[pair.Postgres.Name]
			var effects []native.Action
			got, err := RecreateNativeEnvironmentGatewayLiteLLMWithClient(t.Context(), desc, desc.Binding, top, devices, pair, c.imagesForCreate, intent, next, replacement, c,
				func(_ context.Context, old, new environment.AcceptedRevision, action native.Action, name string) error {
					if old != intent.Revision || new != next || name != pair.LiteLLM.Name {
						t.Fatal("unexpected authority target")
					}
					effects = append(effects, action)
					if action == native.Create && tc == "revoked after delete" {
						return errors.New("opaque authority denial")
					}
					return nil
				})
			if c.instances[pair.Postgres.Name] != pg || len(c.volume.UsedBy) != 1 {
				t.Fatal("recreation changed PostgreSQL")
			}
			switch tc {
			case "replace":
				llm := c.instances[pair.LiteLLM.Name]
				if err != nil || !got.Removal.DatabaseVolumeRetained || llm == nil || llm.Status != "Stopped" || llm.Config["volatile.base_image"] != replacement.Expected.IncusFingerprint ||
					!slices.Equal(c.created, []string{pair.LiteLLM.Name}) || !slices.Equal(effects, []native.Action{native.Delete, native.Create}) {
					t.Fatalf("stopped recreation: %+v %v", got, err)
				}
			case "bad replacement":
				if err == nil || len(c.deletions) != 0 || len(effects) != 0 {
					t.Fatal("deleted guest without verified replacement image")
				}
			case "revoked after delete":
				var fail *NativeGatewayStoppedError
				if !errors.As(err, &fail) || fail.Attempted || !got.Removal.Absent || !got.Removal.DatabaseVolumeRetained || c.instances[pair.LiteLLM.Name] != nil || len(c.created) != 0 {
					t.Fatalf("lost partial result: %+v %v", got, err)
				}
			}
		})
	}
}
