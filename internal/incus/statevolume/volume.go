package statevolume

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	incusscope "github.com/blevinn/aginctus/internal/incus"
	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const Project = "default"

var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
var environmentID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Volume describes an Aginctus-owned filesystem volume.
type Volume struct {
	Pool          string
	Instance      string
	Class         string
	Project       string // empty only for the legacy default-project path
	EnvironmentID string
}

func (v Volume) Name() string { return v.Instance + "-state-" + v.Class }

func (v Volume) Validate() error {
	if !safeName.MatchString(v.Instance) || !safeName.MatchString(v.Pool) {
		return fmt.Errorf("unsafe state-volume instance or pool name")
	}
	if v.Class != "system" && v.Class != "user" {
		return fmt.Errorf("unknown state class %q", v.Class)
	}
	if v.Project != "" || v.EnvironmentID != "" {
		if _, err := incusscope.NewProjectScope(v.Project); err != nil || !environmentID.MatchString(v.EnvironmentID) || v.Class != "system" {
			return fmt.Errorf("scoped Herdr state requires a project and canonical environment identity")
		}
	}
	return nil
}

func (v Volume) markers() map[string]string {
	markers := map[string]string{
		"user.aginctus.managed":     "true",
		"user.aginctus.resource":    "state-volume",
		"user.aginctus.instance":    v.Instance,
		"user.aginctus.state-class": v.Class,
	}
	if v.EnvironmentID != "" {
		markers["user.aginctus.environment-id"] = v.EnvironmentID
	}
	return markers
}

// Backend abstracts Incus volume calls for deterministic lifecycle tests.
type Backend interface {
	Get(ctx context.Context, pool, name string) (*api.StorageVolume, error)
	Create(ctx context.Context, pool string, request api.StorageVolumesPost) error
}

// NotFound identifies absent custom volumes without adopting unrelated errors.
type NotFound struct{ Name string }

func (e NotFound) Error() string { return fmt.Sprintf("custom volume %q not found", e.Name) }

func Verify(v Volume, existing *api.StorageVolume) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if existing == nil || existing.Name != v.Name() || existing.Type != "custom" || existing.ContentType != "filesystem" {
		return fmt.Errorf("state volume %q has unexpected name/type/content", v.Name())
	}
	for key, want := range v.markers() {
		if existing.Config[key] != want {
			return fmt.Errorf("state volume %q ownership conflict (%s)", v.Name(), key)
		}
	}
	_, obsolete := existing.Config["user.aginctus.environment"]
	_, canonical := existing.Config["user.aginctus.environment-id"]
	if obsolete || v.Project == "" && canonical {
		return fmt.Errorf("conflicting state-volume environment ownership")
	}
	if v.Project != "" {
		if existing.Project != v.Project || existing.CreatedAt.IsZero() {
			return fmt.Errorf("state-volume project or incarnation unverified")
		}
		for _, use := range existing.UsedBy {
			u, err := url.Parse(use)
			if err != nil || u.IsAbs() || u.Host != "" || u.Path != "/1.0/instances/"+v.Instance || u.Fragment != "" {
				return fmt.Errorf("state volume has an unexpected attachment")
			}
			q, err := url.ParseQuery(u.RawQuery)
			if err != nil || len(q) != 1 || len(q["project"]) != 1 || q.Get("project") != v.Project {
				return fmt.Errorf("state volume has an unverified attachment project")
			}
		}
	}
	return nil
}

// Ensure creates a missing volume, and *never* adopts an unmarked one.
// It does not delete or rewrite existing state. A race is checked after
// the create call, so another operator cannot silently supply foreign state.
func Ensure(ctx context.Context, backend Backend, v Volume) error {
	if err := v.Validate(); err != nil {
		return err
	}
	existing, err := backend.Get(ctx, v.Pool, v.Name())
	if err == nil {
		return Verify(v, existing)
	}
	if _, ok := err.(NotFound); !ok {
		return fmt.Errorf("inspect state volume: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	req := api.StorageVolumesPost{
		Name: v.Name(), Type: "custom", ContentType: "filesystem",
		StorageVolumePut: api.StorageVolumePut{
			Description: "Aginctus " + v.Class + " state for " + v.Instance,
			Config:      v.markers(),
		},
	}
	if err := backend.Create(ctx, v.Pool, req); err != nil {
		return fmt.Errorf("create state volume %q: %w", v.Name(), err)
	}
	existing, err = backend.Get(ctx, v.Pool, v.Name())
	if err != nil {
		return fmt.Errorf("verify created state volume: %w", err)
	}
	return Verify(v, existing)
}

type nativeBackend struct{ server incusclient.InstanceServer }

func (b nativeBackend) Get(_ context.Context, pool, name string) (*api.StorageVolume, error) {
	volume, _, err := b.server.GetStoragePoolVolume(pool, "custom", name)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return nil, NotFound{Name: name}
	}
	return volume, err
}
func (b nativeBackend) Create(ctx context.Context, pool string, volume api.StorageVolumesPost) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// The Incus client handles the volume creation request synchronously and
	// returns only an error; there is no operation handle to wait on.
	if err := b.server.CreateStoragePoolVolume(pool, volume); err != nil {
		return err
	}
	return ctx.Err()
}

// EnsureSystem uses the local Incus Unix socket; no host NixOS facilities.
func EnsureSystem(ctx context.Context, pool, instance string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return fmt.Errorf("connect Incus: %w", err)
	}
	server = server.UseProject(Project)
	// Do not create a new persistent volume for a colliding foreign instance.
	existing, _, err := server.GetInstance(instance)
	if err != nil && !api.StatusErrorCheck(err, http.StatusNotFound) {
		return fmt.Errorf("inspect existing instance ownership: %w", err)
	}
	if err == nil {
		if existing.Config["user.aginctus.managed"] != "true" ||
			existing.Config["user.aginctus.resource"] != "infrastructure" ||
			existing.Config["user.aginctus.role"] != "herdr-client" {
			return fmt.Errorf("instance %q ownership conflict; refusing state volume", instance)
		}
	}
	return Ensure(ctx, nativeBackend{server: server}, Volume{Pool: pool, Instance: instance, Class: "system"})
}
