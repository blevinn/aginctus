package testenv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
)

// IncusBootstrapContract is operator-owned, nonsecret input, not an agent API.
// ExistingInventoryFingerprint must come from independent inventory review,
// never from automatically trusting discovery output. See docs/testing/testenv/testenv-worker.md.
type IncusBootstrapContract struct {
	SchemaVersion                int                  `json:"schema_version"`
	ImageFingerprint             string               `json:"image_fingerprint"`
	Pool                         WorkerPoolInspection `json:"pool"`
	PoolConfigFingerprint        string               `json:"pool_config_fingerprint"`
	ExistingInventoryFingerprint string               `json:"existing_inventory_fingerprint"`
}

type IncusBootstrapProfile struct {
	Project string                       `json:"project"`
	Name    string                       `json:"name"`
	Config  map[string]string            `json:"config"`
	Devices map[string]map[string]string `json:"devices"`
}

type IncusBootstrapPool struct {
	Metadata WorkerPoolInspection `json:"metadata"`
	Status   string               `json:"status"`
	Total    uint64               `json:"total"`
	Used     uint64               `json:"used"`
}

// IncusBootstrapInventory describes a quiescent initial worker, not a running
// fleet. Complete includes all projects/pools/images/profiles/networks/volumes
// and instances. The default project is represented separately and must be
// empty; its default profile is included in Profiles. Assertions are untrusted
// offline input, not measurements or proof of runtime credential safety.
type IncusBootstrapInventory struct {
	Complete            bool                    `json:"complete"`
	DedicatedLocal      bool                    `json:"dedicated_local"`
	Socket              string                  `json:"socket"`
	DefaultProjectEmpty bool                    `json:"default_project_empty"`
	MarkerReady         bool                    `json:"marker_ready"`
	IdentityReady       bool                    `json:"identity_ready"`
	CredentialReady     bool                    `json:"credential_ready"`
	Projects            []IncusProjectPlan      `json:"projects"`
	Profiles            []IncusBootstrapProfile `json:"profiles"`
	Pools               []IncusBootstrapPool    `json:"pools"`
	Images              []WorkerImageInspection `json:"images"`
	Networks            []string                `json:"networks"`
	Volumes             []string                `json:"volumes"`
	Instances           []string                `json:"instances"`
}

type IncusBootstrapPlan struct {
	SchemaVersion int              `json:"schema_version"`
	Profile       string           `json:"profile"`
	Status        string           `json:"status"`
	Reason        string           `json:"reason,omitempty"`
	Project       IncusProjectPlan `json:"project"`
	Steps         []string         `json:"steps"`
	RequiredGates []string         `json:"required_gates"`
}

// BootstrapInventoryFingerprint hashes the complete typed inventory, including
// runtime pool status/capacity represented explicitly by IncusBootstrapPool.
// Array order is significant; a changed observation requires renewed review.
func BootstrapInventoryFingerprint(inventory IncusBootstrapInventory) (string, error) {
	return bootstrapDigest(inventory)
}

// BootstrapPoolConfigFingerprint uses the installed preflight's pool metadata
// encoding: name, driver and complete config, excluding runtime capacity.
func BootstrapPoolConfigFingerprint(pool WorkerPoolInspection) (string, error) {
	return bootstrapDigestBound(pool, 64*1024)
}

func bootstrapDigest(value any) (string, error) {
	return bootstrapDigestBound(value, 128*1024)
}

