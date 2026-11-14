package plugins_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/pkg/orchestration"
)

func declarations(image string) []orchestration.Target {
	return []orchestration.Target{
		{ID: "service", Capability: "work", DependsOn: []string{"catalog"}, Containers: []orchestration.Container{{
			ID: "guest", Image: orchestration.ImageReference{Format: orchestration.IncusImage, Pointer: image},
			Contract: orchestration.GuestContract{ID: "helper", Version: "v1", Requirements: []string{"fixture helper v1"}},
		}}, Postconditions: []orchestration.Check{{
			ID: "probe", Claim: "guest helper responds", Scope: "selected guest", Required: true,
			Effect: orchestration.ActiveProbe, Access: []orchestration.Access{orchestration.GuestAccess},
			OptIns: []string{"guest-probe"}, Timeout: time.Second,
		}}, Observations: []orchestration.Check{{
			ID: "observed", Claim: "instance exists", Scope: "selected instance", Required: true,
			Effect: orchestration.ReadOnly, Access: []orchestration.Access{orchestration.IncusAccess}, Timeout: time.Second,
		}}},
		{ID: "catalog", Capability: "work", Preconditions: []orchestration.Check{{
			ID: "candidate", Claim: "candidate has expected shape", Scope: "supplied configuration",
			Effect: orchestration.Offline, Timeout: time.Second,
		}}},
	}
}

// fakeHost consumes declarations only. It checks usable dependency order and
// scopes without creating instances, evaluating runtime checks or granting access.
type fakeHost struct{ images map[plugins.TargetKey]string }

func (h *fakeHost) inspect(t *testing.T, graph plugins.Graph) {
	t.Helper()
	seen := make(map[plugins.TargetKey]bool)
	h.images = make(map[plugins.TargetKey]string)
	for _, target := range graph.Targets {
		for _, dependency := range target.DependsOn {
			if !seen[dependency] {
				t.Fatalf("dependency %s is not before %s", dependency, target.Key)
			}
		}
		seen[target.Key] = true
		if target.Key.Target != target.Definition.ID || target.Key.Deployment != graph.Deployment {
			t.Fatal("assembly rewrote local IDs or lost deployment scope")
		}
		if len(target.Definition.Containers) != 0 {
			h.images[target.Key] = target.Definition.Containers[0].Image.Pointer
		}
	}
}

func TestHostConsumesScopedPublicContributions(t *testing.T) {
	reverse := false
	p := contribution{discovery: newDiscovery("example"), targets: func(_ context.Context, raw json.RawMessage) ([]orchestration.Target, error) {
		var config struct{ Image string }
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
		result := declarations(config.Image)
		if reverse {
			result[0], result[1] = result[1], result[0]
		}
		return result, nil
	}}
	r := registry(t, p)
	first := plugins.Instance{Name: "a", Plugin: "example", Configuration: json.RawMessage(`{"Image":"custom:first"}`)}
	second := plugins.Instance{Name: "b", Plugin: "example", Configuration: json.RawMessage(`{"Image":"custom:second"}`)}
	graph, err := r.Assemble(context.Background(), "lab", []plugins.Instance{second, first})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lab/a/catalog", "lab/a/service", "lab/b/catalog", "lab/b/service"}
	var keys []string
	for _, target := range graph.Targets {
		keys = append(keys, target.Key.String())
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("dependency order = %v, want %v", keys, want)
	}
	var host fakeHost
	host.inspect(t, graph)
	if host.images[plugins.TargetKey{Deployment: "lab", Instance: "a", Target: "service"}] != "custom:first" ||
		host.images[plugins.TargetKey{Deployment: "lab", Instance: "b", Target: "service"}] != "custom:second" {
		t.Fatal("instances lost independently configured image pointers")
	}
	reverse = true
	reordered, err := r.Assemble(context.Background(), "lab", []plugins.Instance{first, second})
	if err != nil || !reflect.DeepEqual(graph, reordered) {
		t.Fatalf("instance or target enumeration changed assembly: %v", err)
	}
	other, err := r.Assemble(context.Background(), "other", []plugins.Instance{first})
	if err != nil || other.Targets[0].Key == graph.Targets[0].Key {
		t.Fatalf("deployment scope collided: %+v, %v", other, err)
	}
}

