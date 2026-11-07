package native

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type fakeNetworkClient struct {
	scope   incusscope.HostSharedScope
	network *api.Network
	etag    string
	mode    string
	reads   int
	creates int
	updates int
	deletes int
	created api.NetworksPost
	updated api.NetworkPut
	cancel  context.CancelFunc
}

func (f *fakeNetworkClient) GetConnectionInfo() (*incus.ConnectionInfo, error) {
	info := &incus.ConnectionInfo{
		Protocol:   "incus",
		Project:    f.scope.Project(),
		SocketPath: "/test/incus.sock",
	}
	switch f.mode {
	case "remote":
		info.SocketPath = ""
	case "wrong project":
		info.Project = "aginctus"
	}
	return info, nil
}

func (f *fakeNetworkClient) GetNetwork(name string) (*api.Network, string, error) {
	f.reads++
	switch f.mode {
	case "denied":
		return nil, "", api.StatusErrorf(http.StatusForbidden, "sensitive denial")
	case "lookup failure":
		return nil, "", errors.New("sensitive transport failure")
	case "boundary race":
		if f.reads == 1 {
			return f.network, f.etag, nil
		}
		changed := *f.network
		changed.Config = maps.Clone(f.network.Config)
		changed.Config["user.operator"] = "changed"
		return &changed, "etag-2", nil
	case "absent race":
		if f.reads == 1 {
			return nil, "", api.StatusErrorf(http.StatusNotFound, "not found")
		}
		return f.network, f.etag, nil
	}
	if f.network == nil {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "not found")
	}
	if f.network.Name != name {
		return nil, "", api.StatusErrorf(http.StatusNotFound, "not found")
	}
	return f.network, f.etag, nil
}

func (f *fakeNetworkClient) CreateNetwork(request api.NetworksPost) error {
	f.creates++
	f.created = request
	if f.mode == "create failure" {
		return errors.New("sensitive transport failure")
	}
	f.network = &api.Network{
		Name:       request.Name,
		Type:       request.Type,
		Project:    f.scope.Project(),
		NetworkPut: api.NetworkPut{Config: maps.Clone(request.Config)},
	}
	f.etag = "etag-created"
	if f.mode == "postflight drift" {
		f.network.Config["user.aginctus.resource"] = "someone-else"
	}
	if f.cancel != nil {
		f.cancel()
	}
	return nil
}

func (f *fakeNetworkClient) UpdateNetwork(name string, network api.NetworkPut, etag string) error {
	f.updates++
	f.updated = network
	if f.mode == "update failure" {
		return errors.New("sensitive transport failure")
	}
	// The Incus update replaces the whole writable network document.
	f.network.NetworkPut = api.NetworkPut{
		Config:      maps.Clone(network.Config),
		Description: network.Description,
	}
	f.etag = "etag-updated"
	if f.cancel != nil {
		f.cancel()
	}
	return nil
}

func (f *fakeNetworkClient) DeleteNetwork(name string) error {
	f.deletes++
	if f.mode == "delete failure" {
		return errors.New("sensitive transport failure")
	}
	f.network = nil
	f.etag = ""
	return nil
}

func managementIntent(t *testing.T) NetworkIntent {
	t.Helper()
	scope, err := incusscope.NewHostSharedScope("management-network")
	if err != nil {
		t.Fatal(err)
	}
	return NetworkIntent{
		Scope: scope,
		Name:  "aginctus-mgmt",
		Type:  "bridge",
		Config: map[string]string{
			"ipv4.address":      "auto",
			"ipv4.dhcp.gateway": "none",
			"ipv4.nat":          "false",
			"ipv4.routing":      "false",
			"ipv6.address":      "none",
		},
	}
}

func ownedNetwork(t *testing.T, intent NetworkIntent) *api.Network {
	t.Helper()
	config := maps.Clone(intent.desiredConfig())
	config["user.operator"] = "preserved"
	return &api.Network{
		Name:       intent.Name,
		Type:       intent.Type,
		Project:    intent.Scope.Project(),
		NetworkPut: api.NetworkPut{Config: config},
	}
}

