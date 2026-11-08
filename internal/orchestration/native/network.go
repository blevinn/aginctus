package native

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// networkManagedKeys are the only Incus network config keys Aginctus writes.
// Every other observed key belongs to Incus or the operator and is preserved
// verbatim across updates.
var networkManagedKeys = []string{
	"ipv4.address",
	"ipv4.dhcp.gateway",
	"ipv4.nat",
	"ipv4.routing",
	"ipv6.address",
}

// NetworkPolicyKey carries the immutable DHCP announcement policy of the
// management bridge. A differing value on an existing bridge is policy drift
// that Aginctus refuses to migrate implicitly.
const NetworkPolicyKey = "ipv4.dhcp.gateway"

// autoAddressKeys are config keys whose desired "auto" value is resolved by
// Incus into a concrete address. The resolved address satisfies the intent:
// writing "auto" back would ask Incus to reallocate it.
var autoAddressKeys = map[string]bool{
	"ipv4.address": true,
	"ipv6.address": true,
}

// keySatisfied reports whether an observed config value satisfies a desired
// one. An automatically assigned address is satisfied by the address Incus
// allocated for it.
func keySatisfied(key, desired, observed string) bool {
	if desired == observed {
		return true
	}
	return desired == "auto" && autoAddressKeys[key] && observed != "" && observed != "none"
}

// NetworkAdoption is the explicit authorization to take over a network that
// does not carry Aginctus ownership markers. It is never implied by a name
// match and never granted by a dry run. It covers the ownership markers
// only: it never waives the immutable DHCP announcement policy, and it
// never authorizes the removal of a network of another type.
type NetworkAdoption struct {
	Authorized bool
}

// NetworkIntent is the typed desired state of the Aginctus management
// bridge. Config holds managed keys only; unmanaged keys are never part of
// the intent and therefore never removed.
type NetworkIntent struct {
	Scope  incusscope.HostSharedScope
	Name   string
	Type   string
	Config map[string]string
	Adopt  NetworkAdoption
}

// Validate refuses an intent that does not name every managed key. An
// omitted key is an unstated policy, not an "unchanged" policy.
func (intent NetworkIntent) Validate() error {
	if intent.Scope.Resource() != "management-network" {
		return fmt.Errorf("network intent is not a management bridge")
	}
	if !localName(intent.Name) {
		return fmt.Errorf("network name must be an explicit local name")
	}
	if intent.Type != "bridge" {
		return fmt.Errorf("unsupported network type")
	}
	for _, key := range networkManagedKeys {
		if _, ok := intent.Config[key]; !ok {
			return fmt.Errorf("network intent is missing managed key %q", key)
		}
	}
	for key := range intent.Config {
		if !slices.Contains(networkManagedKeys, key) {
			return fmt.Errorf("network intent manages unsupported key %q", key)
		}
	}
	return nil
}

// desiredConfig is the complete config Aginctus writes for the bridge.
func (intent NetworkIntent) desiredConfig() map[string]string {
	desired := map[string]string{}
	maps.Copy(desired, intent.Config)
	maps.Copy(desired, intent.Scope.Ownership())
	return desired
}

// NetworkObservation is the observed state of one bridge. It carries no SDK
// types and confers no authority to write.
type NetworkObservation struct {
	Name    string
	Type    string
	Project string
	// Description is an operator-owned writable field. Aginctus never
	// manages it, so a reconcile must send it back unchanged.
	Description string
	Config      map[string]string
}

// NetworkPlan is the read-only outcome of comparing intent with observation.
type NetworkPlan struct {
	Identity        string        `json:"identity"`
	Action          Action        `json:"action"`
	Changes         []FieldChange `json:"changes"`
	Ownership       string        `json:"ownership"`
	WriteAuthorized bool          `json:"writeAuthorized"`
}

// NetworkRemovalPlan is the read-only outcome of comparing intent with the
// observed network for a removal.
type NetworkRemovalPlan struct {
	Identity        string `json:"identity"`
	Action          Action `json:"action"`
	Ownership       string `json:"ownership"`
	Present         bool   `json:"present"`
	WriteAuthorized bool   `json:"writeAuthorized"`
}

