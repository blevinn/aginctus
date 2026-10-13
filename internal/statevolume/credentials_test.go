package statevolume

import (
	"context"
	"errors"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

func TestEnsureGatewayCredentialsCreatesOnceAndPreservesState(t *testing.T) {
	v := GatewayCredentials{Pool: "default", GatewayID: "local"}
	b := &mockBackend{getError: NotFound{Name: v.Name()}}
	if err := EnsureGatewayCredentials(context.Background(), b, v); err != nil {
		t.Fatal(err)
	}
	if b.creates != 1 {
		t.Fatalf("create count=%d", b.creates)
	}
	if err := EnsureGatewayCredentials(context.Background(), b, v); err != nil {
		t.Fatal(err)
	}
	if b.creates != 1 {
		t.Fatal("already-owned gateway credential storage was recreated")
	}
	if b.volume.Config["user.aginctus.resource"] != "gateway-credentials" {
		t.Fatal("missing ownership resource")
	}
	if b.volume.Name != "aginctus-gateway-local-credentials" {
		t.Fatalf("unexpected volume name %q", b.volume.Name)
	}
}

func TestGatewayCredentialsRejectsForeignStorage(t *testing.T) {
	v := GatewayCredentials{Pool: "default", GatewayID: "local"}
	for _, got := range []*api.StorageVolume{
		{Name: v.Name(), Type: "custom", ContentType: "filesystem"},
		{Name: v.Name(), Type: "custom", ContentType: "block", StorageVolumePut: api.StorageVolumePut{Config: v.markers()}},
		{Name: "different", Type: "custom", ContentType: "filesystem", StorageVolumePut: api.StorageVolumePut{Config: v.markers()}},
	} {
		b := &mockBackend{volume: got}
		if err := EnsureGatewayCredentials(context.Background(), b, v); err == nil || b.creates != 0 {
			t.Fatalf("foreign credential volume accepted: err=%v, creates=%d", err, b.creates)
		}
	}
}

func TestCredentialStorageFailsClosedOnInspectionAndBadNames(t *testing.T) {
	v := GatewayCredentials{Pool: "default", GatewayID: "local"}
	b := &mockBackend{getError: errors.New("incus authorization denied")}
	if err := EnsureGatewayCredentials(context.Background(), b, v); err == nil || b.creates != 0 {
		t.Fatalf("inspection error not fail-closed: %v", err)
	}
	for _, invalid := range []GatewayCredentials{
		{Pool: "default", GatewayID: "../escape"},
		{Pool: "default", GatewayID: ""},
		{Pool: "", GatewayID: "local"},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("unsafe volume naming accepted: %+v", invalid)
		}
	}
}

func TestCredentialStorageDoesNotCreateWhenContextCanceled(t *testing.T) {
	v := GatewayCredentials{Pool: "default", GatewayID: "local"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &mockBackend{getError: NotFound{Name: v.Name()}}
	if err := EnsureGatewayCredentials(ctx, b, v); err == nil || b.creates != 0 {
		t.Fatalf("canceled ensure modified storage: %v", err)
	}
}