func TestNativeNetworkReconcileCreatesAbsentBridge(t *testing.T) {
	intent := managementIntent(t)
	fake := &fakeNetworkClient{scope: intent.Scope}
	result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureNetworkWithClient() error = %v", err)
	}
	if result.Action != Create || !result.Changed || !result.Verified {
		t.Fatalf("unexpected create result: %+v", result)
	}
	if fake.creates != 1 || fake.updates != 0 || fake.deletes != 0 {
		t.Fatalf("unexpected mutation counts: %+v", fake)
	}
	if fake.created.Name != intent.Name || fake.created.Type != "bridge" {
		t.Fatalf("unexpected create request: %+v", fake.created)
	}
	if !maps.Equal(fake.created.Config, intent.desiredConfig()) {
		t.Fatalf("create omitted ownership markers: %v", fake.created.Config)
	}
}

func TestNativeNetworkReconcileIsIdempotent(t *testing.T) {
	intent := managementIntent(t)
	fake := &fakeNetworkClient{scope: intent.Scope, network: ownedNetwork(t, intent)}
	for range 2 {
		result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
		if err != nil {
			t.Fatalf("EnsureNetworkWithClient() error = %v", err)
		}
		if result.Action != Unchanged || result.Changed || !result.Verified {
			t.Fatalf("unexpected converged result: %+v", result)
		}
	}
	if fake.creates != 0 || fake.updates != 0 {
		t.Fatalf("converged bridge was mutated: %+v", fake)
	}
}

func TestNativeNetworkReconcileUpdatesOwnedBridgeAndPreservesUnmanagedKeys(t *testing.T) {
	intent := managementIntent(t)
	existing := ownedNetwork(t, intent)
	existing.Config["ipv4.nat"] = "true"
	existing.Config["dns.mode"] = "managed"
	fake := &fakeNetworkClient{scope: intent.Scope, network: existing, etag: "etag-1"}
	result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureNetworkWithClient() error = %v", err)
	}
	if result.Action != Update || !result.Changed || !result.Verified {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if fake.updates != 1 || fake.creates != 0 {
		t.Fatalf("unexpected mutation counts: %+v", fake)
	}
	if fake.updated.Config["ipv4.nat"] != "false" {
		t.Fatalf("managed key not reconciled: %v", fake.updated.Config)
	}
	if fake.updated.Config["dns.mode"] != "managed" || fake.updated.Config["user.operator"] != "preserved" {
		t.Fatalf("unmanaged keys were not preserved: %v", fake.updated.Config)
	}
	if fake.updated.Config["user.aginctus.managed"] != "true" {
		t.Fatalf("ownership markers were not retained: %v", fake.updated.Config)
	}
}

func TestNativeNetworkReconcileRefusesForeignBridge(t *testing.T) {
	intent := managementIntent(t)
	foreign := &api.Network{
		Name: intent.Name, Type: intent.Type, Project: intent.Scope.Project(),
		NetworkPut: api.NetworkPut{Config: map[string]string{"ipv4.address": "10.0.0.1/24"}},
	}
	fake := &fakeNetworkClient{scope: intent.Scope, network: foreign, etag: "etag-1"}
	_, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	if err == nil {
		t.Fatal("foreign bridge was not refused")
	}
	if fake.creates != 0 || fake.updates != 0 || fake.deletes != 0 {
		t.Fatalf("foreign bridge was mutated: %+v", fake)
	}
	var failure *ReconcileError
	if !errors.As(err, &failure) {
		t.Fatalf("unexpected error type: %v", err)
	}
}

func TestNativeNetworkReconcileRequiresExplicitAdoption(t *testing.T) {
	intent := managementIntent(t)
	foreign := &api.Network{
		Name: intent.Name, Type: intent.Type, Project: intent.Scope.Project(),
		NetworkPut: api.NetworkPut{Config: map[string]string{
			"ipv4.address":      "10.0.0.1/24",
			"ipv4.dhcp.gateway": "none",
		}},
	}
	fake := &fakeNetworkClient{scope: intent.Scope, network: foreign, etag: "etag-1"}
	intent.Adopt = NetworkAdoption{Authorized: true}
	result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureNetworkWithClient() error = %v", err)
	}
	if result.Ownership != "adopted" || !result.Changed {
		t.Fatalf("explicit adoption was not reported: %+v", result)
	}
	if fake.updated.Config["user.aginctus.managed"] != "true" || fake.updated.Config["user.aginctus.resource"] != "management-network" {
		t.Fatalf("adoption did not write ownership markers: %v", fake.updated.Config)
	}
}

