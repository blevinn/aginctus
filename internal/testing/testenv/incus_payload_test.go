package testenv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type payloadTransportFake struct {
	*fakeIncusVMClient
	data []byte
	writes int
	uncertain bool
	corrupt bool
	drift bool
}

func (c *payloadTransportFake) payloadPresent(_ context.Context, name string) (bool, error) {
	if c.drift {
		vm := c.vms[name]
		vm.Devices["external"] = map[string]string{"type": "disk", "source": "/foreign"}
		c.vms[name] = vm
	}
	return c.data != nil, nil
}

func (c *payloadTransportFake) writePayload(_ context.Context, _ string, data []byte) error {
	c.writes++
	c.data = bytes.Clone(data)
	if c.uncertain {
		return errors.New("private daemon detail")
	}
	return nil
}

func (c *payloadTransportFake) readPayload(context.Context, string) ([]byte, error) {
	if c.corrupt {
		return []byte("different"), nil
	}
	return bytes.Clone(c.data), nil
}

func TestGuestPayloadTransferRequiresSignedScopeAndReadback(t *testing.T) {
	raw := bundleFixture(t, "synthetic payload")
	header, _, _ := strings.Cut(raw, "\n")
	pin := fmt.Sprintf("%x", sha256.Sum256([]byte(header+"\n")))
	for _, mode := range []string{"pass", "present", "uncertain", "corrupt", "drift", "forged", "retained", "expired", "bad-pin", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			c := &payloadTransportFake{fakeIncusVMClient: newFakeIncusClient(t)}
			b, err := newTestIncusBackend(c, "testenv")
			if err != nil {
				t.Fatal(err)
			}
			spec := admittedSpec()
			if mode == "retained" {
				spec.Retention = VMRetained
			}
			ref, err := b.Create(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			data, digest := []byte(raw), pin
			switch mode {
			case "present":
				c.data = []byte("foreign")
			case "uncertain":
				c.uncertain = true
			case "corrupt":
				c.corrupt = true
			case "drift":
				c.drift = true
			case "forged":
				ref.Instance = "foreign"
			case "expired":
				b.now = func() time.Time { return ref.Policy.ExpiresAt }
			case "bad-pin":
				digest = strings.Repeat("0", 64)
			case "oversized":
				data = make([]byte, maxGuestPayloadBytes+1)
			}
			receipt, err := b.deliverApprovedPayloadBundle(ctx, ref, data, digest)
			if mode == "pass" {
				if err != nil || receipt.Status != "guest_transfer_verified" || receipt.SourceAuthority != "not_verified" || c.writes != 1 {
					t.Fatalf("transfer: %+v %v", receipt, err)
				}
				if _, err := b.deliverApprovedPayloadBundle(ctx, ref, data, digest); err == nil || c.writes != 1 {
					t.Fatal("existing payload overwritten or adopted")
				}
			} else {
				if err == nil || receipt.Status != "" || strings.Contains(err.Error(), "private daemon detail") {
					t.Fatalf("unsafe transfer: %+v %v", receipt, err)
				}
				wantWrites := 0
				if mode == "uncertain" || mode == "corrupt" {
					wantWrites = 1
				}
				if c.writes != wantWrites {
					t.Fatalf("unexpected transfer count %d", c.writes)
				}
			}
		})
	}
}
