package testenv

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const cgroupCleanupTimeout = 4 * time.Second

// CgroupLimits bounds the *host* resource usage of the VM and everything
// remaining in its cgroup. Values are fixed in trusted supervisor code;
// neither CLI arguments nor MCP requests can alter them.
type CgroupLimits struct {
	CPUQuota string
	Memory   string
	PIDs     string
	Swap     string
}

var defaultVMLimits = CgroupLimits{
	CPUQuota: "200000 100000", // at most two cores
	Memory:   "5368709120",    // 5 GiB including QEMU overhead
	PIDs:     "256",
	Swap:     "0",
}

// CgroupManager owns an already-delegated, dedicated cgroup v2 subtree.
// It never enables controllers on an arbitrary ancestor or asks for sudo.
// This is NOT a namespace, disk quota, watchdog or security boundary on
// its own; the worker account must be isolated from pilot privileges.
type CgroupManager struct {
	root   string
	limits CgroupLimits
}

func OpenCgroupManager(root string) (*CgroupManager, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root ||
		!strings.HasPrefix(root, "/sys/fs/cgroup/") {
		return nil, errors.New("VM cgroup must be an absolute delegated cgroup v2 subtree")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("VM cgroup root is missing or unsafe")
	}
	if err := ownedByWorker(info); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return nil, errors.New("VM cgroup root traverses a symlink")
	}
	for _, pair := range [][2]string{
		{"cgroup.controllers", "cpu memory pids"},
		{"cgroup.subtree_control", "cpu memory pids"},
	} {
		content, err := os.ReadFile(filepath.Join(root, pair[0]))
		if err != nil {
			return nil, fmt.Errorf("delegated cgroup control %s not available: %w", pair[0], err)
		}
		controllers := strings.Fields(string(content))
		for _, want := range strings.Fields(pair[1]) {
			found := false
			for _, got := range controllers {
				if want == got {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("VM cgroup controller %s is not delegated in %s", want, pair[0])
			}
		}
	}
	return &CgroupManager{root: root, limits: defaultVMLimits}, nil
}

// RunCgroup retains a directory FD. Linux clone3(CLONE_INTO_CGROUP) places
// QEMU in the bounded subtree atomically with process creation; moving
// its PID using cgroup.procs after Start would introduce a race.
type RunCgroup struct {
	dir  string
	fd   *os.File
}

func openRunCgroup(dir string) (*RunCgroup, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("run cgroup directory is not trusted")
	}
	if err := ownedByWorker(info); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open run cgroup: %w", err)
	}
	return &RunCgroup{dir: dir, fd: os.NewFile(uintptr(fd), "run-cgroup")}, nil
}

func (c *RunCgroup) controlFile(name string, flags int) (*os.File, error) {
	if c == nil || c.fd == nil {
		return nil, errors.New("missing run cgroup handle")
	}
	switch name {
	case "cpu.max", "memory.max", "memory.swap.max", "memory.oom.group", "pids.max", "cgroup.events", "cgroup.kill":
	default:
		return nil, errors.New("unapproved cgroup control file")
	}
	fd, err := syscall.Openat(int(c.fd.Fd()), name, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("cgroup control %s unavailable: %w", name, err)
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (c *RunCgroup) writeControl(name, value string) error {
	file, err := c.controlFile(name, syscall.O_WRONLY)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(value)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("could not enforce cgroup %s", name)
	}
	return nil
}

func (c *RunCgroup) readControl(name string) (string, error) {
	file, err := c.controlFile(name, syscall.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return "", fmt.Errorf("cannot inspect cgroup %s", name)
	}
	return string(data), nil
}

func (m *CgroupManager) Prepare(id string) (*RunCgroup, error) {
	if m == nil || !validRunID(id) {
		return nil, errors.New("invalid run or missing delegated cgroup")
	}
	return m.prepare(id, os.Mkdir)
}

// prepare permits injecting a fake cgroup filesystem in hostless unit tests.
// Production always uses os.Mkdir against the already-validated delegation.
func (m *CgroupManager) prepare(id string, mkdir func(string, os.FileMode) error) (*RunCgroup, error) {
	dir := filepath.Join(m.root, id)
	if err := mkdir(dir, 0700); err != nil {
		return nil, fmt.Errorf("reserve VM cgroup: %w", err)
	}
	cg, err := openRunCgroup(dir)
	if err != nil {
		return nil, err
	}
	for _, control := range [][2]string{
		{"cpu.max", m.limits.CPUQuota},
		{"memory.max", m.limits.Memory},
		{"memory.swap.max", m.limits.Swap},
		{"memory.oom.group", "1"},
		{"pids.max", m.limits.PIDs},
	} {
		if err := cg.writeControl(control[0], control[1]); err != nil {
			_ = cg.Close()
			return nil, err
		}
	}
	return cg, nil
}

func (c *RunCgroup) Attach(cmd *exec.Cmd) error {
	if c == nil || c.fd == nil || cmd == nil || cmd.Process != nil {
		return errors.New("invalid pre-start cgroup attachment")
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cmd.SysProcAttr.UseCgroupFD {
		return errors.New("VM command already has a cgroup")
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = int(c.fd.Fd())
	return nil
}

// KillAndVerify terminates *all* processes in the run cgroup, regardless of
// whether the direct QEMU child has exited. This uses the run directory FD
// instead of a model-controlled path or a raw, potentially reused PID.
// A failing write or populated!=0 is a hard cleanup failure.
func (c *RunCgroup) KillAndVerify() error {
	if err := c.writeControl("cgroup.kill", "1"); err != nil {
		return err
	}
	deadline := time.Now().Add(cgroupCleanupTimeout)
	for {
		events, err := c.readControl("cgroup.events")
		if err != nil {
			return err
		}
		for _, line := range strings.Split(events, "\n") {
			if strings.TrimSpace(line) == "populated 0" {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("run cgroup remains populated after kill")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (c *RunCgroup) Close() error {
	if c == nil || c.fd == nil {
		return nil
	}
	err := c.fd.Close()
	c.fd = nil
	return err
}

// Remove is called only after KillAndVerify has confirmed the cgroup empty.
// Failure is reported; no recursive delete of foreign or agent-controlled
// filesystem paths is performed.
func (c *RunCgroup) Remove() error {
	if err := c.Close(); err != nil {
		return err
	}
	return os.Remove(c.dir)
}