func TestNativeNetworkReconcileRefusesImmutableDrift(t *testing.T) {
	intent := managementIntent(t)
	for _, mode := range []string{"type drift", "policy drift"} {
		t.Run(mode, func(t *testing.T) {
			existing := ownedNetwork(t, intent)
			switch mode {
			case "type drift":
				existing.Type = "macvlan"
			case "policy drift":
				existing.Config["ipv4.dhcp.gateway"] = "10.10.0.1"
			}
			fake := &fakeNetworkClient{scope: intent.Scope, network: existing, etag: "etag-1"}
			_, err := EnsureNetworkWithClient(context.Background(), intent, fake)
			if err == nil {
				t.Fatalf("%s was not refused", mode)
			}
			if fake.creates != 0 || fake.updates != 0 {
				t.Fatalf("%s caused a mutation: %+v", mode, fake)
			}
		})
	}
}

func TestNativeNetworkReconcilePreservesNetworkDescription(t *testing.T) {
	intent := managementIntent(t)
	existing := ownedNetwork(t, intent)
	existing.Description = "operator supplied description"
	existing.Config["ipv4.nat"] = "true"
	fake := &fakeNetworkClient{scope: intent.Scope, network: existing, etag: "etag-1"}
	result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureNetworkWithClient() error = %v", err)
	}
	if result.Action != Update || !result.Changed {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if fake.updated.Description != "operator supplied description" {
		t.Fatalf("update did not preserve the network description: %q", fake.updated.Description)
	}
	if fake.network.Description != "operator supplied description" {
		t.Fatalf("network description was lost: %q", fake.network.Description)
	}
}

func TestNativeNetworkReconcileRefusesPolicyDriftEvenWithAdoption(t *testing.T) {
	intent := managementIntent(t)
	owned := ownedNetwork(t, intent)
	owned.Config["ipv4.dhcp.gateway"] = "10.10.0.1"
	foreign := &api.Network{
		Name: intent.Name, Type: intent.Type, Project: intent.Scope.Project(),
		NetworkPut: api.NetworkPut{Config: map[string]string{
			"ipv4.address":      "10.0.0.1/24",
			"ipv4.dhcp.gateway": "10.10.0.1",
		}},
	}
	for name, observed := range map[string]*api.Network{"owned": owned, "foreign": foreign} {
		t.Run(name, func(t *testing.T) {
			authorized := managementIntent(t)
			authorized.Adopt = NetworkAdoption{Authorized: true}
			fake := &fakeNetworkClient{scope: authorized.Scope, network: observed, etag: "etag-1"}
			_, err := EnsureNetworkWithClient(context.Background(), authorized, fake)
			if err == nil {
				t.Fatal("DHCP gateway policy drift was not refused")
			}
			if fake.creates != 0 || fake.updates != 0 || fake.deletes != 0 {
				t.Fatalf("policy drift caused a mutation: %+v", fake)
			}
			var failure *ReconcileError
			if !errors.As(err, &failure) || failure.Stage != "planning" {
				t.Fatalf("unexpected failure report: %v", err)
			}
		})
	}
}

func TestNativeNetworkRemovalRefusesMismatchedType(t *testing.T) {
	intent := managementIntent(t)
	existing := ownedNetwork(t, intent)
	existing.Type = "macvlan"
	for _, authorized := range []bool{false, true} {
		t.Run(fmt.Sprintf("authorized=%t", authorized), func(t *testing.T) {
			selected := managementIntent(t)
			selected.Adopt = NetworkAdoption{Authorized: authorized}
			fake := &fakeNetworkClient{scope: selected.Scope, network: existing, etag: "etag-1"}
			_, err := RemoveNetworkWithClient(context.Background(), selected, fake)
			if err == nil {
				t.Fatal("removal of a network of another type was not refused")
			}
			if fake.deletes != 0 {
				t.Fatalf("network of another type was deleted: %+v", fake)
			}
			var failure *ReconcileError
			if !errors.As(err, &failure) || failure.Stage != "planning" {
				t.Fatalf("unexpected failure report: %v", err)
			}
		})
	}
}

