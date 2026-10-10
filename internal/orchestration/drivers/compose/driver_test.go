package compose

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lxc/incus-compose/iclient"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration"
)

func TestValidateAcceptsComposeConfiguration(t *testing.T) {
	driver := New()
	err := driver.Validate(json.RawMessage(`{
		"project": "aginctus-test",
		"compose": {
			"services": {
				"web": {
					"image": "alpine:latest"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsUnknownConfigurationField(t *testing.T) {
	driver := New()
	err := driver.Validate(json.RawMessage(`{
		"project": "aginctus-test",
		"compose": {"services": {}},
		"unexpected": true
	}`))
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestSupportsDryRunIsFalse(t *testing.T) {
	if New().SupportsDryRun() {
		t.Fatal("SupportsDryRun() = true, want false until incus-compose exposes planning")
	}
}

func TestExecuteUsesValidatedConfiguration(t *testing.T) {
	driver := New()
	var got Config
	driver.execute = func(_ context.Context, cfg Config, _ map[string]string) error {
		got = cfg
		return nil
	}

	err := driver.Execute(context.Background(), json.RawMessage(`{
		"project": "aginctus-test",
		"compose": {
			"services": {
				"web": {
					"image": "alpine:latest"
				}
			}
		}
	}`), orchestration.ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.Project != "aginctus-test" {
		t.Fatalf("project = %q, want aginctus-test", got.Project)
	}
}

func TestExecuteRejectsDryRun(t *testing.T) {
	driver := New()
	err := driver.Execute(context.Background(), json.RawMessage(`{
		"project": "aginctus-test",
		"compose": {"services": {}}
	}`), orchestration.ExecuteOptions{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "does not support dry-run") {
		t.Fatalf("Execute() error = %v, want dry-run capability error", err)
	}
}

func TestComposeClientConstructionProvidesLocalConnection(t *testing.T) {
	var called bool
	client, err := newComposeGlobalClient(context.Background(), func(info *iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
		called = true
		if info.Name != "local" || len(info.Addrs) != 1 || info.Addrs[0] != "unix://" {
			t.Fatalf("unexpected connection info: %#v", info)
		}
		return iclient.NewConnection(info)
	})
	if err != nil || !called || client == nil {
		t.Fatalf("newComposeGlobalClient() = %v, %v, called=%t", client, err, called)
	}
}

func TestComposeClientConstructionRejectsConnectionError(t *testing.T) {
	want := errors.New("unavailable socket")
	client, err := newComposeGlobalClient(context.Background(), func(*iclient.ConfigRemoteInfo) (*iclient.Connection, error) {
		return nil, want
	})
	if client != nil || !errors.Is(err, want) {
		t.Fatalf("newComposeGlobalClient() = %v, %v", client, err)
	}
}
