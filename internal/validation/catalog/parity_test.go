package catalog

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func readSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func manifestBody(t *testing.T, source, start, end string) string {
	t.Helper()
	pos := strings.Index(source, start)
	if pos == -1 {
		t.Fatalf("manifest %q not found", start)
	}
	body := source[pos+len(start):]
	stop := strings.Index(body, end)
	if stop == -1 {
		t.Fatalf("manifest %q is not terminated", start)
	}
	return body[:stop]
}

func quotedItems(body string) []string {
	pattern := regexp.MustCompile("\"([^\"]+)\"")
	var items []string
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		items = append(items, match[1])
	}
	return items
}

func requireSame(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s drift: got %q, want %q", what, got, want)
	}
}

func TestHermeticSuiteParity(t *testing.T) {
	var ids []string
	seen := make(map[string]bool)
	recipes := readSource(t, ".just/check.just")
	for _, suite := range HermeticSuites() {
		if suite.ID == "" || suite.Recipe != suite.ID || suite.Tier != Hermetic || seen[suite.ID] {
			t.Fatalf("invalid suite metadata: %#v", suite)
		}
		seen[suite.ID] = true
		ids = append(ids, suite.ID)
		pattern := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(suite.Recipe) + `:(?:[[:space:]]|$)`)
		if !pattern.MatchString(recipes) {
			t.Fatalf("just check recipe %q is missing", suite.Recipe)
		}
	}
	ci := readSource(t, "scripts/checks/ci_summary.py")
	requireSame(t, "Python report suite IDs", quotedItems(manifestBody(t, report, "SUITES = (", "
)")), ids)
	requireSame(t, "CI summary suite IDs", quotedItems(manifestBody(t, ci, "EXPECTED_SUITES = (", "
)")), ids)
}

func TestVMScenarioParity(t *testing.T) {
	runner := readSource(t, "scripts/vm_harness/runner.py")
	flake := readSource(t, "flake.nix")
	just := readSource(t, "justfile")
	evidence := readSource(t, "scripts/checks/vm_evidence.py")

	pythonScenarios := manifestBody(t, runner, "SCENARIOS = {", "
}")
	pattern := regexp.MustCompile(""([a-z0-9-]+)": "(vm-harness-[a-z0-9-]+)"")
	pythonTargets := make(map[string]string)
	for _, match := range pattern.FindAllStringSubmatch(pythonScenarios, -1) {
		if _, exists := pythonTargets[match[1]]; exists {
			t.Fatalf("duplicate Python VM scenario %q", match[1])
		}
		pythonTargets[match[1]] = match[2]
	}
	networked := quotedItems(manifestBody(t, runner, "NETWORKED_SCENARIOS = {", "
}"))
	evidenceScenarios := quotedItems(manifestBody(t, evidence, "SCENARIOS = (", "
)"))
	var catalogNetworked, public []string
	seen := make(map[string]bool)
	for _, scenario := range VMScenarios() {
		if scenario.Tier != Disposable || scenario.Description == "" || scenario.Name == "" || seen[scenario.Name] {
			t.Fatalf("invalid VM catalog metadata: %#v", scenario)
		}
		seen[scenario.Name] = true
		if pythonTargets[scenario.Name] != scenario.Target {
			t.Fatalf("Python target mismatch for %q: %q != %q", scenario.Name, pythonTargets[scenario.Name], scenario.Target)
		}
		if !strings.Contains(flake, scenario.Target+" =") {
			t.Fatalf("flake target %q missing", scenario.Target)
		}
		if scenario.Networked {
			catalogNetworked = append(catalogNetworked, scenario.Name)
		}
		switch scenario.Surface {
		case Operator:
			if !scenario.Evidence {
				t.Fatalf("operator batch scenario %q has no v1 evidence contract", scenario.Name)
			}
			public = append(public, scenario.Name)
			if !strings.Contains(just, "test-vm-"+scenario.Name+":") {
				t.Fatalf("missing operator just recipe for %q", scenario.Name)
			}
		case Internal:
			if scenario.Evidence {
				t.Fatalf("internal experiment %q unexpectedly accepted as evidence", scenario.Name)
			}
		default:
			t.Fatalf("unclassified VM surface for %q", scenario.Name)
		}
		delete(pythonTargets, scenario.Name)
	}
	if len(pythonTargets) > 0 {
		t.Fatalf("unclassified Python VM scenarios: %v", pythonTargets)
	}
	slices.Sort(catalogNetworked)
	slices.Sort(networked)
	requireSame(t, "networked VM selection", networked, catalogNetworked)
	slices.Sort(evidenceScenarios)
	slices.Sort(public)
	requireSame(t, "VM evidence allowlist", evidenceScenarios, public)

	// The runner unit test is not itself a disposable VM scenario.
	recipePattern := regexp.MustCompile("(?m)^test-vm-([a-z0-9-]+):")
	var recipes []string
	for _, match := range recipePattern.FindAllStringSubmatch(just, -1) {
		if match[1] != "runner" {
			recipes = append(recipes, match[1])
		}
	}
	slices.Sort(recipes)
	requireSame(t, "operator Just VM recipes", recipes, public)
}

func TestCatalogReturnsDefensiveCopies(t *testing.T) {
	suites := HermeticSuites()
	suites[0].ID = "corrupted"
	if HermeticSuites()[0].ID == "corrupted" {
		t.Fatal("hermetic catalog shared mutable backing array")
	}
	scenarios := VMScenarios()
	scenarios[0].Name = "corrupted"
	if VMScenarios()[0].Name == "corrupted" {
		t.Fatal("VM catalog shared mutable backing array")
	}
}

func TestMismatchIsDetected(t *testing.T) {
	expected := []string{"go", "python"}
	if slices.Equal([]string{"go", "python", "new-untested-suite"}, expected) {
		t.Fatal("new manifest entry escaped parity detection")
	}
}