func TestNativeNetworkRemovalDoesNotReportVerifiedAbsenceWhenBridgeAppears(t *testing.T) {
	intent := managementIntent(t)
	fake := &fakeNetworkClient{scope: intent.Scope, network: ownedNetwork(t, intent), etag: "etag-1", mode: "absent race"}
	result, err := RemoveNetworkWithClient(context.Background(), intent, fake)
	if err == nil {
		t.Fatal("a bridge that appeared after the first read was reported as absent")
	}
	if result.Verified {
		t.Fatalf("removal reported a verified absence: %+v", result)
	}
	if fake.deletes != 0 {
		t.Fatalf("a bridge that appeared was deleted: %+v", fake)
	}
	var failure *ReconcileError
	if !errors.As(err, &failure) || failure.Stage != "mutation-boundary" {
		t.Fatalf("unexpected failure report: %v", err)
	}
}

func TestNativeNetworkReconcileFailsClosedOnUnexpectedChange(t *testing.T) {
	intent := managementIntent(t)
	fake := &fakeNetworkClient{scope: intent.Scope, network: ownedNetwork(t, intent), etag: "etag-1", mode: "boundary race"}
	_, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	if err == nil {
		t.Fatal("unexpected change between planning and execution was not detected")
	}
	if fake.creates != 0 || fake.updates != 0 || fake.deletes != 0 {
		t.Fatalf("unexpected change caused a mutation: %+v", fake)
	}
	var failure *ReconcileError
	if !errors.As(err, &failure) || failure.Stage != "mutation-boundary" {
		t.Fatalf("unexpected failure report: %v", err)
	}
}

func TestNativeNetworkReconcileRequiresLocalProjectScope(t *testing.T) {
	intent := managementIntent(t)
	for _, mode := range []string{"remote", "wrong project"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeNetworkClient{scope: intent.Scope, mode: mode}
			_, err := EnsureNetworkWithClient(context.Background(), intent, fake)
			if err == nil {
				t.Fatalf("%s transport was not refused", mode)
			}
			if fake.creates != 0 || fake.updates != 0 {
				t.Fatalf("%s transport caused a mutation: %+v", mode, fake)
			}
		})
	}
}

func TestNativeNetworkReconcileRefusesDeniedObservation(t *testing.T) {
	intent := managementIntent(t)
	for _, mode := range []string{"denied", "lookup failure"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeNetworkClient{scope: intent.Scope, mode: mode}
			_, err := EnsureNetworkWithClient(context.Background(), intent, fake)
			if err == nil {
				t.Fatalf("%s observation was not refused", mode)
			}
			if fake.creates != 0 || fake.updates != 0 {
				t.Fatalf("%s observation caused a mutation: %+v", mode, fake)
			}
		})
	}
}

func TestNativeNetworkReconcileReportsUncertainCompletion(t *testing.T) {
	intent := managementIntent(t)
	for _, mode := range []string{"create failure", "update failure"} {
		t.Run(mode, func(t *testing.T) {
			existing := ownedNetwork(t, intent)
			if mode == "update failure" {
				existing.Config["ipv4.nat"] = "true"
			}
			fake := &fakeNetworkClient{scope: intent.Scope, network: existing, etag: "etag-1", mode: mode}
			if mode == "create failure" {
				fake.network = nil
			}
			result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
			var failure *ReconcileError
			if !errors.As(err, &failure) {
				t.Fatalf("unexpected error type: %v", err)
			}
			if !failure.Mutated || !failure.Uncertain || result.Verified {
				t.Fatalf("uncertain completion was not reported: %+v, %+v", failure, result)
			}
		})
	}
}

func TestNativeNetworkReconcileReportsPostflightDrift(t *testing.T) {
	intent := managementIntent(t)
	fake := &fakeNetworkClient{scope: intent.Scope, mode: "postflight drift"}
	result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	var failure *ReconcileError
	if !errors.As(err, &failure) {
		t.Fatalf("unexpected error type: %v", err)
	}
	if failure.Stage != "verification" || !failure.Uncertain || result.Verified {
		t.Fatalf("postflight drift was not reported: %+v, %+v", failure, result)
	}
}

func TestNativeNetworkReconcileHonorsCancellation(t *testing.T) {
	intent := managementIntent(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fakeNetworkClient{scope: intent.Scope}
	_, err := EnsureNetworkWithClient(ctx, intent, fake)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not reported: %v", err)
	}
	if fake.creates != 0 || fake.updates != 0 {
		t.Fatalf("cancellation before mutation caused a write: %+v", fake)
	}
}