// NetworkResult reports what the reconciler did and what it verified.
type NetworkResult struct {
	Network   string `json:"network"`
	Action    Action `json:"action"`
	Ownership string `json:"ownership"`
	Changed   bool   `json:"changed"`
	Verified  bool   `json:"verified"`
}

// NetworkClient is the narrow Incus surface used to reconcile one bridge.
// It cannot reach profiles, instances, storage or any other project.
type NetworkClient interface {
	GetConnectionInfo() (*incus.ConnectionInfo, error)
	GetNetwork(name string) (*api.Network, string, error)
	CreateNetwork(network api.NetworksPost) error
	UpdateNetwork(name string, network api.NetworkPut, etag string) error
	DeleteNetwork(name string) error
}

// PlanNetwork compares intent with observation without touching Incus. A
// foreign network is refused rather than adopted unless adoption was
// explicitly authorized. Unmanaged config keys never appear in the plan.
func PlanNetwork(intent NetworkIntent, observed *NetworkObservation) (NetworkPlan, error) {
	if err := intent.Validate(); err != nil {
		return NetworkPlan{}, err
	}
	plan := NetworkPlan{Identity: intent.Name, Action: Unchanged}
	desired := intent.desiredConfig()
	if observed == nil {
		plan.Action = Create
		plan.Ownership = "absent"
		plan.Changes = changedFields(desired, nil)
		return plan, nil
	}
	ownership, err := assessNetworkOwnership(intent, observed)
	if err != nil {
		return NetworkPlan{}, err
	}
	plan.Ownership = ownership
	if observed.Type != intent.Type {
		return NetworkPlan{}, fmt.Errorf("existing network type cannot be changed in place")
	}
	// The DHCP announcement is immutable policy. Aginctus writes it only
	// when it creates the bridge; an existing bridge must already carry
	// the configured announcement. Adoption authorization covers the
	// ownership markers only, so it never waives this policy: a bridge
	// that advertises a default route is never rewritten or adopted
	// implicitly.
	if observed.Config[NetworkPolicyKey] != intent.Config[NetworkPolicyKey] {
		return NetworkPlan{}, fmt.Errorf("existing network DHCP gateway policy differs")
	}
	plan.Changes = changedFields(desired, observed.Config)
	if len(plan.Changes) > 0 {
		plan.Action = Update
	}
	return plan, nil
}

// PlanNetworkRemoval compares intent with observation for a removal. It
// refuses a foreign network unless adoption was explicitly authorized.
func PlanNetworkRemoval(intent NetworkIntent, observed *NetworkObservation) (NetworkRemovalPlan, error) {
	if err := intent.Validate(); err != nil {
		return NetworkRemovalPlan{}, err
	}
	plan := NetworkRemovalPlan{Identity: intent.Name, Action: Delete}
	if observed == nil {
		plan.Ownership = "absent"
		return plan, nil
	}
	ownership, err := assessNetworkOwnership(intent, observed)
	if err != nil {
		return NetworkRemovalPlan{}, err
	}
	plan.Ownership = ownership
	if observed.Type != intent.Type {
		// This reconciler owns the management bridge and nothing else.
		// A network of another type is never a removal target, even
		// with explicit adoption authorization.
		return NetworkRemovalPlan{}, fmt.Errorf("existing network type does not match the management bridge")
	}
	plan.Present = true
	return plan, nil
}

// assessNetworkOwnership reports how the observed network relates to the
// intent. A network that only shares the name is foreign.
func assessNetworkOwnership(intent NetworkIntent, observed *NetworkObservation) (string, error) {
	if observed.Name != intent.Name {
		return "", fmt.Errorf("observed network identity mismatch")
	}
	if observed.Project != "" && observed.Project != intent.Scope.Project() {
		return "", fmt.Errorf("observed network belongs to another project")
	}
	if intent.Scope.AssessOwnership(observed.Config) == incusscope.HostOwned {
		return "owned", nil
	}
	if !intent.Adopt.Authorized {
		return "", fmt.Errorf("existing network is not owned by Aginctus")
	}
	return "adopted", nil
}

