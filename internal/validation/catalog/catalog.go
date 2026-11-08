// Package catalog owns declarative validation metadata, not execution authority.
package catalog

// Tier classifies the resources and authority required to execute a check.
type Tier string

const (
	Hermetic   Tier = "hermetic"
	Disposable Tier = "disposable_vm"
)

// Surface distinguishes operator-discoverable batch scenarios from experiments.
// Neither value authorizes untrusted agents to invoke VM or host operations.
type Surface string

const (
	Operator Surface = "operator_batch"
	Internal Surface = "internal_feasibility"
)

// Suite is a fixed hermetic check invoked through an existing just recipe.
type Suite struct {
	ID     string
	Recipe string
	Tier   Tier
}

// Scenario is a pinned NixOS test. Evidence means the existing v1 VM evidence
// reader accepts its identity, not that the scenario has been executed.
type Scenario struct {
	Name        string
	Target      string
	Description string
	Tier        Tier
	Surface     Surface
	Networked   bool
	Evidence    bool
}

var hermetic = []Suite{
	{ID: "go", Recipe: "go", Tier: Hermetic},
	{ID: "lint", Recipe: "lint", Tier: Hermetic},
	{ID: "python", Recipe: "python", Tier: Hermetic},
	{ID: "shell-tests", Recipe: "shell-tests", Tier: Hermetic},
	{ID: "shell", Recipe: "shell", Tier: Hermetic},
	{ID: "markdown", Recipe: "markdown", Tier: Hermetic},
	{ID: "docs-examples", Recipe: "docs-examples", Tier: Hermetic},
	{ID: "nix", Recipe: "nix", Tier: Hermetic},
}

var scenarios = []Scenario{
	{Name: "smoke", Target: "vm-harness-smoke", Description: "Real guest Incus and Herdr image smoke", Tier: Disposable, Surface: Operator, Evidence: true},
	{Name: "storage-network", Target: "vm-harness-storage-network", Description: "Guest-local Btrfs storage and management bridge", Tier: Disposable, Surface: Operator, Evidence: true},
	{Name: "runtime-lifecycle", Target: "vm-harness-runtime-lifecycle", Description: "Real Herdr/OpenCode provisioning and reconciliation", Tier: Disposable, Surface: Operator, Evidence: true},
	{Name: "gateway-topology", Target: "vm-harness-gateway-topology", Description: "Real guest-local LiteLLM/Postgres and Herdr/OpenCode topology (OCI pulls required; no inference)", Tier: Disposable, Surface: Operator, Networked: true, Evidence: true},
	// Feasibility-only: runnable through Python, not exported by the Go CLI
	// or accepted by the existing v1 evidence normalizer.
	{Name: "native-oci", Target: "vm-harness-native-oci", Description: "Experimental native OCI feasibility spike", Tier: Disposable, Surface: Internal, Networked: true},
	{Name: "synthetic-gateway", Target: "vm-harness-synthetic-gateway", Description: "Real guest LiteLLM/Postgres with scoped-key synthetic Responses upstream (no agent tool proof)", Tier: Disposable, Surface: Operator, Networked: true, Evidence: true},
	{Name: "synthetic-tool-gateway", Target: "vm-harness-synthetic-tool-gateway", Description: "Four fake-result Responses tool turns through real LiteLLM (no tool execution)", Tier: Disposable, Surface: Operator, Networked: true, Evidence: true},
	{Name: "full-stack", Target: "vm-harness-full-stack", Description: "Real Herdr/OpenCode read/edit/bash through a scoped synthetic gateway (dedicated KVM worker only; no live provider)", Tier: Disposable, Surface: Operator, Networked: true, Evidence: true},
	{Name: "opencode-tool-loopback", Target: "vm-harness-opencode-tool-loopback", Description: "Guest-isolated pinned OpenCode executing scripted read/edit/Go test tools (no Herdr/gateway)", Tier: Disposable, Surface: Operator, Evidence: true},
	{Name: "ownership-recovery", Target: "vm-harness-ownership-recovery", Description: "Foreign resource denial and guest daemon recovery", Tier: Disposable, Surface: Operator, Evidence: true},
}

// HermeticSuites returns a defensive copy in the stable report order.
func HermeticSuites() []Suite {
	return append([]Suite(nil), hermetic...)
}

// VMScenarios returns all pinned scenarios, including internal experiments.
// Only PublicVMScenarios may be used for CLI discovery.
func VMScenarios() []Scenario {
	return append([]Scenario(nil), scenarios...)
}

func PublicVMScenarios() []Scenario {
	var result []Scenario
	for _, scenario := range scenarios {
		if scenario.Surface == Operator {
			result = append(result, scenario)
		}
	}
	return result
}
