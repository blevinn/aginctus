package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func fakeCgroupFilesystem(t *testing.T) (*CgroupManager, func(string, os.FileMode) error) {
	t.Helper()
	root := t.TempDir()
	manager := &CgroupManager{root: root, limits: defaultVMLimits}
	mkdir := func(path string, mode os.FileMode) error {
		if err := os.Mkdir(path, mode); err != nil {
			return err
		}
		for _, name := range []string{
			"cpu.max", "memory.max", "memory.swap.max", "memory.oom.group",
			"pids.max", "cgroup.kill", "cgroup.events",
		} {
			value := ""
			if name == "cgroup.events" {
				value = "populated 0\nfrozen 0\n"
			}
			if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0600); err != nil {
				return err
			}
		}
		return nil
	}
	return manager, mkdir
}

func TestCgroupDelegationFailsClosed(t *testing.T) {
	for _, path := range []string{"", "/tmp", "/sys/fs/cgroup/../../etc", "relative", "/sys/fs/cgroup/missing-aginctus-worker"} {
		if _, err := OpenCgroupManager(path); err == nil {
			t.Errorf("accepted unavailable or unsafe delegated cgroup %q", path)
		}
	}
	var manager *CgroupManager
	if _, err := manager.Prepare("vm-aaaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		t.Fatal("nil cgroup manager accepted")
	}
}

func TestCgroupLimitsWrittenBeforeAttach(t *testing.T) {
	manager, mkdir := fakeCgroupFilesystem(t)
	id := "vm-aaaaaaaaaaaaaaaaaaaaaaaa"
	cg, err := manager.prepare(id, mkdir)
	if err != nil {
		t.Fatal(err)
	}
	defer cg.Close()
	for file, want := range map[string]string{
		"cpu.max": "200000 100000",
		"memory.max": "5368709120",
		"memory.swap.max": "0",
		"memory.oom.group": "1",
		"pids.max": "256",
	} {
		data, err := os.ReadFile(filepath.Join(manager.root, id, file))
		if err != nil || string(data) != want {
			t.Errorf("%s=%q want %q; error=%v", file, data, want, err)
		}
	}
	cmd := exec.Command("/bin/true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cg.Attach(cmd); err != nil {
		t.Fatal(err)
	}
	if !cmd.SysProcAttr.UseCgroupFD || cmd.SysProcAttr.CgroupFD != int(cg.fd.Fd()) || !cmd.SysProcAttr.Setsid {
		t.Fatalf("command was not atomically bound to run cgroup: %#v", cmd.SysProcAttr)
	}
	if err := cg.Attach(cmd); err == nil {
		t.Fatal("accepted duplicate command attachment")
	}
	if err := cg.KillAndVerify(); err != nil {
		t.Fatalf("failed to verify empty fake cgroup: %v", err)
	}
	kill, err := os.ReadFile(filepath.Join(manager.root, id, "cgroup.kill"))
	if err != nil || string(kill) != "1" {
		t.Fatalf("did not write cgroup.kill: %q %v", kill, err)
	}
	for _, file := range []string{"cpu.max", "memory.max", "memory.swap.max", "memory.oom.group", "pids.max", "cgroup.kill", "cgroup.events"} {
		if err := os.Remove(filepath.Join(manager.root, id, file)); err != nil {
			t.Fatal(err)
		}
	}
	if err := cg.Remove(); err != nil {
		t.Fatalf("failed to release empty cgroup: %v", err)
	}
}

func TestCgroupControlFilesCannotBeCreatedOrRedirected(t *testing.T) {
	manager, mkdir := fakeCgroupFilesystem(t)
	id := "vm-bbbbbbbbbbbbbbbbbbbbbbbb"
	// The cgroup interface must already provide every control file. Do not
	// silently create arbitrary regular files in a foreign path.
	broken := func(path string, mode os.FileMode) error {
		if err := mkdir(path, mode); err != nil {
			return err
		}
		return os.Remove(filepath.Join(path, "memory.max"))
	}
	if _, err := manager.prepare(id, broken); err == nil || !strings.Contains(err.Error(), "memory.max") {
		t.Fatalf("missing memory controller did not block launch: %v", err)
	}
	id = "vm-cccccccccccccccccccccccc"
	link := func(path string, _ os.FileMode) error {
		return os.Symlink(t.TempDir(), path)
	}
	if _, err := manager.prepare(id, link); err == nil {
		t.Fatal("accepted symlinked cgroup")
	}
}

func TestCgroupUnknownControlAndMissingFileRejected(t *testing.T) {
	manager, mkdir := fakeCgroupFilesystem(t)
	cg, err := manager.prepare("vm-dddddddddddddddddddddddd", mkdir)
	if err != nil {
		t.Fatal(err)
	}
	defer cg.Close()
	if err := cg.writeControl("../../etc/shadow", "destroy"); err == nil {
		t.Fatal("accepted arbitrary control file")
	}
	if _, err := cg.readControl("cgroup.procs"); err == nil {
		t.Fatal("accepted process enumeration as an arbitrary control file")
	}
}