// changedFields lists the managed keys whose observed value differs. Values
// are never included: they can carry operator data.
func changedFields(desired map[string]string, observed map[string]string) []FieldChange {
	changes := make([]FieldChange, 0, len(desired))
	for _, key := range slices.Sorted(maps.Keys(desired)) {
		if observed == nil || !keySatisfied(key, desired[key], observed[key]) {
			changes = append(changes, FieldChange{Field: "config." + key})
		}
	}
	return changes
}

// PlanLocalNetwork observes the local Incus daemon and returns a read-only
// plan. It performs no mutation and is the only path a dry run may use.
func PlanLocalNetwork(ctx context.Context, intent NetworkIntent) (NetworkPlan, error) {
	client, cancel, err := connectLocalNetwork(ctx, intent)
	if err != nil {
		return NetworkPlan{}, err
	}
	defer cancel()
	defer client.Disconnect()
	observed, etag, err := observeNetwork(ctx, client, intent.Name)
	if err != nil {
		return NetworkPlan{}, &ReconcileError{Stage: "observation", cause: err}
	}
	plan, err := PlanNetwork(intent, observed)
	if err != nil {
		return NetworkPlan{}, &ReconcileError{Stage: "planning", cause: err}
	}
	if err := recheckNetwork(ctx, client, intent.Name, observed, etag); err != nil {
		return NetworkPlan{}, &ReconcileError{Stage: "observation", cause: err}
	}
	return plan, nil
}

// PlanLocalNetworkRemoval observes the local Incus daemon and returns a
// read-only removal plan. It performs no mutation.
func PlanLocalNetworkRemoval(ctx context.Context, intent NetworkIntent) (NetworkRemovalPlan, error) {
	client, cancel, err := connectLocalNetwork(ctx, intent)
	if err != nil {
		return NetworkRemovalPlan{}, err
	}
	defer cancel()
	defer client.Disconnect()
	observed, etag, err := observeNetwork(ctx, client, intent.Name)
	if err != nil {
		return NetworkRemovalPlan{}, &ReconcileError{Stage: "observation", cause: err}
	}
	plan, err := PlanNetworkRemoval(intent, observed)
	if err != nil {
		return NetworkRemovalPlan{}, &ReconcileError{Stage: "planning", cause: err}
	}
	if err := recheckNetwork(ctx, client, intent.Name, observed, etag); err != nil {
		return NetworkRemovalPlan{}, &ReconcileError{Stage: "observation", cause: err}
	}
	return plan, nil
}

// EnsureLocalNetwork reconciles the management bridge on the local daemon.
func EnsureLocalNetwork(ctx context.Context, intent NetworkIntent) (NetworkResult, error) {
	client, cancel, err := connectLocalNetwork(ctx, intent)
	if err != nil {
		return NetworkResult{}, err
	}
	defer cancel()
	defer client.Disconnect()
	return EnsureNetworkWithClient(ctx, intent, client)
}

// RemoveLocalNetwork removes the management bridge from the local daemon.
func RemoveLocalNetwork(ctx context.Context, intent NetworkIntent) (NetworkResult, error) {
	client, cancel, err := connectLocalNetwork(ctx, intent)
	if err != nil {
		return NetworkResult{}, err
	}
	defer cancel()
	defer client.Disconnect()
	return RemoveNetworkWithClient(ctx, intent, client)
}

// connectLocalNetwork opens a bounded local Incus connection bound to the
// host-shared project of the intent. The caller must hold the returned
// cancel function for as long as the connection is used.
func connectLocalNetwork(ctx context.Context, intent NetworkIntent) (incus.InstanceServer, context.CancelFunc, error) {
	if err := intent.Validate(); err != nil {
		return nil, nil, err
	}
	return connectLocal(ctx, intent.Scope.Project())
}

