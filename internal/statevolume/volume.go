package statevolume

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const Project = "default"

var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// Volume describes an Aginctus-owned filesystem volume.
type Volume struct {
	Pool     string
	Instance string
	Class    string
}

func (v Volume) Name() string { return v.Instance + "-state-" + v.Class }

func (v Volume) Validate() error {
	if !safeName.MatchString(v.Instance) || !safeName.MatchString(v.Pool) {
		return fmt.Errorf("unsafe state-volume instance or pool name")
	}
	if v.Class != "system" && v.Class != "user" {
		return fmt.Errorf("unknown state class %q", v.Class)
	}
	return nil
}

func (v Volume) markers() map[string]string {
	return map[string]string{
		"user.aginctus.managed": "true",
		"user.aginctus.resource": "state-volume",
		"user.aginctus.instance": v.Instance,
		"user.aginctus.state-class": v.Class,
	}
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
	if err := v.Validate(); err != nil { return err }
	if existing == nil || existing.Name != v.Name() || existing.Type != "custom" || existing.ContentType != "filesystem" {
		return fmt.Errorf("state volume %q has unexpected name/type/content", v.Name())
	}
	for key, want := range v.markers() {
		if existing.Config[key] != want {
			return fmt.Errorf("state volume %q ownership conflict (%s)", v.Name(), key)
		}
	}
	return nil
}

// Ensure creates a missing volume, and *never* adopts an unmarked one.
// It does not delete or rewrite existing state. A race is checked after
// the create call, so another operator cannot silently supply foreign state.
func Ensure(ctx context.Context, backend Backend, v Volume) error {
	if err := v.Validate(); err != nil { return err }
	existing, err := backend.Get(ctx, v.Pool, v.Name())
	if err == nil { return Verify(v, existing) }
	if _, ok := err.(NotFound); !ok { return fmt.Errorf("inspect state volume: %w", err) }
	if err := ctx.Err(); err != nil { return err }
	req := api.StorageVolumesPost{
		Name: v.Name(), Type: "custom", ContentType: "filesystem",
		StorageVolumePut: api.StorageVolumePut{
			Description: "Aginctus " + v.Class + " state for " + v.Instance,
			Config: v.markers(),
		},
	}
	if err := backend.Create(ctx, v.Pool, req); err != nil {
		return fmt.Errorf("create state volume %q: %w", v.Name(), err)
	}
	existing, err = backend.Get(ctx, v.Pool, v.Name())
	if err != nil { return fmt.Errorf("verify created state volume: %w", err) }
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
	op, err := b.server.CreateStoragePoolVolume(pool, volume)
	if err != nil { return err }
	// Incus's Operation.Wait blocks until the operation finishes. Check the
	// caller context before the request and after completion.
	if err := op.Wait(); err != nil { return err }
	return ctx.Err()
}

// EnsureSystem uses the local Incus Unix socket; no host NixOS facilities.
func EnsureSystem(ctx context.Context, pool, instance string) error {
	if err := ctx.Err(); err != nil { return err }
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, "", &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil { return fmt.Errorf("connect Incus: %w", err) }
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
