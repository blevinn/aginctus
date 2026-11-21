package testenv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/blevinn/aginctus/internal/filestate"
)

const (
	DisposableWorkerAcknowledgement = "DEDICATED_DISPOSABLE_WORKER"
	acceptanceMarker                = "/etc/aginctus-testenv/disposable-worker"
	acceptanceMarkerContent         = "aginctus-dedicated-disposable-worker-v1\n"
)

// IncusAcceptanceOptions are operator inputs, never CLI/MCP agent arguments.
// The marker and acknowledgement do not establish host isolation by themselves.
type IncusAcceptanceOptions struct {
	Acknowledgement  string
	SocketPath       string
	Project          string
	ImageFingerprint string
	PolicyKeyPath    string
	ReportRoot       string
	ForeignInstance  string
	Revision         string
}

type IncusAcceptanceCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// IncusAcceptanceReport follows the existing schema_version/status/results and
// disposable_guest_no_live_provider evidence conventions, not hermetic proof.
// Handles stay in the private result file; guest output and daemon errors do not.
type IncusAcceptanceReport struct {
	SchemaVersion    int                    `json:"schema_version"`
	Profile          string                 `json:"profile"`
	Tier             string                 `json:"evidence_tier"`
	RunID            string                 `json:"run_id"`
	CreatedUTC       time.Time              `json:"created_utc"`
	Revision         string                 `json:"revision"`
	ImageFingerprint string                 `json:"image_fingerprint"`
	Project          string                 `json:"project"`
	Status           string                 `json:"status"`
	FailureStage     string                 `json:"failure_stage,omitempty"`
	Results          []IncusAcceptanceCheck `json:"results"`
	Handles          []VMRef                `json:"handles"`
}

func (cfg IncusAcceptanceOptions) validate() error {
	if cfg.Acknowledgement != DisposableWorkerAcknowledgement ||
		cfg.SocketPath != "/var/lib/incus/unix.socket" || cfg.Project != DefaultVMProject ||
		!vmDigest.MatchString(cfg.ImageFingerprint) ||
		!vmSafeName.MatchString(cfg.ForeignInstance) || strings.HasPrefix(cfg.ForeignInstance, "testenv-") ||
		!filepath.IsAbs(cfg.PolicyKeyPath) || filepath.Clean(cfg.PolicyKeyPath) != cfg.PolicyKeyPath ||
		!filepath.IsAbs(cfg.ReportRoot) || filepath.Clean(cfg.ReportRoot) != cfg.ReportRoot {
		return errors.New("invalid dedicated worker acceptance configuration")
	}
	return nil
}

func checkDisposableWorkerMarker(path string) error {
	// The fixed parent and file must be root-controlled, not an agent's marker.
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0022 != 0 {
		return errors.New("dedicated worker marker parent unavailable")
	}
	stat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return errors.New("dedicated worker marker parent untrusted")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return errors.New("dedicated worker marker unavailable")
	}
	f := os.NewFile(uintptr(fd), "dedicated worker marker")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() != int64(len(acceptanceMarkerContent)) {
		return errors.New("dedicated worker marker unsafe")
	}
	stat, ok = info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Nlink != 1 {
		return errors.New("dedicated worker marker untrusted")
	}
	data := make([]byte, len(acceptanceMarkerContent)+1)
	n, err := f.Read(data)
	if err != nil || string(data[:n]) != acceptanceMarkerContent {
		return errors.New("dedicated worker marker invalid")
	}
	return nil
}

// RunTrustedIncusAcceptance is an explicit operator-only real-daemon entrypoint.
// Nothing calls it from normal checks, services, the public CLI or MCP.
func RunTrustedIncusAcceptance(ctx context.Context, cfg IncusAcceptanceOptions) (IncusAcceptanceReport, error) {
	var report IncusAcceptanceReport
	if err := cfg.validate(); err != nil {
		return report, err
	}
	if os.Geteuid() == 0 {
		return report, errors.New("acceptance requires a dedicated non-root worker")
	}
	if err := checkDisposableWorkerMarker(acceptanceMarker); err != nil {
		return report, err
	}
	key, err := readTrustedWorkerKey(cfg.PolicyKeyPath)
	if err != nil {
		return report, err
	}
	defer clear(key)
	// Trusted ancestors and same-UID writers remain the operator's boundary.
	info, err := os.Lstat(cfg.ReportRoot)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return report, errors.New("report root must be a private existing directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return report, errors.New("report root must be worker-owned")
	}
	lock, err := filestate.LockRegistry(filepath.Join(cfg.ReportRoot, "acceptance"))
	if err != nil {
		return report, err
	}
	defer lock.Close()
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return report, err
	}
	runID := hex.EncodeToString(token[:])
	dir := filepath.Join(cfg.ReportRoot, runID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return report, err
	}
	root, err := os.Open(cfg.ReportRoot)
	if err != nil {
		return report, err
	}
	err = root.Sync()
	root.Close()
	if err != nil {
		return report, err
	}
	save := func(r IncusAcceptanceReport) error {
		return saveIncusAcceptanceReport(dir, r)
	}
	report = newIncusAcceptanceReport(cfg, runID)
	if err := save(report); err != nil {
		return report, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	client, err := ConnectTrustedIncusVMClient(ctx, cfg.SocketPath)
	if err != nil {
		return report, errors.New("acceptance worker daemon unavailable")
	}
	backend, err := NewIncusVMBackend(client, "testenv", key)
	if err != nil {
		return report, err
	}
	defer clear(backend.policyKey)
	return runIncusAcceptance(ctx, backend, cfg, report, save, waitAcceptanceExpiry)
}

