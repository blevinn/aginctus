package testenv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"
)

// WorkerPoolInspection is read-only daemon metadata, never a creation request.
// All pool configuration is included in the operator-reviewed fingerprint.
type WorkerPoolInspection struct {
	Name   string            `json:"name"`
	Driver string            `json:"driver"`
	Config map[string]string `json:"config"`
	Status string            `json:"-"`
	Total  uint64            `json:"-"`
	Used   uint64            `json:"-"`
}

type WorkerImageInspection struct {
	Fingerprint, Project, Type, Architecture string
	Public, AutoUpdate, Cached, RemoteSource bool
}

type incusWorkerInspector interface {
	InspectWorkerPool(context.Context) (WorkerPoolInspection, error)
	InspectWorkerImage(context.Context, string) (WorkerImageInspection, error)
}

type TrustedIncusPreflightOptions struct {
	SocketPath            string
	PolicyKeyPath         string
	ImageFingerprint      string
	PoolConfigFingerprint string
	// InspectPool prints a candidate fingerprint for independent operator review.
	// It never returns an admission pass or updates installed configuration.
	InspectPool bool
}

type IncusWorkerPreflightReport struct {
	SchemaVersion         int                    `json:"schema_version"`
	Profile               string                 `json:"profile"`
	Status                string                 `json:"status"`
	Results               []IncusAcceptanceCheck `json:"results"`
	PoolConfigFingerprint string                 `json:"pool_config_fingerprint,omitempty"`
}

func (cfg TrustedIncusPreflightOptions) validate() error {
	if cfg.SocketPath != "/var/lib/incus/unix.socket" || !vmDigest.MatchString(cfg.ImageFingerprint) ||
		(!cfg.InspectPool && !vmDigest.MatchString(cfg.PoolConfigFingerprint)) {
		return errors.New("invalid installed worker preflight configuration")
	}
	return nil
}

// RunTrustedIncusWorkerPreflight performs no daemon mutations or provisioning.
// Runtime options belong to the installed operator unit, not an agent checkout.
func RunTrustedIncusWorkerPreflight(ctx context.Context, cfg TrustedIncusPreflightOptions) (IncusWorkerPreflightReport, error) {
	var report IncusWorkerPreflightReport
	if err := cfg.validate(); err != nil {
		return report, err
	}
	if os.Geteuid() == 0 {
		return report, errors.New("worker preflight requires a dedicated non-root identity")
	}
	if err := checkDisposableWorkerMarker(acceptanceMarker); err != nil {
		return report, err
	}
	key, err := readTrustedWorkerKey(cfg.PolicyKeyPath)
	if err != nil {
		return report, err
	}
	defer clear(key)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := ConnectTrustedIncusVMClient(ctx, cfg.SocketPath)
	if err != nil {
		return report, errors.New("worker preflight daemon unavailable")
	}
	backend, err := NewIncusVMBackend(client, "testenv", key)
	if err != nil {
		return report, err
	}
	defer clear(backend.policyKey)
	return checkIncusWorkerPreflight(ctx, backend, cfg)
}

func checkIncusWorkerPreflight(ctx context.Context, backend *IncusVMBackend, cfg TrustedIncusPreflightOptions) (IncusWorkerPreflightReport, error) {
	r := IncusWorkerPreflightReport{SchemaVersion: 1, Profile: "worker_preflight", Status: "fail"}
	for _, id := range []string{"project_policy", "imported_vm_image", "reported_pool_capacity", "reviewed_pool_config", "resource_enforcement", "crash_reboot", "cleanup"} {
		r.Results = append(r.Results, IncusAcceptanceCheck{ID: id, Status: "not_verified"})
	}
	set := func(id, status string) {
		for i := range r.Results {
			if r.Results[i].ID == id {
				r.Results[i].Status = status
			}
		}
	}
	if err := cfg.validate(); err != nil {
		return r, err
	}
	if backend == nil || backend.client == nil {
		return r, ErrBackendUnavailable
	}
	inspector, ok := backend.client.(incusWorkerInspector)
	if !ok {
		return r, ErrUnsupportedAction
	}
	// One authority for the restricted project contract, shared with lifecycle.
	set("project_policy", "fail")
	if err := backend.checkProject(ctx); err != nil {
		return r, err
	}
	set("project_policy", "pass")
	set("imported_vm_image", "fail")
	image, err := inspector.InspectWorkerImage(ctx, cfg.ImageFingerprint)
	if err != nil || image.Fingerprint != cfg.ImageFingerprint || image.Project != DefaultVMProject ||
		image.Type != "virtual-machine" || image.Architecture != "x86_64" || image.Public || image.AutoUpdate || image.Cached || image.RemoteSource {
		return r, errors.New("worker imported image metadata unverified")
	}
	set("imported_vm_image", "pass")
	set("reported_pool_capacity", "fail")
	pool, err := inspector.InspectWorkerPool(ctx)
	if err != nil || pool.Name != "testenv" || pool.Status != "Created" ||
		(pool.Driver != "zfs" && pool.Driver != "lvm") {
		return r, errors.New("worker pool identity or driver unverified")
	}
	// This bounds a daemon-reported capacity snapshot, not effective quotas or
	// host filesystem isolation. Dedicated-worker acceptance must prove those.
	const gib = uint64(1024 * 1024 * 1024)
	if pool.Total < 64*gib || pool.Total > 80*gib || pool.Used > pool.Total {
		return r, errors.New("worker pool reported capacity outside admitted range")
	}
	set("reported_pool_capacity", "pass")
	data, err := json.Marshal(pool)
	if err != nil || len(data) > 64*1024 {
		return r, errors.New("worker pool metadata outside admitted bounds")
	}
	digest := sha256.Sum256(data)
	r.PoolConfigFingerprint = hex.EncodeToString(digest[:])
	if cfg.InspectPool {
		r.Status = "inspection_only"
		return r, nil
	}
	set("reviewed_pool_config", "fail")
	if r.PoolConfigFingerprint != cfg.PoolConfigFingerprint {
		return r, errors.New("worker pool configuration differs from reviewed fingerprint")
	}
	set("reviewed_pool_config", "pass")
	if err := ctx.Err(); err != nil {
		return r, err
	}
	r.Status = "pass"
	return r, nil
}
