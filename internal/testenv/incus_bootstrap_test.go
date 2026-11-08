package testenv

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func bootstrapFixture(t *testing.T, existing bool) (IncusBootstrapContract, IncusBootstrapInventory) {
	t.Helper()
	c := IncusBootstrapContract{SchemaVersion: 1, ImageFingerprint: strings.Repeat("a", 64),
		Pool: WorkerPoolInspection{Name: "testenv", Driver: "zfs", Config: map[string]string{"source": "operator-approved-device"}}}
	c.PoolConfigFingerprint, _ = bootstrapDigest(c.Pool)
	i := IncusBootstrapInventory{Complete: true, DedicatedLocal: true, Socket: "/var/lib/incus/unix.socket",
		DefaultProjectEmpty: true, MarkerReady: true, IdentityReady: true, CredentialReady: true,
		Profiles: []IncusBootstrapProfile{{Project: "default", Name: "default"}}}
	if existing {
		project, _ := IncusProjectPolicy("testenv")
		i.Projects = []IncusProjectPlan{project}
		i.Profiles = append(i.Profiles, IncusBootstrapProfile{Project: DefaultVMProject, Name: "default"})
		i.Pools = []IncusBootstrapPool{{Metadata: c.Pool, Status: "Created", Total: 72 * 1024 * 1024 * 1024}}
		i.Images = []WorkerImageInspection{{Fingerprint: c.ImageFingerprint, Project: DefaultVMProject, Type: "virtual-machine", Architecture: "x86_64"}}
		c.ExistingInventoryFingerprint, _ = BootstrapInventoryFingerprint(i)
	}
	return c, i
}

func TestBootstrapPureDeterministicAndDerived(t *testing.T) {
	for _, existing := range []bool{false, true} {
		c, i := bootstrapFixture(t, existing)
		before, _ := json.Marshal(i)
		p, err := PlanIncusWorkerBootstrap(c, i)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := IncusProjectPolicy("testenv")
		if !reflect.DeepEqual(p.Project, want) || len(p.RequiredGates) != 6 {
			t.Fatal("policy or acceptance gates lost", p)
		}
		if existing && (p.Status != "offline_match" || len(p.Steps) != 0) {
			t.Fatal(p)
		}
		if !existing && (p.Status != "plan_only" || len(p.Steps) != 3) {
			t.Fatal(p)
		}
		again, _ := PlanIncusWorkerBootstrap(c, i)
		after, _ := json.Marshal(i)
		if !reflect.DeepEqual(p, again) || string(before) != string(after) {
			t.Fatal("nondeterminism or input mutation")
		}
		data, _ := json.Marshal(p)
		if strings.Contains(string(data), "operator-approved-device") {
			t.Fatal("raw inventory leaked")
		}
	}
}