func TestNativeNetworkReconcileReportsCancellationDuringMutation(t *testing.T) {
	intent := managementIntent(t)
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeNetworkClient{scope: intent.Scope, cancel: cancel}
	result, err := EnsureNetworkWithClient(ctx, intent, fake)
	if err == nil {
		t.Fatal("cancellation during mutation was not reported")
	}
	if fake.creates != 1 {
		t.Fatalf("mutation did not reach the client: %+v", fake)
	}
	if result.Verified {
		t.Fatal("cancelled reconciliation reported success")
	}
	cancel()
}

func TestNativeNetworkRemovalRefusesForeignBridge(t *testing.T) {
	intent := managementIntent(t)
	foreign := &api.Network{
		Name: intent.Name, Type: intent.Type, Project: intent.Scope.Project(),
		NetworkPut: api.NetworkPut{Config: map[string]string{"ipv4.address": "10.0.0.1/24"}},
	}
	fake := &fakeNetworkClient{scope: intent.Scope, network: foreign, etag: "etag-1"}
	_, err := RemoveNetworkWithClient(context.Background(), intent, fake)
	if err == nil {
		t.Fatal("foreign bridge removal was not refused")
	}
	if fake.deletes != 0 {
		t.Fatalf("foreign bridge was deleted: %+v", fake)
	}
}

func TestNativeNetworkRemovalDeletesOwnedBridgeAndToleratesAbsence(t *testing.T) {
	intent := managementIntent(t)
	fake := &fakeNetworkClient{scope: intent.Scope, network: ownedNetwork(t, intent), etag: "etag-1"}
	result, err := RemoveNetworkWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("RemoveNetworkWithClient() error = %v", err)
	}
	if !result.Changed || !result.Verified || fake.deletes != 1 {
		t.Fatalf("unexpected removal result: %+v, %+v", result, fake)
	}
	absent := &fakeNetworkClient{scope: intent.Scope}
	result, err = RemoveNetworkWithClient(context.Background(), intent, absent)
	if err != nil {
		t.Fatalf("RemoveNetworkWithClient() error = %v", err)
	}
	if result.Changed || !result.Verified || absent.deletes != 0 {
		t.Fatalf("absent removal was not a verified no-op: %+v, %+v", result, absent)
	}
}

func TestNativeNetworkRemovalReportsUncertainCompletion(t *testing.T) {
	intent := managementIntent(t)
	fake := &fakeNetworkClient{scope: intent.Scope, network: ownedNetwork(t, intent), etag: "etag-1", mode: "delete failure"}
	result, err := RemoveNetworkWithClient(context.Background(), intent, fake)
	var failure *ReconcileError
	if !errors.As(err, &failure) {
		t.Fatalf("unexpected error type: %v", err)
	}
	if !failure.Mutated || !failure.Uncertain || result.Verified {
		t.Fatalf("uncertain removal was not reported: %+v, %+v", failure, result)
	}
}

func TestNativeNetworkIntentValidation(t *testing.T) {
	intent := managementIntent(t)
	if err := intent.Validate(); err != nil {
		t.Fatalf("valid intent rejected: %v", err)
	}
	missing := managementIntent(t)
	delete(missing.Config, "ipv4.nat")
	if err := missing.Validate(); err == nil {
		t.Fatal("intent missing a managed key was accepted")
	}
	extra := managementIntent(t)
	extra.Config["dns.mode"] = "managed"
	if err := extra.Validate(); err == nil {
		t.Fatal("intent managing an unmanaged key was accepted")
	}
	renamed := managementIntent(t)
	renamed.Name = "../other"
	if err := renamed.Validate(); err == nil {
		t.Fatal("intent with an unsafe name was accepted")
	}
	typed := managementIntent(t)
	typed.Type = "macvlan"
	if err := typed.Validate(); err == nil {
		t.Fatal("intent with an unsupported type was accepted")
	}
}