// EnsureNetworkWithClient reconciles one bridge: observe, plan, revalidate
// ownership and scope at the mutation boundary, apply the guarded mutation
// and verify convergence. Unmanaged config keys are preserved.
func EnsureNetworkWithClient(ctx context.Context, intent NetworkIntent, client NetworkClient) (NetworkResult, error) {
	if err := intent.Validate(); err != nil {
		return NetworkResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return NetworkResult{}, err
	}
	if err := requireLocalScope(client, intent); err != nil {
		return NetworkResult{}, &ReconcileError{Stage: "transport", cause: err}
	}
	observed, etag, err := observeNetwork(ctx, client, intent.Name)
	if err != nil {
		return NetworkResult{}, &ReconcileError{Stage: "observation", cause: err}
	}
	plan, err := PlanNetwork(intent, observed)
	if err != nil {
		return NetworkResult{}, &ReconcileError{Stage: "planning", cause: err}
	}
	result := NetworkResult{Network: intent.Name, Action: plan.Action, Ownership: plan.Ownership}
	// Recheck at the mutation boundary: the bridge may have appeared or
	// changed between the plan and the write.
	current, currentETag, err := observeNetwork(ctx, client, intent.Name)
	if err != nil {
		return result, &ReconcileError{Stage: "mutation-boundary", cause: err}
	}
	if !sameObservation(observed, current) || etag != currentETag {
		return result, &ReconcileError{Stage: "mutation-boundary", cause: fmt.Errorf("network changed during planning")}
	}
	switch plan.Action {
	case Unchanged:
	case Create:
		if err := ctx.Err(); err != nil {
			return result, &ReconcileError{Stage: "cancellation", cause: err}
		}
		request := api.NetworksPost{Name: intent.Name, Type: intent.Type, NetworkPut: api.NetworkPut{Config: intent.desiredConfig()}}
		if err := client.CreateNetwork(request); err != nil {
			return result, &ReconcileError{Stage: "create", Mutated: true, Uncertain: true, cause: err}
		}
		result.Changed = true
	case Update:
		if err := ctx.Err(); err != nil {
			return result, &ReconcileError{Stage: "cancellation", cause: err}
		}
		// Only the managed keys that actually differ are written. Every
		// other observed key belongs to Incus or the operator and is
		// preserved verbatim. The Incus update replaces the whole
		// writable network document, so the operator-owned description
		// is sent back unchanged.
		merged := maps.Clone(current.Config)
		if merged == nil {
			merged = map[string]string{}
		}
		for key, value := range intent.desiredConfig() {
			if !keySatisfied(key, value, current.Config[key]) {
				merged[key] = value
			}
		}
		request := api.NetworkPut{Config: merged, Description: current.Description}
		if err := client.UpdateNetwork(intent.Name, request, currentETag); err != nil {
			return result, &ReconcileError{Stage: "update", Mutated: true, Uncertain: true, cause: err}
		}
		result.Changed = true
	}
	after, _, err := observeNetwork(ctx, client, intent.Name)
	if err != nil {
		return result, &ReconcileError{Stage: "verification", Mutated: result.Changed, Uncertain: result.Changed, cause: err}
	}
	if err := verifyNetwork(intent, after); err != nil {
		return result, &ReconcileError{Stage: "verification", Mutated: result.Changed, Uncertain: result.Changed, cause: err}
	}
	result.Verified = true
	return result, nil
}