func TestBootstrapFailsClosedEvenWithReviewedUnsafeState(t *testing.T) {
	changes := map[string]func(*IncusBootstrapContract, *IncusBootstrapInventory){
		"schema":      func(c *IncusBootstrapContract, _ *IncusBootstrapInventory) { c.SchemaVersion = 2 },
		"image-alias": func(c *IncusBootstrapContract, _ *IncusBootstrapInventory) { c.ImageFingerprint = "latest" },
		"image-short": func(c *IncusBootstrapContract, _ *IncusBootstrapInventory) { c.ImageFingerprint = "abcdef" },
		"image-uppercase": func(c *IncusBootstrapContract, _ *IncusBootstrapInventory) {
			c.ImageFingerprint = strings.Repeat("A", 64)
		},
		"wrong-pool-contract": func(c *IncusBootstrapContract, _ *IncusBootstrapInventory) { c.Pool.Name = "pilot" },
		"pool-pin": func(c *IncusBootstrapContract, _ *IncusBootstrapInventory) {
			c.PoolConfigFingerprint = strings.Repeat("b", 64)
		},
		"inventory-incomplete": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Complete = false },
		"not-dedicated":        func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.DedicatedLocal = false },
		"socket":               func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Socket = "/tmp/pilot.socket" },
		"default-project":      func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.DefaultProjectEmpty = false },
		"credential":           func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.CredentialReady = false },
		"identity":             func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.IdentityReady = false },
		"marker":               func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.MarkerReady = false },
		"foreign-project": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Projects = append(i.Projects, IncusProjectPlan{Name: "foreign"})
		},
		"project-limit": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Projects[0].Config["limits.cpu"] = "100"
		},
		"project-extra": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Projects[0].Config["features.images"] = "false"
		},
		"unsafe-network-policy": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Projects[0].Config["restricted.devices.nic"] = "allow"
		},
		"network":  func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Networks = []string{"bridge"} },
		"volume":   func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Volumes = []string{"foreign"} },
		"instance": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Instances = []string{"unsigned"} },
		"profile-nic": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Profiles[0].Devices = map[string]map[string]string{"eth0": {"type": "nic"}}
		},
		"profile-host-device": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Profiles[1].Devices = map[string]map[string]string{"host": {"type": "disk", "source": "/"}}
		},
		"profile-config": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Profiles[1].Config = map[string]string{"raw.qemu": ""}
		},
		"extra-profile": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Profiles = append(i.Profiles, IncusBootstrapProfile{Project: DefaultVMProject, Name: "unsafe"})
		},
		"missing-profile": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Profiles = nil },
		"duplicate-profile": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Profiles = append(i.Profiles, i.Profiles[0])
		},
		"wrong-pool": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Pools[0].Metadata.Name = "pilot" },
		"pool-source": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Pools[0].Metadata.Config = map[string]string{"source": "unreviewed"}
		},
		"pool-driver": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Pools[0].Metadata.Driver = "dir" },
		"pool-status": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Pools[0].Status = "Pending" },
		"pool-small":  func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Pools[0].Total = 1 },
		"pool-large": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Pools[0].Total = 81 * 1024 * 1024 * 1024
		},
		"pool-used":   func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Pools[0].Used = i.Pools[0].Total + 1 },
		"extra-image": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Images = append(i.Images, i.Images[0]) },
		"image-mismatch": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) {
			i.Images[0].Fingerprint = strings.Repeat("b", 64)
		},
		"image-project": func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Images[0].Project = "default" },
		"image-type":    func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Images[0].Type = "container" },
		"image-arch":    func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Images[0].Architecture = "aarch64" },
		"image-public":  func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Images[0].Public = true },
		"image-update":  func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Images[0].AutoUpdate = true },
		"image-cached":  func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Images[0].Cached = true },
		"image-remote":  func(_ *IncusBootstrapContract, i *IncusBootstrapInventory) { i.Images[0].RemoteSource = true },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			c, i := bootstrapFixture(t, true)
			change(&c, &i)
			// Even independent operator approval must not override safety checks.
			c.ExistingInventoryFingerprint, _ = BootstrapInventoryFingerprint(i)
			p, err := PlanIncusWorkerBootstrap(c, i)
			if err == nil || p.Status != "needs_operator_action" || len(p.Steps) != 0 {
				t.Fatal("accepted unsafe state or emitted partial actions", p, err)
			}
		})
	}
}

func TestBootstrapExistingResourcesRequireSeparateReview(t *testing.T) {
	for _, review := range []string{"", strings.Repeat("0", 64), "invalid"} {
		c, i := bootstrapFixture(t, true)
		c.ExistingInventoryFingerprint = review
		if p, err := PlanIncusWorkerBootstrap(c, i); err == nil || p.Status != "needs_operator_action" {
			t.Fatal("silently adopted matching existing state", p, err)
		}
	}
	c, i := bootstrapFixture(t, true)
	i.Pools[0].Used++
	if _, err := PlanIncusWorkerBootstrap(c, i); err == nil {
		t.Fatal("runtime inventory change lost from review hash")
	}
	i.Profiles[0].Config = map[string]string{"unbounded": strings.Repeat("x", 129*1024)}
	if _, err := BootstrapInventoryFingerprint(i); err == nil {
		t.Fatal("unbounded inventory")
	}
}
