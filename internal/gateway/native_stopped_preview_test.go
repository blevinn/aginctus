package gateway

import (
	"context"
	"net/http"
	"testing"

	"github.com/lxc/incus/v7/shared/api"
)

type gatewayPreviewReader struct {
	*fakeGatewayStoppedClient
	volumeStatus int
}

func (c gatewayPreviewReader) GetStoragePoolVolume(pool, kind, name string) (*api.StorageVolume, string, error) {
	if c.volumeStatus != 0 {
		return nil, "", api.StatusErrorf(c.volumeStatus, "synthetic storage response")
	}
	return c.fakeGatewayStoppedClient.GetStoragePoolVolume(pool, kind, name)
}

func TestGatewayStoppedPreviewDistinguishesAbsentVolumeAndCollision(t *testing.T) {
	top, devices, pair, client := environmentGatewayCreateFixture(t)
	desc := gatewayTestEnvironment(top.EnvironmentID, top.Project)
	reader := gatewayPreviewReader{fakeGatewayStoppedClient: client, volumeStatus: http.StatusNotFound}
	preview := func() (NativeGatewayStoppedPreview, error) {
		return PreviewNativeEnvironmentGatewayStoppedWithReader(context.Background(), desc, desc.Binding, top, devices, pair, client.imagesForCreate, reader)
	}
	got, err := preview()
	if err != nil || !got.CreateDatabaseVolume || len(got.Instances.Steps) != 2 || got.Instances.Steps[0].Action != NativeGatewayCreateStopped {
		t.Fatalf("absent volume and pair: %+v %v", got, err)
	}
	reader.volumeStatus = http.StatusForbidden
	if _, err := preview(); err == nil {
		t.Fatal("inaccessible volume treated as absent")
	}
	reader.volumeStatus = 0
	if _, err := CreateStoppedGatewayWithClient(context.Background(), top, devices, pair, client.imagesForCreate, client, context.Context.Err); err != nil {
		t.Fatal(err)
	}
	got, err = preview()
	if err != nil || got.CreateDatabaseVolume || got.Instances.Steps[0].Action != NativeGatewayRetainExisting {
		t.Fatalf("existing volume and pair: %+v %v", got, err)
	}
	reader.volumeStatus = http.StatusNotFound
	if _, err := preview(); err == nil {
		t.Fatal("existing PostgreSQL with missing database volume accepted")
	}
	delete(client.instances, pair.Postgres.Name)
	client.instances[pair.LiteLLM.Name].Config["user.aginctus.environment-id"] = "123e4567-e89b-42d3-a456-426614174099"
	if _, err := preview(); err == nil {
		t.Fatal("foreign LiteLLM collision did not block volume planning")
	}
}
