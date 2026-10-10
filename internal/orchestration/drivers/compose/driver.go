package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"

	composeclient "github.com/lxc/incus-compose/client"
	"github.com/lxc/incus-compose/iclient"
	composeproject "github.com/lxc/incus-compose/project"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration"
)

const (
	ownershipManagedKey    = "user.aginctus.managed"
	ownershipResourceKey   = "user.aginctus.resource"
	ownershipDeploymentKey = "user.aginctus.deployment"
)

type Config struct {
	Project         string          `json:"project"`
	Compose         json.RawMessage `json:"compose"`
	RecreateOnDrift bool            `json:"recreateOnDrift,omitempty"`
}

type Driver struct {
	execute     func(context.Context, Config, map[string]string) error
	environment map[string]string
}

var _ orchestration.Driver = (*Driver)(nil)

func New() *Driver {
	return &Driver{execute: executeCompose}
}

func NewWithEnvironment(environment map[string]string) *Driver {
	copied := make(map[string]string, len(environment))
	for key, value := range environment {
		copied[key] = value
	}
	return &Driver{execute: executeCompose, environment: copied}
}

func (d *Driver) SupportsDryRun() bool {
	return false
}

func (d *Driver) Validate(raw json.RawMessage) error {
	_, err := parseConfig(context.Background(), raw)
	return err
}

func (d *Driver) Execute(ctx context.Context, raw json.RawMessage, options orchestration.ExecuteOptions) error {
	if options.DryRun {
		return fmt.Errorf("compose driver does not support dry-run")
	}
	cfg, err := parseConfig(ctx, raw)
	if err != nil {
		return err
	}
	return d.execute(ctx, cfg, d.environment)
}

func parseConfig(ctx context.Context, raw json.RawMessage) (Config, error) {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode compose driver configuration: %w", err)
	}
	if cfg.Project == "" {
		return Config{}, fmt.Errorf("compose project must not be empty")
	}
	if len(bytes.TrimSpace(cfg.Compose)) == 0 || bytes.Equal(bytes.TrimSpace(cfg.Compose), []byte("null")) {
		return Config{}, fmt.Errorf("compose configuration must not be empty")
	}

	path, cleanup, err := writeCompose(cfg.Compose)
	if err != nil {
		return Config{}, err
	}
	defer cleanup()

	_, err = loadProject(ctx, cfg.Project, path, nil)
	if err != nil {
		return Config{}, fmt.Errorf("validate compose project %q: %w", cfg.Project, err)
	}
	return cfg, nil
}

func runComposeServicesSequentially(ctx context.Context, order []string, run func(string) error) error {
	for _, service := range order {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := run(service); err != nil {
			return err
		}
	}
	return nil
}

func newComposeGlobalClient(ctx context.Context, newConnection func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error)) (*composeclient.GlobalClient, error) {
	connection, err := newConnection(&iclient.ConfigRemoteInfo{
		Name:     "local",
		Addrs:    []string{"unix://"},
		Protocol: "incus",
	})
	if err != nil {
		return nil, err
	}
	return composeclient.New(ctx, composeclient.ClientProvideConnection(connection)), nil
}

// Image caching copies images between Incus projects using pull mode. Even when
// the primary connection uses the local Unix socket, Incus must offer HTTPS.
func explainComposeConnectionError(err error) error {
	if strings.Contains(err.Error(), "core.https_address is not set") {
		return fmt.Errorf("%w; configure an appropriately restricted Incus HTTPS listener with `incus config set core.https_address=<host-address>:8443` and verify it using `incus config get core.https_address` (setting a listener on all interfaces may expose the daemon; review firewall and trust configuration first)", err)
	}
	return err
}

func isComposePrerequisite(priority int) bool {
	return priority < composeclient.PriorityInstance
}

func verifyComposeProjectOwnership(config map[string]string, project string) error {
	expected := map[string]string{
		ownershipManagedKey:    "true",
		ownershipResourceKey:   "compose-project",
		ownershipDeploymentKey: project,
	}
	for key, want := range expected {
		if config[key] != want {
			return fmt.Errorf("compose project %q is not owned by this deployment: %s ownership marker missing or mismatched; refusing adoption", project, key)
		}
	}
	return nil
}

