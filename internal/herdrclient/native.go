package herdrclient

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/blevinn/aginctus/internal/incus/statevolume"
	"github.com/blevinn/aginctus/internal/orchestration"
	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
	"github.com/lxc/incus/v7/shared/api"
)

const (
	NativeInstanceID = "herdr-client"
	NativeVolumeID   = "herdr-system-state"
)

type NativeClient interface {
	native.ProjectInstanceClient
	GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error)
	CreateStoragePoolVolume(string, api.StorageVolumesPost) error
}

// BindNative supplies the persistent volume and instance stages. Composition
// must require the volume before the instance, including stop/removal, and
// independently authorize this exact image, pool, bridge, name and operation.
// CreateState explicitly permits initial bootstrap when both objects are absent.
// No existing instance may be given an empty replacement identity disk.
func (s Spec) BindNative(enrolled environment.Descriptor, revision environment.AcceptedRevision, op native.ProjectInstanceOperation, imageFingerprint, trustedPin string, createState bool, client NativeClient, expectedInstance, expectedVolume time.Time, verifyUse func(context.Context) error) (volume, instance orchestration.NativeResourceBinding, err error) {
	if enrolled.Validate() != nil || revision.ValidateIdentity() != nil || revision.EnvironmentID != enrolled.ID || revision.ServerID != enrolled.Binding.ServerID || revision.Project != enrolled.Binding.Project ||
		client == nil || verifyUse == nil || s.ManagementNetwork == "" || createState && op != native.ReconcileInstance {
		return volume, instance, errors.New("invalid pinned Herdr lifecycle binding")
	}
	v := statevolume.Volume{Pool: s.StoragePool, Instance: s.Name, Class: "system", Project: enrolled.Binding.Project, EnvironmentID: enrolled.ID}
	if err := v.Validate(); err != nil {
		return volume, instance, err
	}
	intent := native.InstanceIntent{
		Identity: native.InstanceIdentity{Project: v.Project, Name: s.Name}, EnvironmentID: enrolled.ID, HerdrClient: true,
		Type: "container", ImageFingerprint: imageFingerprint, EnsureRunning: true,
		Description: "Aginctus Herdr client infrastructure", Profiles: []string{},
		Config: map[string]string{"user.aginctus.managed": "true", "user.aginctus.resource": "infrastructure", "user.aginctus.role": "herdr-client", "user.aginctus.environment-id": enrolled.ID},
		Devices: map[string]map[string]string{
			"root":         {"type": "disk", "path": "/", "pool": v.Pool},
			"state_system": {"type": "disk", "pool": v.Pool, "source": v.Name(), "path": "/var/lib/aginctus/state/system"},
			"management":   {"type": "nic", "network": s.ManagementNetwork, "name": "eth0"},
		},
	}
	stage, err := native.NewProjectInstanceStage(intent, op, enrolled.Binding, trustedPin, client)
	if err != nil {
		return volume, instance, err
	}
	backend := herdrVolumeBackend{client}
	read := func(ctx context.Context) (*api.StorageVolume, error) {
		if err := native.VerifyProjectTransport(ctx, client, enrolled.Binding, trustedPin); err != nil {
			return nil, err
		}
		p, _, err := client.GetProject(v.Project)
		if err != nil || p == nil || p.Name != v.Project || p.Config["features.storage.volumes"] != "true" {
			return nil, errors.New("project-local Herdr storage unavailable")
		}
		data, err := backend.Get(ctx, v.Pool, v.Name())
		if _, missing := err.(statevolume.NotFound); missing {
			if !expectedVolume.IsZero() {
				return nil, errors.New("authorized Herdr state volume is absent")
			}
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if data != nil && !expectedVolume.IsZero() && !data.CreatedAt.Equal(expectedVolume) {
			return nil, errors.New("Herdr state differs from authorized live incarnation")
		}
		return data, statevolume.Verify(v, data)
	}
	canCreate := func(ctx context.Context) error {
		if !createState {
			return errors.New("retained Herdr state volume is missing")
		}
		// This also proves verified absence and refuses unknown/foreign instances.
		actions, incarnation, err := stage.Observe(ctx)
		if err != nil || !incarnation.IsZero() || len(actions) == 0 || actions[0] != native.Create {
			return errors.New("will not create an identity volume for an existing or unknown Herdr instance")
		}
		return nil
	}
	var previewed, verified time.Time
	verifyVolume := func(ctx context.Context) (string, error) {
		data, err := read(ctx)
		if err != nil || data == nil || verified.IsZero() || !data.CreatedAt.Equal(verified) {
			return "", errors.New("retained Herdr volume incarnation unverified")
		}
		return verified.UTC().Format(time.RFC3339Nano), nil
	}
	volume = orchestration.NativeResourceBinding{
		Validate: func() error { return nil },
		Preview: func(ctx context.Context) (orchestration.NativePreview, error) {
			data, err := read(ctx)
			if err != nil {
				return orchestration.NativePreview{}, err
			}
			action := native.Unchanged
			previewed = time.Time{}
			if data == nil {
				if err := canCreate(ctx); err != nil {
					return orchestration.NativePreview{}, err
				}
				action = native.Create
			} else {
				previewed = data.CreatedAt
			}
			return orchestration.NativePreview{Actions: []orchestration.NativeEffect{{Action: action, Target: NativeVolumeID}}}, nil
		},
		Apply: func(ctx context.Context, run orchestration.RunNativeEffect) error {
			data, err := read(ctx)
			if err != nil {
				return err
			}
			if !previewed.IsZero() {
				if data == nil || !data.CreatedAt.Equal(previewed) {
					return errors.New("retained Herdr volume changed after preview")
				}
				verified = data.CreatedAt
				return nil
			}
			if data != nil {
				return errors.New("selected Herdr volume appeared after preview")
			}
			return run(orchestration.NativeEffect{Action: native.Create, Target: NativeVolumeID}, func(ctx context.Context) error {
				if err := verifyUse(ctx); err != nil {
					return err
				}
				if err := canCreate(ctx); err != nil {
					return err
				}
				if old, err := read(ctx); err != nil || old != nil {
					return errors.New("selected Herdr volume absence changed at submission")
				}
				// Ensure has no update/delete surface. Require explicit absence
				// at its last read, rather than allowing reuse after this race.
				writer := &createOnlyHerdrVolume{herdrVolumeBackend: backend}
				if err := statevolume.Ensure(ctx, writer, v); err != nil {
					return err
				}
				verified = writer.created
				return nil
			})
		},
		Conditions: orchestration.StepConditions{Postconditions: []orchestration.BoundCheck{
			orchestration.NativeObservationCheck(NativeVolumeID, "state-volume-verified", "Herdr identity volume verified", revision.ID, verifyVolume),
		}},
	}
	instance = orchestration.BindNativeProjectInstance(NativeInstanceID, revision.ID, stage, expectedInstance, verifyUse, func(ctx context.Context) error {
		if err := verifyUse(ctx); err != nil {
			return err
		}
		_, err := verifyVolume(ctx)
		return err
	})
	return volume, instance, nil
}

type herdrVolumeBackend struct{ client NativeClient }

func (b herdrVolumeBackend) Get(ctx context.Context, pool, name string) (*api.StorageVolume, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v, etag, err := b.client.GetStoragePoolVolume(pool, "custom", name)
	if v == nil && etag == "" && api.StatusErrorCheck(err, http.StatusNotFound) {
		return nil, statevolume.NotFound{Name: name}
	}
	if err != nil || v == nil || etag == "" {
		return nil, errors.New("selected Herdr state-volume observation unavailable")
	}
	return v, ctx.Err()
}

func (b herdrVolumeBackend) Create(ctx context.Context, pool string, post api.StorageVolumesPost) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.client.CreateStoragePoolVolume(pool, post); err != nil {
		return errors.New("selected Herdr state-volume creation unverified")
	}
	return ctx.Err()
}

type createOnlyHerdrVolume struct {
	herdrVolumeBackend
	submitted bool
	created   time.Time
}

func (b *createOnlyHerdrVolume) Get(ctx context.Context, pool, name string) (*api.StorageVolume, error) {
	v, err := b.herdrVolumeBackend.Get(ctx, pool, name)
	if err == nil {
		if !b.submitted {
			return nil, errors.New("selected Herdr volume appeared at submission")
		}
		b.created = v.CreatedAt
	}
	return v, err
}

func (b *createOnlyHerdrVolume) Create(ctx context.Context, pool string, post api.StorageVolumesPost) error {
	b.submitted = true
	return b.herdrVolumeBackend.Create(ctx, pool, post)
}
