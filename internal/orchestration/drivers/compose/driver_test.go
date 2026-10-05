package compose

import (
	"context"
	"encoding/json"
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