func TestNativeNetworkPlanReportsManagedChangesOnly(t *testing.T) {
	intent := managementIntent(t)
	observed := &NetworkObservation{
		Name: intent.Name, Type: intent.Type, Project: intent.Scope.Project(),
		Config: map[string]string{
			"ipv4.address":           "auto",
			"ipv4.dhcp.gateway":      "none",
			"ipv4.nat":               "false",
			"ipv4.routing":           "false",
			"ipv6.address":           "none",
			"user.aginctus.managed":  "true",
			"user.aginctus.resource": "management-network",
			"dns.mode":               "managed",
		},
	}
	plan, err := PlanNetwork(intent, observed)
	if err != nil {
		t.Fatalf("PlanNetwork() error = %v", err)
	}
	if plan.Action != Unchanged || len(plan.Changes) != 0 {
		t.Fatalf("converged bridge produced changes: %+v", plan)
	}
	observed.Config["ipv4.nat"] = "true"
	plan, err = PlanNetwork(intent, observed)
	if err != nil {
		t.Fatalf("PlanNetwork() error = %v", err)
	}
	if plan.Action != Update || len(plan.Changes) != 1 || plan.Changes[0].Field != "config.ipv4.nat" {
		t.Fatalf("unexpected change set: %+v", plan)
	}
	if plan.WriteAuthorized {
		t.Fatal("plan claimed write authority")
	}
}

func TestNativeNetworkPlanRefusesForeignBridgeWithoutAdoption(t *testing.T) {
	intent := managementIntent(t)
	observed := &NetworkObservation{
		Name: intent.Name, Type: intent.Type, Project: intent.Scope.Project(),
		Config: map[string]string{"ipv4.address": "10.0.0.1/24"},
	}
	if _, err := PlanNetwork(intent, observed); err == nil {
		t.Fatal("foreign bridge produced a plan")
	}
	if _, err := PlanNetworkRemoval(intent, observed); err == nil {
		t.Fatal("foreign bridge produced a removal plan")
	}
}

func TestNativeNetworkPlanRefusesForeignProject(t *testing.T) {
	intent := managementIntent(t)
	observed := &NetworkObservation{
		Name: intent.Name, Type: intent.Type, Project: "someone-else",
		Config: intent.desiredConfig(),
	}
	if _, err := PlanNetwork(intent, observed); err == nil {
		t.Fatal("bridge from another project produced a plan")
	}
}

func TestNativeNetworkAcceptsIncusResolvedAutomaticAddress(t *testing.T) {
	intent := managementIntent(t)
	observed := &NetworkObservation{
		Name: intent.Name, Type: intent.Type, Project: intent.Scope.Project(),
		Config: map[string]string{
			"ipv4.address":           "10.231.33.1/24",
			"ipv4.dhcp.gateway":      "none",
			"ipv4.nat":               "false",
			"ipv4.routing":           "false",
			"ipv6.address":           "none",
			"user.aginctus.managed":  "true",
			"user.aginctus.resource": "management-network",
		},
	}
	plan, err := PlanNetwork(intent, observed)
	if err != nil {
		t.Fatalf("PlanNetwork() error = %v", err)
	}
	if plan.Action != Unchanged || len(plan.Changes) != 0 {
		t.Fatalf("resolved automatic address produced changes: %+v", plan)
	}
	if err := verifyNetwork(intent, observed); err != nil {
		t.Fatalf("resolved automatic address did not converge: %v", err)
	}
}

func TestNativeNetworkReconcileKeepsIncusResolvedAutomaticAddress(t *testing.T) {
	intent := managementIntent(t)
	existing := ownedNetwork(t, intent)
	existing.Config["ipv4.address"] = "10.231.33.1/24"
	fake := &fakeNetworkClient{scope: intent.Scope, network: existing, etag: "etag-1"}
	result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureNetworkWithClient() error = %v", err)
	}
	if result.Action != Unchanged || result.Changed {
		t.Fatalf("resolved automatic address was rewritten: %+v", result)
	}
	if fake.updates != 0 {
		t.Fatalf("resolved automatic address caused a write: %+v", fake)
	}
}

func TestNativeNetworkReconcileUpdatesAutomaticAddressWhenPolicyChanges(t *testing.T) {
	intent := managementIntent(t)
	intent.Config["ipv4.address"] = "10.42.0.1/24"
	existing := ownedNetwork(t, intent)
	existing.Config["ipv4.address"] = "10.231.33.1/24"
	fake := &fakeNetworkClient{scope: intent.Scope, network: existing, etag: "etag-1"}
	result, err := EnsureNetworkWithClient(context.Background(), intent, fake)
	if err != nil {
		t.Fatalf("EnsureNetworkWithClient() error = %v", err)
	}
	if result.Action != Update || !result.Changed {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if fake.updated.Config["ipv4.address"] != "10.42.0.1/24" {
		t.Fatalf("automatic address was not reconciled: %v", fake.updated.Config)
	}
}