func verifyComposeInstanceOwnership(config map[string]string, project, instance string) error {
	expected := map[string]string{
		ownershipManagedKey:    "true",
		ownershipResourceKey:   "compose-instance",
		ownershipDeploymentKey: project,
	}
	for key, want := range expected {
		if config[key] != want {
			return fmt.Errorf("compose instance %q in project %q is not owned by this deployment: %s ownership marker missing or mismatched; refusing adoption", instance, project, key)
		}
	}
	return nil
}

// ComposeDriftKind classifies reconciliation decisions without exposing values.
type ComposeDriftKind string

const (
	ComposeDriftConfig   ComposeDriftKind = "config"
	ComposeDriftProfiles ComposeDriftKind = "profiles"
	ComposeDriftDevice   ComposeDriftKind = "device"
	ComposeDriftImage    ComposeDriftKind = "image"
)

// ComposeDriftError identifies a change that the pinned upstream Compose
// implementation cannot converge on an existing instance. Key names are
// safe to print; old and new configuration values are deliberately omitted.
type ComposeDriftError struct {
	Kind   ComposeDriftKind
	Key    string
	Reason string
}

func (e *ComposeDriftError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("compose %s drift at %q: %s", e.Kind, e.Key, e.Reason)
	}
	return fmt.Sprintf("compose %s drift: %s", e.Kind, e.Reason)
}

// detectComposeInstanceDrift rejects changes the pinned Compose Ensure would
// silently ignore. Missing extension keys are safe: Ensure adds them.
func detectComposeInstanceDrift(desired map[string]string, desiredProfiles []string, actual map[string]string, actualProfiles []string) error {
	for key, want := range desired {
		if got, exists := actual[key]; exists && got != want {
			return &ComposeDriftError{Kind: ComposeDriftConfig, Key: key, Reason: "instance recreation or an explicit update is required"}
		}
	}
	if desiredProfiles != nil && !reflect.DeepEqual(desiredProfiles, actualProfiles) {
		return &ComposeDriftError{Kind: ComposeDriftProfiles, Reason: "explicit reconciliation required"}
	}
	return nil
}

// detectComposeDeviceDrift only compares devices explicitly controlled by the
// desired Compose model. Extra host devices are left untouched.
func detectComposeDeviceDrift(desired, actual map[string]map[string]string) error {
	for name, desiredValues := range desired {
		current, exists := actual[name]
		if !exists {
			// The pinned upstream Ensure does not add missing raw devices to
			// existing instances, so absence is drift too.
			return &ComposeDriftError{Kind: ComposeDriftDevice, Key: name, Reason: "device missing; explicit instance reconciliation required"}
		}
		for key, want := range desiredValues {
			if got, ok := current[key]; !ok || got != want {
				return &ComposeDriftError{Kind: ComposeDriftDevice, Key: name + "." + key, Reason: "explicit instance reconciliation required"}
			}
		}
	}
	return nil
}

// A change to the source image alias is recreate-only with pinned incus-compose.
// Comparing resolved Incus names avoids treating equivalent normalized OCI
// references as different while preserving a fail-closed result on drift.
func detectComposeImageDrift(want, got string) error {
	if want == "" {
		return nil
	}
	if got == "" {
		return &ComposeDriftError{Kind: ComposeDriftImage, Reason: "missing recorded image alias; cannot verify convergence without recreation"}
	}
	if want != got {
		return &ComposeDriftError{Kind: ComposeDriftImage, Reason: "recreation required to apply changed image"}
	}
	return nil
}

// A mutable image alias can continue to name the same tag after its image
// has changed. The instance records the fingerprint it was launched from.
func detectComposeImageFingerprintDrift(desiredFingerprint, actualFingerprint string) error {
	if desiredFingerprint == "" || actualFingerprint == "" {
		return &ComposeDriftError{Kind: ComposeDriftImage, Reason: "missing base image fingerprint; explicit reconciliation required"}
	}
	if !strings.EqualFold(desiredFingerprint, actualFingerprint) {
		return &ComposeDriftError{Kind: ComposeDriftImage, Reason: "recreation required"}
	}
	return nil
}

// verifyComposeEnsuredImage is invoked after the image prerequisite pass,
// when an initially uncached alias must now be resolvable.
func verifyComposeEnsuredImage(imageAlias, cachedAlias, desiredFingerprint, instanceFingerprint string) error {
	if err := detectComposeImageDrift(imageAlias, cachedAlias); err != nil {
		return err
	}
	return detectComposeImageFingerprintDrift(desiredFingerprint, instanceFingerprint)
}

