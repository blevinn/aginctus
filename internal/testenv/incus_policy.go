package testenv

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// This file describes an offline, declarative Incus launch plan. It never
// connects to Incus or performs host mutations. The trusted adapter must
// compare these policies with the actual server before any create/start.
var incusPoolName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// IncusProjectPlan is confined to a private worker daemon. These project
// limits are SUMS of configured instance values, not host hard disk quotas
// on every storage backend. Real worker acceptance is still mandatory.
type IncusProjectPlan struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
}

// IncusInstancePlan contains only safe, typed fields. Callers MUST NOT
// append raw QEMU options, host bind mounts, inherited host profiles,
// network devices or mutable image aliases.
type IncusInstancePlan struct {
	Project           string                       `json:"project"`
	Name              string                       `json:"name"`
	Type              string                       `json:"type"`
	ImageFingerprint  string                       `json:"image_fingerprint"`
	Profiles          []string                     `json:"profiles"`
	Config            map[string]string            `json:"config"`
	Devices           map[string]map[string]string `json:"devices"`
	ExpiresAt         time.Time                    `json:"expires_at"`
}

// IncusProjectPolicy produces an explicit allowlist, with no inherited
// network and no arbitrary host devices. The selected pool is trusted worker
// configuration; it is not a client-provided MCP argument.
func IncusProjectPolicy(storagePool string) (IncusProjectPlan, error) {
	if !incusPoolName.MatchString(storagePool) {
		return IncusProjectPlan{}, errors.New("testenv storage pool must be a simple explicit name")
	}
	return IncusProjectPlan{
		Name: DefaultVMProject,
		Config: map[string]string{
			"restricted":                         "true",
			"restricted.virtual-machines.lowlevel": "block",
			"restricted.virtual-machines.nesting":  "allow",
			"restricted.devices.nic":              "block",
			"restricted.devices.disk":             "block",
			"restricted.devices.pci":              "block",
			"restricted.devices.usb":              "block",
			"restricted.devices.unix-char":        "block",
			"restricted.devices.unix-block":       "block",
			"restricted.devices.proxy":            "block",
			"restricted.snapshots":                "block",
			"restricted.storage-pools.access":     storagePool,
			"features.profiles":                   "true",
			"features.networks":                   "false",
			"limits.containers":                   "0",
			"limits.virtual-machines":             "2",
			"limits.instances":                    "2",
			"limits.cpu":                          "4",
			"limits.memory":                       "10GiB",
			"limits.disk":                         "64GiB",
		},
	}, nil
}

// PlanIncusVM is pure: a trusted service creates the run ID and passes its
// authorized spec. Validation alone never authenticates an existing resource.
func PlanIncusVM(spec VMCreateSpec, ref VMRef, storagePool string, now time.Time) (IncusInstancePlan, error) {
	if err := spec.Validate(); err != nil {
		return IncusInstancePlan{}, err
	}
	if err := ref.Validate(); err != nil {
		return IncusInstancePlan{}, err
	}
	if ref.Backend != spec.Backend ||
		ref.Project != spec.Project ||
		ref.ImageFingerprint != spec.ImageFingerprint {
		return IncusInstancePlan{}, ErrOwnershipMismatch
	}
	if _, err := IncusProjectPolicy(storagePool); err != nil {
		return IncusInstancePlan{}, err
	}
	if now.IsZero() {
		return IncusInstancePlan{}, errors.New("trusted creation time is required")
	}
	// Use explicit empty profiles: never inherit a worker's default NIC or
	// host-share device. Incus adapter must verify resultant devices.
	return IncusInstancePlan{
		Project:          DefaultVMProject,
		Name:             ref.Instance,
		Type:             "virtual-machine",
		ImageFingerprint: ref.ImageFingerprint,
		Profiles:         []string{},
		Config: map[string]string{
			"limits.cpu":                 strconv.Itoa(spec.CPU),
			"limits.memory":              fmt.Sprintf("%dMiB", spec.MemoryMiB),
			"security.nesting":           "true",
			"user.aginctus.testenv.run": ref.RunID,
			"user.aginctus.testenv.image": ref.ImageFingerprint,
			"user.aginctus.testenv.expires": now.UTC().Add(spec.TimeToLive).Format(time.RFC3339Nano),
		},
		Devices: map[string]map[string]string{
			"root": {
				"type": "disk", "path": "/", "pool": storagePool,
				"size": fmt.Sprintf("%dGiB", spec.RootDiskGiB),
			},
		},
		ExpiresAt: now.UTC().Add(spec.TimeToLive),
	}, nil
}