func bootstrapDigestBound(value any, limit int) (string, error) {
	data, err := json.Marshal(value)
	if err != nil || len(data) > limit {
		return "", errors.New("bootstrap metadata outside admitted bounds")
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// PlanIncusWorkerBootstrap is pure. Steps are operator obligations, never raw
// Incus requests or commands. A matching offline snapshot is not admission.
func PlanIncusWorkerBootstrap(c IncusBootstrapContract, i IncusBootstrapInventory) (IncusBootstrapPlan, error) {
	project, _ := IncusProjectPolicy("testenv")
	p := IncusBootstrapPlan{SchemaVersion: 1, Profile: "worker_bootstrap_offline", Status: "needs_operator_action",
		Project: project, Steps: []string{}, RequiredGates: []string{
			"independent_inventory_review", "installed_worker_preflight", "real_worker_lifecycle_acceptance",
			"independent_recovery_crash_reboot", "nested_device_network_isolation", "storage_and_report_reclamation",
		}}
	block := func(reason string) (IncusBootstrapPlan, error) {
		p.Reason = reason
		p.Steps = []string{}
		return p, errors.New("worker bootstrap needs operator action: " + reason)
	}
	poolHash, err := BootstrapPoolConfigFingerprint(c.Pool)
	if err != nil || c.SchemaVersion != 1 || !vmDigest.MatchString(c.ImageFingerprint) ||
		!vmDigest.MatchString(c.PoolConfigFingerprint) || poolHash != c.PoolConfigFingerprint ||
		c.Pool.Name != "testenv" || (c.Pool.Driver != "zfs" && c.Pool.Driver != "lvm") ||
		len(c.Pool.Config) == 0 || c.Pool.Config["source"] == "" ||
		(c.ExistingInventoryFingerprint != "" && !vmDigest.MatchString(c.ExistingInventoryFingerprint)) {
		return block("invalid_contract")
	}
	if !i.Complete || !i.DedicatedLocal || i.Socket != "/var/lib/incus/unix.socket" || !i.DefaultProjectEmpty {
		return block("dedicated_inventory_required")
	}
	if !i.MarkerReady || !i.IdentityReady || !i.CredentialReady {
		return block("runtime_prerequisites_required")
	}
	if len(i.Projects) > 1 || len(i.Pools) > 1 || len(i.Images) > 1 ||
		len(i.Networks) != 0 || len(i.Volumes) != 0 || len(i.Instances) != 0 {
		return block("unexpected_resources")
	}
	seen := map[string]bool{}
	for _, profile := range i.Profiles {
		if (profile.Project != "default" && profile.Project != DefaultVMProject) || profile.Name != "default" ||
			seen[profile.Project] || len(profile.Config) != 0 || len(profile.Devices) != 0 ||
			(profile.Project == DefaultVMProject && len(i.Projects) == 0) {
			return block("unsafe_profiles")
		}
		seen[profile.Project] = true
	}
	if !seen["default"] || (len(i.Projects) == 1 && !seen[DefaultVMProject]) {
		return block("incomplete_profiles")
	}
	if len(i.Projects) == 1 && !reflect.DeepEqual(i.Projects[0], project) {
		return block("project_drift")
	}
	if len(i.Pools) == 1 {
		pool := i.Pools[0]
		hash, err := BootstrapPoolConfigFingerprint(pool.Metadata)
		const gib = uint64(1024 * 1024 * 1024)
		if err != nil || hash != c.PoolConfigFingerprint || pool.Status != "Created" ||
			pool.Total < 64*gib || pool.Total > 80*gib || pool.Used > pool.Total {
			return block("pool_drift")
		}
	}
	if len(i.Images) == 1 {
		image := i.Images[0]
		if len(i.Projects) != 1 || image.Fingerprint != c.ImageFingerprint || image.Project != DefaultVMProject ||
			image.Type != "virtual-machine" || image.Architecture != "x86_64" || image.Public ||
			image.AutoUpdate || image.Cached || image.RemoteSource {
			return block("image_drift")
		}
	}
	hash, err := BootstrapInventoryFingerprint(i)
	if err != nil {
		return block("invalid_inventory")
	}
	if len(i.Projects)+len(i.Pools)+len(i.Images) != 0 && c.ExistingInventoryFingerprint == "" {
		return block("existing_resources_untrusted")
	}
	if c.ExistingInventoryFingerprint != "" && hash != c.ExistingInventoryFingerprint {
		return block("inventory_review_mismatch")
	}
	if len(i.Pools) == 0 {
		p.Steps = append(p.Steps, "operator_provision_reviewed_pool")
	}
	if len(i.Projects) == 0 {
		p.Steps = append(p.Steps, "operator_create_restricted_project_and_verify_empty_profile")
	}
	if len(i.Images) == 0 {
		p.Steps = append(p.Steps, "operator_import_reviewed_local_image_by_fingerprint")
	}
	if len(p.Steps) == 0 {
		p.Status = "offline_match"
	} else {
		p.Status = "plan_only"
	}
	return p, nil
}
