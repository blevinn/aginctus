package testenv

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

func payloadVMName(name string) bool {
	return strings.HasPrefix(name, "testenv-") && vmRunID.MatchString(strings.TrimPrefix(name, "testenv-"))
}

func (n *nativeIncusVMClient) payloadPresent(ctx context.Context, name string) (bool, error) {
	if !payloadVMName(name) {
		return false, ErrOwnershipMismatch
	}
	ctx, server, cancel := n.requestClient(ctx)
	defer cancel()
	body, _, err := server.GetInstanceFile(name, guestPayloadPath)
	if body != nil {
		body.Close()
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return false, nil
	}
	if err != nil || body == nil {
		return false, errors.New("guest payload destination inventory unavailable")
	}
	return true, nil
}

func (n *nativeIncusVMClient) writePayload(ctx context.Context, name string, data []byte) error {
	if !payloadVMName(name) || len(data) == 0 || len(data) > maxGuestPayloadBytes {
		return ErrOwnershipMismatch
	}
	ctx, server, cancel := n.requestClient(ctx)
	defer cancel()
	// Incus file upload lacks an atomic no-replace mode. The trusted worker
	// must prove absence and exclude guest writers before this operation.
	err := server.CreateInstanceFile(name, guestPayloadPath, incusclient.InstanceFileArgs{
		Content: bytes.NewReader(data), UID: 0, GID: 0, Mode: 0600, Type: "file", WriteMode: "overwrite",
	})
	if err != nil {
		return safeIncusError("guest payload transfer unverified", err)
	}
	return ctx.Err()
}

func (n *nativeIncusVMClient) readPayload(ctx context.Context, name string) ([]byte, error) {
	if !payloadVMName(name) {
		return nil, ErrOwnershipMismatch
	}
	ctx, server, cancel := n.requestClient(ctx)
	defer cancel()
	body, meta, err := server.GetInstanceFile(name, guestPayloadPath)
	if body != nil {
		defer body.Close()
	}
	if err != nil || body == nil || meta == nil || meta.UID != 0 || meta.GID != 0 || meta.Mode != 0600 || meta.Type != "file" {
		return nil, errors.New("guest payload readback metadata unverified")
	}
	data, err := io.ReadAll(io.LimitReader(body, maxGuestPayloadBytes+1))
	if err != nil || len(data) > maxGuestPayloadBytes || ctx.Err() != nil {
		return nil, errors.New("guest payload readback incomplete or oversized")
	}
	return data, nil
}

var _ guestPayloadClient = (*nativeIncusVMClient)(nil)
