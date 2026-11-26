package testenv

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"
)

const (
	guestPayloadPath      = "/run/aginctus-testenv-payload.bundle"
	maxGuestPayloadBytes = 1 << 20
	guestPayloadTimeout  = 30 * time.Second
)

// This optional trusted capability has one fixed guest destination. It
// cannot transport arbitrary paths, mount host files, or grant guest exec.
type guestPayloadClient interface {
	payloadPresent(context.Context, string) (bool, error)
	writePayload(context.Context, string, []byte) error
	readPayload(context.Context, string) ([]byte, error)
}

// deliverApprovedPayloadBundle is a bounded transport primitive, not
// source authorization. Only the trusted operator worker may call it, on
// a fresh signed VM with no concurrent guest/daemon writer. A failed or
// uncertain transfer must not be retried or adopted; destroy the VM.
func (b *IncusVMBackend) deliverApprovedPayloadBundle(ctx context.Context, ref VMRef, data []byte, manifestDigest string) (PayloadBundleReceipt, error) {
	if b == nil || b.client == nil {
		return PayloadBundleReceipt{}, ErrBackendUnavailable
	}
	if len(data) == 0 || len(data) > maxGuestPayloadBytes {
		return PayloadBundleReceipt{}, errors.New("guest payload exceeds transport bound")
	}
	data = bytes.Clone(data)
	receipt, err := VerifyApprovedPayloadBundle(bytes.NewReader(data), manifestDigest)
	if err != nil {
		return PayloadBundleReceipt{}, err
	}
	transport, ok := b.client.(guestPayloadClient)
	if !ok {
		return PayloadBundleReceipt{}, ErrUnsupportedAction
	}
	ctx, cancel := context.WithTimeout(ctx, guestPayloadTimeout)
	defer cancel()
	check := func() error {
		instance, expiry, err := b.loadOwned(ctx, ref)
		if err != nil {
			return err
		}
		deadline, _ := ctx.Deadline()
		if !strings.EqualFold(instance.Status, "Running") || ref.Policy.Retention != VMEphemeral ||
			!b.now().Before(expiry) || b.now().Add(time.Until(deadline)).After(expiry) {
			return errors.New("guest payload requires a running ephemeral VM within its lifetime")
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return PayloadBundleReceipt{}, err
	}
	present, err := transport.payloadPresent(ctx, ref.Instance)
	if err != nil || present {
		return PayloadBundleReceipt{}, errors.New("guest payload destination not proven absent")
	}
	if err := check(); err != nil {
		return PayloadBundleReceipt{}, err
	}
	if err := transport.writePayload(ctx, ref.Instance, data); err != nil {
		return PayloadBundleReceipt{}, errors.New("guest payload delivery uncertain")
	}
	if err := check(); err != nil {
		return PayloadBundleReceipt{}, err
	}
	actual, err := transport.readPayload(ctx, ref.Instance)
	if err != nil || !bytes.Equal(actual, data) {
		return PayloadBundleReceipt{}, errors.New("guest payload readback differs or is unverified")
	}
	if err := check(); err != nil {
		return PayloadBundleReceipt{}, err
	}
	receipt.Status = "guest_transfer_verified"
	return receipt, nil
}
