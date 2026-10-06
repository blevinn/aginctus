package apply

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	incusapply "github.com/abiosoft/incus-apply/apply"

	"github.com/blevinn/aginctus/internal/orchestration"
)

type fakeApplyClient struct {
	planned  bool
	executed bool
	input    string
}

func (f *fakeApplyClient) Plan(reader io.Reader) (incusapply.Preview, error) {
	f.planned = true
	data, _ := io.ReadAll(reader)
	f.input = string(data)
	return incusapply.Preview{}, nil
}

func (f *fakeApplyClient) Execute(reader io.Reader) (incusapply.Result, error) {
	f.executed = true
	data, _ := io.ReadAll(reader)
	f.input = string(data)
	return incusapply.Result{}, nil
}

func TestSupportsDryRun(t *testing.T) {
	if !New().SupportsDryRun() {
		t.Fatal("SupportsDryRun() = false, want true")
	}
}

func TestValidateRejectsEmptyDocuments(t *testing.T) {
	if err := New().Validate([]byte(`{"documents":[]}`)); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestExecuteDryRunUsesPlan(t *testing.T) {
	fake := &fakeApplyClient{}
	driver := New()
	driver.newClient = func(incusapply.Options) applyClient { return fake }

	err := driver.Execute(t.Context(), []byte(`{
		"documents": [{
			"kind": "network",
			"name": "aginctus-mgmt",
			"networkType": "bridge",
			"config": {
				"ipv4.address": "10.42.0.1/24",
				"user.aginctus.managed": "true"
			}
		}]
	}`), orchestration.ExecuteOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !fake.planned || fake.executed {
		t.Fatalf("planned=%v executed=%v", fake.planned, fake.executed)
	}
	for _, want := range []string{"kind: network", "name: aginctus-mgmt", "networkType: bridge"} {
		if !strings.Contains(fake.input, want) {
			t.Fatalf("rendered input missing %q:\n%s", want, fake.input)
		}
	}
}

func TestExecuteAppliesThroughNativeClient(t *testing.T) {
	fake := &fakeApplyClient{}
	driver := New()
	driver.newClient = func(options incusapply.Options) applyClient {
		if options.Operation != incusapply.Upsert || !options.FailFast {
			t.Fatalf("options = %#v", options)
		}
		return fake
	}

	err := driver.Execute(t.Context(), []byte(`{
		"documents": [{"kind":"network","name":"aginctus-mgmt","networkType":"bridge"}]
	}`), orchestration.ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if fake.planned || !fake.executed {
		t.Fatalf("planned=%v executed=%v", fake.planned, fake.executed)
	}
}

func TestRenderDocumentsProducesYAMLStream(t *testing.T) {
	rendered, err := renderDocuments([]json.RawMessage{
		json.RawMessage(`{"kind":"network","name":"one"}`),
		json.RawMessage(`{"kind":"network","name":"two"}`),
	})
	if err != nil {
		t.Fatalf("renderDocuments() error = %v", err)
	}
	if bytes.Count(rendered, []byte("---\n")) != 1 {
		t.Fatalf("rendered stream = %q", rendered)
	}
}

func TestExecuteMapsDeleteOperationAndExistingResourceGuard(t *testing.T) {
	fake := &fakeApplyClient{}
	driver := New()
	driver.newClient = func(options incusapply.Options) applyClient {
		if options.Operation != incusapply.Delete {
			t.Fatalf("Operation = %q, want delete", options.Operation)
		}
		if options.RequireExistingConfig["user.aginctus.managed"] != "true" {
			t.Fatalf("RequireExistingConfig = %#v", options.RequireExistingConfig)
		}
		return fake
	}

	err := driver.Execute(t.Context(), []byte(`{
		"operation": "delete",
		"requireExistingConfig": {
			"user.aginctus.managed": "true"
		},
		"documents": [{
			"kind": "network",
			"name": "aginctus-mgmt"
		}]
	}`), orchestration.ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !fake.executed {
		t.Fatal("Execute() did not call incus-apply")
	}
}

func TestValidateRejectsUnknownApplyOperation(t *testing.T) {
	err := New().Validate([]byte(`{
		"operation": "explode",
		"documents": [{"kind":"network","name":"aginctus-mgmt"}]
	}`))
	if err == nil {
		t.Fatal("Validate() error = nil, want unsupported operation error")
	}
}
