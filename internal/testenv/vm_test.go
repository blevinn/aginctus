package testenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVMScenariosAreFixedAndIndependent(t *testing.T) {
	all := VMScenarios()
	if len(all) != 7 {
		t.Fatalf("expected seven pinned scenarios, got %d", len(all))
	}
	all[0].Name = "injected"
	found, err := VMScenario("smoke")
	if err != nil || found.Target != "vm-harness-smoke" {
		t.Fatalf("manifest changed: %#v, %v", found, err)
	}
	for _, invalid := range []string{"", "../smoke", "smoke;id", ".#vm-harness-smoke", "debug", "--unsafe"} {
		if _, err := VMScenario(invalid); err == nil {
			t.Errorf("accepted unlisted scenario %q", invalid)
		}
	}
}

func TestVMTestRequestFixedCommand(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "scripts", "vm_harness", "runner.py")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("pass"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range VMScenarios() {
		req, err := VMTestRequest(root, scenario.Name)
		if err != nil {
			t.Fatalf("%s: %v", scenario.Name, err)
		}
		if req.Program != "python3" || req.Dir != root || !reflect.DeepEqual(req.Args, []string{file, "test", scenario.Name}) {
			t.Errorf("%s: unexpected invocation %#v", scenario.Name, req)
		}
	}
}

func TestVMTestRequestRefusesUntrustedScriptPathAndScenario(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "scripts", "vm_harness", "runner.py")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := VMTestRequest(root, "smoke"); err == nil {
		t.Fatal("accepted missing runner")
	}
	if err := os.Symlink("/tmp/untrusted-script", file); err != nil {
		t.Fatal(err)
	}
	if _, err := VMTestRequest(root, "smoke"); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("accepted symlink runner: %v", err)
	}
	if _, err := VMTestRequest(root, "smoke --other"); err == nil {
		t.Fatal("accepted caller supplied arguments")
	}
	if _, err := VMTestRequest(root, "smoke"); err == nil {
		t.Fatal("symlink still accepted")
	}
}