// dir is a freshly created private run directory under a validated worker root.
func saveIncusAcceptanceReport(dir string, report IncusAcceptanceReport) error {
	data, err := json.Marshal(report)
	if err != nil || len(data) >= 32*1024 {
		return errors.New("acceptance report encoding failed")
	}
	if err := filestate.WritePrivateFile(filepath.Join(dir, "result.json"), append(data, '\n')); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func newIncusAcceptanceReport(cfg IncusAcceptanceOptions, runID string) IncusAcceptanceReport {
	r := IncusAcceptanceReport{SchemaVersion: 1, Profile: "dedicated_worker", Tier: "disposable_guest_no_live_provider",
		RunID: runID, CreatedUTC: time.Now().UTC(), Revision: cfg.Revision,
		ImageFingerprint: cfg.ImageFingerprint, Project: cfg.Project, Status: "fail", FailureStage: "preflight"}
	for _, id := range []string{"project_policy", "ephemeral_lifecycle", "retained_lifecycle", "scoped_ttl_recovery", "root_volumes", "foreign_preserved", "instance_cleanup", "crash_reboot", "nested_isolation", "storage_reclamation", "report_retention"} {
		r.Results = append(r.Results, IncusAcceptanceCheck{ID: id, Status: "not_verified"})
	}
	return r
}

func (r *IncusAcceptanceReport) set(id, status string) {
	for i := range r.Results {
		if r.Results[i].ID == id {
			r.Results[i].Status = status
			return
		}
	}
}

func (r IncusAcceptanceReport) passed(id string) bool {
	for _, check := range r.Results {
		if check.ID == id {
			return check.Status == "pass"
		}
	}
	return false
}

type incusRootVolumeInventory interface {
	RootVolumePresent(context.Context, string, string) (bool, error)
}

func waitAcceptanceExpiry(ctx context.Context, expiry time.Time) error {
	timer := time.NewTimer(time.Until(expiry))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func cloneAcceptanceInstance(vm *IncusVMInstance) *IncusVMInstance {
	// Copy the adapter's subset in memory; never persist a foreign config or output.
	data, _ := json.Marshal(vm)
	var copy IncusVMInstance
	_ = json.Unmarshal(data, &copy)
	return &copy
}

func runIncusAcceptance(ctx context.Context, b *IncusVMBackend, cfg IncusAcceptanceOptions, r IncusAcceptanceReport,
	save func(IncusAcceptanceReport) error, wait func(context.Context, time.Time) error) (result IncusAcceptanceReport, retErr error) {
	result = r
	inventory, ok := b.client.(incusRootVolumeInventory)
	if !ok {
		return result, ErrUnsupportedAction
	}
	if err := b.checkProject(ctx); err != nil {
		return result, err
	}
	owned, err := b.ListOwned(ctx)
	if err != nil || len(owned) != 0 {
		return result, errors.New("acceptance requires no existing signed runs")
	}
	foreign, err := b.client.Get(ctx, cfg.ForeignInstance)
	if err != nil || foreign == nil || foreign.Type != "virtual-machine" || foreign.Name != cfg.ForeignInstance ||
		foreign.Config["user.aginctus.testenv.run"] != "" || foreign.Config[incusPolicyKey] != "" || foreign.Config[incusPolicyMACKey] != "" {
		return result, errors.New("acceptance requires an unsigned foreign VM sentinel")
	}
	baseline := cloneAcceptanceInstance(foreign)
	result.set("project_policy", "pass")
	defer func() {
		// Cleanup has its own budget even on cancellation, and only sealed handles.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		clean := true
		volumes := len(result.Handles) == 2
		for _, ref := range result.Handles {
			if err := b.Destroy(cleanupCtx, ref); err != nil {
				clean = false
			}
			present, err := inventory.RootVolumePresent(cleanupCtx, "testenv", ref.Instance)
			if err != nil || present {
				volumes = false
			}
		}
		if clean && len(result.Handles) > 0 {
			result.set("instance_cleanup", "pass")
		} else {
			result.set("instance_cleanup", "fail")
		}
		// Only claim absence after the volume was positively observed while alive.
		if volumes && result.passed("ephemeral_lifecycle") && result.passed("retained_lifecycle") {
			result.set("root_volumes", "pass")
		}
		after, err := b.client.Get(cleanupCtx, cfg.ForeignInstance)
		if err == nil && reflect.DeepEqual(baseline, after) {
			result.set("foreign_preserved", "pass")
		} else {
			result.set("foreign_preserved", "fail")
			result.FailureStage = "foreign_preserved"
			retErr = errors.New("foreign sentinel changed or unverifiable")
		}
		if !clean || !volumes {
			result.FailureStage = "cleanup"
			retErr = errors.New("acceptance cleanup incomplete or unverified")
		}
		if retErr == nil {
			result.Status = "pass"
			result.FailureStage = ""
		}
		if err := save(result); err != nil {
			result.Status = "fail"
			result.FailureStage = "evidence"
			retErr = errors.New("acceptance evidence persistence failed")
		}
	}()
	for _, mode := range []VMRetention{VMEphemeral, VMRetained} {
		result.FailureStage = string(mode) + "_create"
		spec := VMCreateSpec{Backend: IncusBackend, Project: DefaultVMProject, ImageFingerprint: cfg.ImageFingerprint,
			CPU: 2, MemoryMiB: 4096, RootDiskGiB: 32, TimeToLive: 3 * time.Minute, Retention: mode}
		ref, createErr := b.Create(ctx, spec)
		if ref.RunID != "" {
			result.Handles = append(result.Handles, ref)
		}
		if err := save(result); err != nil {
			return result, errors.New("acceptance handle persistence failed")
		}
		if createErr != nil {
			return result, errors.New("acceptance create unverified")
		}
		result.FailureStage = string(mode) + "_agent"
		if err := b.WaitForIncusVMAgent(ctx, ref, MaxIncusAgentReadiness); err != nil {
			return result, errors.New("acceptance agent readiness unverified")
		}
		result.FailureStage = string(mode) + "_status_exec_volume"
		state, err := b.Status(ctx, ref)
		if err != nil || state.State != "Running" {
			return result, errors.New("acceptance running status unverified")
		}
		out, err := b.Exec(ctx, ref, []string{"/run/current-system/sw/bin/incus", "--version"}, 10*time.Second)
		if err != nil || out.ExitCode != 0 || strings.TrimSpace(out.Stdout) == "" || out.Stderr != "" {
			return result, errors.New("acceptance guest exec unverified")
		}
		present, err := inventory.RootVolumePresent(ctx, "testenv", ref.Instance)
		if err != nil || !present {
			return result, errors.New("acceptance live root volume unverified")
		}
		// A copied signed ref cannot be retargeted at a foreign resource.
		forged := ref
		forged.Instance = cfg.ForeignInstance
		if err := b.Destroy(ctx, forged); !errors.Is(err, ErrOwnershipMismatch) {
			return result, errors.New("acceptance foreign handle rejection unverified")
		}
		if mode == VMEphemeral {
			result.FailureStage = "ephemeral_destroy"
			if err := b.Destroy(ctx, ref); err != nil {
				return result, errors.New("acceptance ephemeral destroy unverified")
			}
			result.set("ephemeral_lifecycle", "pass")
		} else {
			result.set("retained_lifecycle", "pass")
			result.FailureStage = "scoped_ttl_recovery"
			if err := wait(ctx, ref.Policy.ExpiresAt); err != nil {
				return result, err
			}
			// Use the same scoped expiry/ownership path as the reconciler, with
			// actual wall time; never sweep other runs or fabricate expiry.
			if b.now().Before(ref.Policy.ExpiresAt) {
				return result, errors.New("acceptance TTL has not elapsed")
			}
			if _, err := b.Status(ctx, ref); err != nil {
				return result, errors.New("acceptance retained VM absent or unverifiable before TTL destruction")
			}
			if err := b.destroy(ctx, ref, b.now()); err != nil {
				return result, errors.New("acceptance scoped TTL recovery unverified")
			}
			result.set("scoped_ttl_recovery", "pass")
		}
		if err := save(result); err != nil {
			return result, errors.New("acceptance evidence persistence failed")
		}
	}
	return result, nil
}