func TestInvalidGraphsReturnNoPartialAssembly(t *testing.T) {
	for _, tt := range []struct {
		name    string
		targets []orchestration.Target
	}{
		{"missing", []orchestration.Target{{ID: "service", Capability: "work", DependsOn: []string{"absent"}}}},
		{"cycle", []orchestration.Target{{ID: "first", Capability: "work", DependsOn: []string{"second"}}, {ID: "second", Capability: "work", DependsOn: []string{"first"}}}},
		{"duplicate target", []orchestration.Target{{ID: "same", Capability: "work"}, {ID: "same", Capability: "work"}}},
		{"unadvertised capability", []orchestration.Target{{ID: "target", Capability: "unknown"}}},
		{"invalid criterion", []orchestration.Target{{ID: "target", Capability: "work", Preconditions: []orchestration.Check{{ID: "incomplete"}}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := contribution{discovery: newDiscovery("example"), targets: func(context.Context, json.RawMessage) ([]orchestration.Target, error) { return tt.targets, nil }}
			graph, err := registry(t, p).Assemble(context.Background(), "lab", []plugins.Instance{instance("one", "example")})
			if err == nil || !reflect.DeepEqual(graph, plugins.Graph{}) {
				t.Fatalf("invalid graph yielded partial assembly: %+v, %v", graph, err)
			}
		})
	}
}

func TestDependencyCannotResolveThroughAnotherInstance(t *testing.T) {
	p := contribution{discovery: newDiscovery("example"), targets: func(_ context.Context, raw json.RawMessage) ([]orchestration.Target, error) {
		if string(raw) == `{"needs":true}` {
			return []orchestration.Target{{ID: "service", Capability: "work", DependsOn: []string{"catalog"}}}, nil
		}
		return []orchestration.Target{{ID: "catalog", Capability: "work"}}, nil
	}}
	first := plugins.Instance{Name: "first", Plugin: "example", Configuration: json.RawMessage(`{"needs":true}`)}
	graph, err := registry(t, p).Assemble(context.Background(), "lab", []plugins.Instance{first, instance("other", "example")})
	if err == nil || !strings.Contains(err.Error(), "lab/first/catalog") || !reflect.DeepEqual(graph, plugins.Graph{}) {
		t.Fatalf("dependency escaped local instance scope: %+v, %v", graph, err)
	}
}

func TestAssemblySnapshotsMetadataAndNestedDeclarations(t *testing.T) {
	source := declarations("images:original")
	p := contribution{discovery: newDiscovery("example"), targets: func(context.Context, json.RawMessage) ([]orchestration.Target, error) { return source, nil }}
	r := registry(t, p)
	// Contribution validation uses registered metadata, not mutable rediscovery.
	p.descriptor.Capabilities[0].Name = "changed"
	graph, err := r.Assemble(context.Background(), "lab", []plugins.Instance{instance("one", "example")})
	if err != nil {
		t.Fatal(err)
	}
	service := &graph.Targets[1].Definition
	service.DependsOn[0] = "changed"
	service.Containers[0].Contract.Requirements[0] = "changed"
	service.Postconditions[0].Access[0] = orchestration.HostAccess
	service.Postconditions[0].OptIns[0] = "changed"
	service.Observations[0].Access[0] = orchestration.HostAccess
	graph.Targets[0].Definition.Preconditions[0].Claim = "changed"
	if !reflect.DeepEqual(source, declarations("images:original")) {
		t.Fatal("assembled graph aliases plugin declaration slices")
	}
	// Later plugin mutation must not rewrite a graph already handed to a consumer.
	source[0].Containers[0].Image.Pointer = "images:new"
	if service.Containers[0].Image.Pointer != "images:original" {
		t.Fatal("plugin mutation rewrote previously assembled graph")
	}
}

func TestAssemblyDoesNotShareConfigurationWithCaller(t *testing.T) {
	selected := instance("one", "example")
	p := contribution{discovery: newDiscovery("example"), targets: func(_ context.Context, raw json.RawMessage) ([]orchestration.Target, error) {
		// Trusted plugin code could mutate its input. Do not let that rewrite the
		// caller's candidate bytes, even though it violates the pure contract.
		raw[0] = ' '
		return nil, nil
	}}
	if _, err := registry(t, p).Assemble(context.Background(), "lab", []plugins.Instance{selected}); err != nil {
		t.Fatal(err)
	}
	if string(selected.Configuration) != `{}` {
		t.Fatal("plugin callback rewrote caller configuration")
	}
}