// RemoveNetworkWithClient removes one owned bridge. A foreign bridge is
// refused unless adoption was explicitly authorized.
func RemoveNetworkWithClient(ctx context.Context, intent NetworkIntent, client NetworkClient) (NetworkResult, error) {
	if err := intent.Validate(); err != nil {
		return NetworkResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return NetworkResult{}, err
	}
	if err := requireLocalScope(client, intent); err != nil {
		return NetworkResult{}, &ReconcileError{Stage: "transport", cause: err}
	}
	observed, etag, err := observeNetwork(ctx, client, intent.Name)
	if err != nil {
		return NetworkResult{}, &ReconcileError{Stage: "observation", cause: err}
	}
	plan, err := PlanNetworkRemoval(intent, observed)
	if err != nil {
		return NetworkResult{}, &ReconcileError{Stage: "planning", cause: err}
	}
	result := NetworkResult{Network: intent.Name, Action: plan.Action, Ownership: plan.Ownership}
	// Recheck at the mutation boundary. It also proves an absent bridge
	// is still absent: a bridge that appeared after the first read is
	// never reported as a verified absence.
	current, currentETag, err := observeNetwork(ctx, client, intent.Name)
	if err != nil {
		return result, &ReconcileError{Stage: "mutation-boundary", cause: err}
	}
	if !sameObservation(observed, current) || etag != currentETag {
		return result, &ReconcileError{Stage: "mutation-boundary", cause: fmt.Errorf("network changed during planning")}
	}
	if !plan.Present {
		result.Verified = true
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, &ReconcileError{Stage: "cancellation", cause: err}
	}
	if err := client.DeleteNetwork(intent.Name); err != nil {
		return result, &ReconcileError{Stage: "delete", Mutated: true, Uncertain: true, cause: err}
	}
	result.Changed = true
	after, _, err := observeNetwork(ctx, client, intent.Name)
	if err != nil {
		return result, &ReconcileError{Stage: "verification", Mutated: true, Uncertain: true, cause: err}
	}
	if after != nil {
		return result, &ReconcileError{Stage: "verification", Mutated: true, Uncertain: true, cause: fmt.Errorf("network still present after removal")}
	}
	result.Verified = true
	return result, nil
}

// requireLocalScope refuses any client that is not the local Incus socket
// bound to the host-shared project of the intent.
func requireLocalScope(client NetworkClient, intent NetworkIntent) error {
	info, err := client.GetConnectionInfo()
	if err != nil {
		return fmt.Errorf("native network reconciliation requires the local Incus socket")
	}
	return localTransport(info, intent.Scope.Project())
}

// observeNetwork reads one bridge. Absence is proven by an explicit
// not-found status; any other lookup failure is not an empty observation.
func observeNetwork(ctx context.Context, client NetworkClient, name string) (*NetworkObservation, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	network, etag, err := client.GetNetwork(name)
	if api.StatusErrorCheck(err, http.StatusNotFound) {
		return nil, "", nil
	}
	if err != nil || network == nil {
		return nil, "", fmt.Errorf("network observation unavailable")
	}
	return &NetworkObservation{
		Name:        network.Name,
		Type:        network.Type,
		Project:     network.Project,
		Description: network.Description,
		Config:      maps.Clone(network.Config),
	}, etag, nil
}

// recheckNetwork proves the observation still holds. It is a read-only
// snapshot comparison, not a lock.
func recheckNetwork(ctx context.Context, client NetworkClient, name string, observed *NetworkObservation, etag string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	again, afterETag, err := observeNetwork(ctx, client, name)
	if err != nil {
		return err
	}
	if !sameObservation(observed, again) || etag != afterETag {
		return fmt.Errorf("network changed during observation")
	}
	return nil
}

// sameObservation compares two read-only snapshots strictly.
func sameObservation(a, b *NetworkObservation) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Name != b.Name || a.Type != b.Type || a.Project != b.Project {
		return false
	}
	if a.Description != b.Description {
		return false
	}
	return maps.Equal(a.Config, b.Config)
}

// verifyNetwork proves that every managed key converged and that the
// ownership markers are present.
func verifyNetwork(intent NetworkIntent, observed *NetworkObservation) error {
	if observed == nil {
		return fmt.Errorf("network is absent after reconciliation")
	}
	for key, want := range intent.desiredConfig() {
		if !keySatisfied(key, want, observed.Config[key]) {
			// The key name is safe to report; its value may carry
			// operator data.
			return fmt.Errorf("network did not converge at managed key %q", key)
		}
	}
	return nil
}