// explainComposeRecreationSafety retains the original typed drift error while
// checking that all attached persistent disks can be identified in advance.
func explainComposeRecreationSafety(err error, devices map[string]map[string]string) error {
	var drift *ComposeDriftError
	if !errors.As(err, &drift) {
		return err
	}
	plan, planErr := planComposeRecreation(drift, devices)
	if planErr != nil {
		return fmt.Errorf("%w; recreation safety preflight failed: %v", err, planErr)
	}
	return fmt.Errorf("%w; recreation requires preserving %d explicit storage volume attachment(s)", err, len(plan.PersistentDevices))
}

func executeCompose(ctx context.Context, cfg Config, environment map[string]string) error {
	path, cleanup, err := writeCompose(cfg.Compose)
	if err != nil {
		return err
	}
	defer cleanup()

	p, err := loadProject(ctx, cfg.Project, path, environment)
	if err != nil {
		return fmt.Errorf("load compose project %q: %w", cfg.Project, err)
	}

	global, err := newComposeGlobalClient(ctx, iclient.NewConnection)
	if err != nil {
		return fmt.Errorf("construct local Incus connection for Compose: %w", err)
	}
	if err := global.Connect(); err != nil {
		return fmt.Errorf("connect incus-compose client: %w", explainComposeConnectionError(err))
	}

	connection, err := global.Connection()
	if err != nil {
		return fmt.Errorf("inspect Compose project ownership: %w", err)
	}
	existing, _, err := connection.GetProject(ctx, composeclient.SanitizeProjectName(p.Name))
	if err == nil {
		if err := verifyComposeProjectOwnership(existing.Config, p.Name); err != nil {
			return err
		}
		instances, listErr := connection.GetInstances(ctx, composeclient.SanitizeProjectName(p.Name), nil)
		if listErr != nil {
			return fmt.Errorf("inspect Compose instances in project %q: %w", p.Name, listErr)
		}
		for _, instance := range instances {
			if err := verifyComposeInstanceOwnership(instance.Config, p.Name, instance.Name); err != nil {
				return err
			}
		}
	} else if !api.StatusErrorCheck(err, http.StatusNotFound) {
		return fmt.Errorf("inspect Compose project %q: %w", p.Name, err)
	}

	c, err := global.EnsureProject(
		p.Name,
		composeclient.EnsureProjectWithCreate(),
		composeclient.EnsureProjectWithConfig(p.ClientConfig.XIncus),
	)
	if err != nil {
		return fmt.Errorf("ensure compose project %q: %w", p.Name, err)
	}
	defer c.WarnError(c.Done, "compose client cleanup failed")

	if err := c.Open(); err != nil {
		return fmt.Errorf("open compose project %q: %w", p.Name, err)
	}

	resources, err := p.Resources(c)
	if err != nil {
		return fmt.Errorf("build compose resources for %q: %w", p.Name, err)
	}
	order, err := p.ServiceOrder(false)
	if err != nil {
		return fmt.Errorf("order compose services for %q: %w", p.Name, err)
	}

	recreationCandidates := map[string]bool{}
	// Detect drift before any resource in this orchestration step is changed.
	// The pinned upstream Ensure only fills missing keys; silently treating
	// conflicting values as converged would be unsafe.
	for _, service := range order {
		for _, resource := range resources[service] {
			instance, ok := resource.(*composeclient.Instance)
			if !ok {
				continue
			}
			existing, _, err := connection.GetInstance(ctx, composeclient.SanitizeProjectName(p.Name), instance.IncusName(), nil)
			if err != nil {
				if api.StatusErrorCheck(err, http.StatusNotFound) {
					continue
				}
				return fmt.Errorf("inspect Compose instance %q before reconcile: %w", instance.IncusName(), err)
			}
			if instance.Config.Image != "" {
				imageResource, err := c.Resource(composeclient.KindImage, instance.Config.Image, &composeclient.ImageConfig{})
				if err != nil {
					return fmt.Errorf("resolve desired image for service %q: %w", service, err)
				}
				image, ok := imageResource.(*composeclient.Image)
				if !ok {
					return fmt.Errorf("resolve desired image for service %q: unexpected resource type", service)
				}
				if err := detectComposeImageDrift(image.IncusName(), existing.Config["user.image_alias"]); err != nil {
					if cfg.RecreateOnDrift {
						recreationCandidates[instance.IncusName()] = true
					} else {
						return fmt.Errorf("compose service %q instance %q: %w", service, instance.IncusName(), explainComposeRecreationSafety(err, existing.Devices))
					}
				}
				// Resolve the *current* local alias as well as comparing its
				// name. Without this, a refreshed mutable tag goes unnoticed.
				alias, _, err := connection.GetImageAlias(ctx, composeclient.SanitizeProjectName(p.Name), image.IncusName(), nil)
				if err == nil {
					if err := detectComposeImageFingerprintDrift(alias.Target, existing.Config["volatile.base_image"]); err != nil {
						if cfg.RecreateOnDrift {
							recreationCandidates[instance.IncusName()] = true
						} else {
							return fmt.Errorf("compose service %q instance %q: %w", service, instance.IncusName(), explainComposeRecreationSafety(err, existing.Devices))
						}
					}
				} else if !api.StatusErrorCheck(err, http.StatusNotFound) {
					return fmt.Errorf("inspect image alias for service %q: %w", service, err)
				}
				// Missing aliases may be downloaded in the prerequisite pass;
				// do not infer freshness before the image is available.

			}
			if err := detectComposeInstanceDrift(instance.Config.Extensions, instance.Config.Profiles, existing.Config, existing.Profiles); err != nil {
				if cfg.RecreateOnDrift {
					recreationCandidates[instance.IncusName()] = true
				} else {
					return fmt.Errorf("compose service %q instance %q: %w", service, instance.IncusName(), explainComposeRecreationSafety(err, existing.Devices))
				}
			}
			if err := detectComposeDeviceDrift(instance.Config.ExtraDevices, existing.Devices); err != nil {
				if cfg.RecreateOnDrift {
					recreationCandidates[instance.IncusName()] = true
				} else {
					return fmt.Errorf("compose service %q instance %q: %w", service, instance.IncusName(), explainComposeRecreationSafety(err, existing.Devices))
				}
			}
		}
	}

	runOptions := []composeclient.Option{composeclient.OptionCreate()}
	// Instance resources depend on images, networks, profiles and volumes.
	// Ensure prerequisites across *all* services before proceeding through
	// the service dependency order; per-service stacks otherwise try to
	// create an instance while its image has not yet been ensured.
	prerequisites := composeclient.NewStack(c, composeclient.StackWorkers(1), composeclient.StackFailFast())
	for _, service := range order {
		for _, resource := range resources[service] {
			if isComposePrerequisite(resource.Priority()) {
				prerequisites.Add(resource)
			}
		}
	}
	if err := prerequisites.ForAction(composeclient.ActionEnsure).Run(ctx, composeclient.ActionEnsure, runOptions...); err != nil {
		return fmt.Errorf("compose project %q ensure prerequisites: %w", p.Name, err)
	}
	// Image aliases absent at initial preflight can appear during the
	// prerequisite pass. Recheck every pre-existing owned instance now,
	// before Ensure or Start can silently reuse an obsolete base image.
	for _, service := range order {
		for _, resource := range resources[service] {
			instance, ok := resource.(*composeclient.Instance)
			if !ok || instance.Config.Image == "" {
				continue
			}
			existing, _, err := connection.GetInstance(ctx, composeclient.SanitizeProjectName(p.Name), instance.IncusName(), nil)
			if api.StatusErrorCheck(err, http.StatusNotFound) {
				continue
			}
			if err != nil {
				return fmt.Errorf("recheck Compose instance %q image state: %w", instance.IncusName(), err)
			}
			if err := verifyComposeInstanceOwnership(existing.Config, p.Name, instance.IncusName()); err != nil {
				return err
			}
			imageResource, err := c.Resource(composeclient.KindImage, instance.Config.Image, &composeclient.ImageConfig{})
			if err != nil {
				return fmt.Errorf("resolve ensured image for service %q: %w", service, err)
			}
			image, ok := imageResource.(*composeclient.Image)
			if !ok {
				return fmt.Errorf("resolve ensured image for service %q: unexpected resource type", service)
			}
			alias, _, err := connection.GetImageAlias(ctx, composeclient.SanitizeProjectName(p.Name), image.IncusName(), nil)
			if err != nil {
				return fmt.Errorf("resolve ensured image alias for service %q: %w", service, err)
			}
			if err := verifyComposeEnsuredImage(image.IncusName(), existing.Config["user.image_alias"], alias.Target, existing.Config["volatile.base_image"]); err != nil {
				if cfg.RecreateOnDrift {
					recreationCandidates[instance.IncusName()] = true
				} else {
					return fmt.Errorf("compose service %q instance %q: %w", service, instance.IncusName(), explainComposeRecreationSafety(err, existing.Devices))
				}
			}
		}
	}

	// A replacement is opt-in and never proceeds for attached persistent data.
	// We re-fetch ownership immediately before stop and once again before
	// deletion. A failed create remains an explicit error, never success.
	for _, service := range order {
		for _, resource := range resources[service] {
			instance, ok := resource.(*composeclient.Instance)
			if !ok || !recreationCandidates[instance.IncusName()] {
				continue
			}
			name := instance.IncusName()
			project := composeclient.SanitizeProjectName(p.Name)
			current, _, err := connection.GetInstance(ctx, project, name, nil)
			if err != nil {
				return fmt.Errorf("fetch instance for recreation: %w", err)
			}
			recheck := func(ctx context.Context) error {
				v, _, err := connection.GetInstance(ctx, project, name, nil)
				if err != nil {
					return err
				}
				return verifyComposeInstanceOwnership(v.Config, p.Name, name)
			}
			if err := rejectPersistentDisks(current.Devices); err != nil {
				return fmt.Errorf("recreate Compose instance %q: %w", name, err)
			}
			// Hydrate the pinned Compose resource before using its Stop/Delete
			// lifecycle. Its Ensure may add missing extension keys.
			if err := instance.Ensure(ctx, composeclient.OptionCreate()); err != nil {
				return fmt.Errorf("prepare owned instance %q for recreation: %w", name, err)
			}
			err = recreateWithoutPersistentDisks(ctx, current.Devices, recheck,
				func(ctx context.Context) error {
					if !instance.Running() {
						return nil
					}
					return instance.Stop(ctx)
				},
				func(ctx context.Context) error { return instance.Delete(ctx) },
				func(ctx context.Context) error { return instance.Ensure(ctx, composeclient.OptionCreate()) },
			)
			if err != nil {
				return fmt.Errorf("recreate Compose instance %q: %w", name, err)
			}
		}
	}

	for _, action := range []composeclient.Action{composeclient.ActionEnsure, composeclient.ActionStart} {
		if err := runComposeServicesSequentially(ctx, order, func(service string) error {
			for _, resource := range resources[service] {
				if action == composeclient.ActionEnsure && isComposePrerequisite(resource.Priority()) {
					continue // already ensured in the prerequisite pass
				}
				if action == composeclient.ActionStart && !resource.IsEnsured() {
					continue
				}
				stack := composeclient.NewStack(c, composeclient.StackWorkers(1))
				stack.Add(resource)
				if err := stack.ForAction(action).Run(ctx, action, runOptions...); err != nil {
					return fmt.Errorf("service %q resource action %v: %w", service, action, err)
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("compose project %q action %v: %w", p.Name, action, err)
		}
	}
	return nil
}

func loadProject(ctx context.Context, name, path string, environment map[string]string) (*composeproject.Project, error) {
	options := []composeproject.LoadOption{
		composeproject.LoadFiles([]string{path}),
		composeproject.LoadName(name),
		composeproject.LoadInstanceMarks(map[string]string{
			ownershipManagedKey:    "true",
			ownershipResourceKey:   "compose-instance",
			ownershipDeploymentKey: name,
		}),
		composeproject.LoadProjectMarks(map[string]string{
			ownershipManagedKey:    "true",
			ownershipResourceKey:   "compose-project",
			ownershipDeploymentKey: name,
		}),
	}
	if len(environment) > 0 {
		envPath, cleanup, err := writeEnvironment(environment)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		options = append(options, composeproject.LoadEnvFiles([]string{envPath}))
	}
	return composeproject.New().Load(ctx, options...)
}

func writeEnvironment(environment map[string]string) (string, func(), error) {
	f, err := os.CreateTemp("", "aginctus-compose-env-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary compose environment: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("protect temporary compose environment: %w", err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	for key, value := range environment {
		if _, err := fmt.Fprintf(f, "%s=%s\n", key, value); err != nil {
			_ = f.Close()
			cleanup()
			return "", nil, fmt.Errorf("write temporary compose environment: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary compose environment: %w", err)
	}
	return f.Name(), cleanup, nil
}

func writeCompose(contents []byte) (string, func(), error) {
	f, err := os.CreateTemp("", "aginctus-compose-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary compose file: %w", err)
	}
	cleanup := func() {
		_ = os.Remove(f.Name())
	}
	if _, err := f.Write(contents); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write temporary compose file: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temporary compose file: %w", err)
	}
	return f.Name(), cleanup, nil
}
