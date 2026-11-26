package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

const bootstrapConfigPath = "/etc/aginctus/project-state-bootstrap.json"
const bootstrapConfigVersion = "aginctus.dev/project-state-bootstrap/v1alpha1"

type projectStateBootstrapConfig struct {
	APIVersion         string  `json:"apiVersion"`
	Binding            Binding `json:"binding"`
	ServiceUID         uint32  `json:"serviceUid"`
	MountPath          string  `json:"mountPath"`
	OperatorConfigFile string  `json:"operatorConfigFile"`
}

// BootstrapMountedProjectState is a one-shot guest role. Its fixed,
// administrator-owned config selects the service identity and existing mount.
// Host-side exclusive attachment verification is an independent prerequisite.
func BootstrapMountedProjectState(ctx context.Context) (string, error) {
	deny := errors.New("project-state bootstrap authorization unavailable")
	if ctx == nil || ctx.Err() != nil || os.Geteuid() == 0 || os.Getuid() != os.Geteuid() {
		return "", deny
	}
	for _, path := range []string{"/", "/etc", "/etc/aginctus"} {
		var st unix.Stat_t
		if unix.Lstat(path, &st) != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0o022 != 0 {
			return "", deny
		}
	}
	config, err := readProjectStateBootstrapConfig(bootstrapConfigPath, 0)
	if err != nil || config.ServiceUID != uint32(os.Geteuid()) {
		return "", deny
	}
	if checkProjectStateMountpoint(config.MountPath) != nil || checkPrivateLocalDir(config.MountPath) != nil {
		return "", errors.New("project-state bootstrap mount unavailable")
	}
	operator, err := ReadRestrictedOperatorReadConfig(config.OperatorConfigFile)
	if err != nil || operator.Project != config.Binding.Project || "sha256:"+operator.TrustedServerSHA256 != config.Binding.ServerID {
		return "", deny
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	scoped, err := OpenExplicitRestrictedIncusProject(ctx, operator.Connection())
	if err != nil {
		return "", deny
	}
	defer scoped.Disconnect()
	verifier := RestrictedIncusProjectVerifier{Observer: scoped, PinnedServerCertificateSHA256: operator.TrustedServerSHA256}
	return bootstrapProjectStore(ctx, config.MountPath, config.Binding, verifier, checkProjectStateMountpoint)
}

func readProjectStateBootstrapConfig(path string, owner uint32) (projectStateBootstrapConfig, error) {
	var c projectStateBootstrapConfig
	deny := errors.New("protected project-state bootstrap configuration unavailable")
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return c, deny
	}
	f := os.NewFile(uintptr(fd), "project-state bootstrap configuration")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != owner || st.Nlink != 1 || st.Mode != unix.S_IFREG|0o644 {
		return c, deny
	}
	const limit = 4096
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	var fields, binding map[string]json.RawMessage
	if err != nil || len(data) > limit || rejectCandidateDuplicateKeys(data) != nil || json.Unmarshal(data, &fields) != nil ||
		candidateKeys(fields, "apiVersion", "binding", "serviceUid", "mountPath", "operatorConfigFile") != nil ||
		json.Unmarshal(fields["binding"], &binding) != nil || candidateKeys(binding, "serverId", "project") != nil ||
		json.Unmarshal(data, &c) != nil || c.APIVersion != bootstrapConfigVersion || c.ServiceUID == 0 ||
		validateLocalProjectRoot(c.MountPath, c.Binding) != nil ||
		!filepath.IsAbs(c.OperatorConfigFile) || filepath.Clean(c.OperatorConfigFile) != c.OperatorConfigFile {
		return projectStateBootstrapConfig{}, deny
	}
	return c, nil
}

func bootstrapProjectStore(ctx context.Context, mount string, binding Binding, verifier BindingVerifier, checkMount func(string) error) (string, error) {
	deny := errors.New("project-state bootstrap mount or binding unverified")
	if ctx == nil || ctx.Err() != nil || verifier == nil || checkMount == nil || validateLocalProjectRoot(mount, binding) != nil ||
		checkMount(mount) != nil || checkPrivateLocalDir(mount) != nil {
		return "", deny
	}
	dir, err := os.OpenFile(mount, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return "", deny
	}
	defer dir.Close()
	if unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return "", errors.New("project-state bootstrap mount busy")
	}
	verify := func() error {
		actual, err := verifier.VerifyProject(ctx, binding.Project)
		held, statErr := dir.Stat()
		named, nameErr := os.Lstat(mount)
		if err != nil || actual != binding || ctx.Err() != nil || statErr != nil || nameErr != nil || !os.SameFile(held, named) ||
			checkMount(mount) != nil || checkPrivateLocalDir(mount) != nil {
			return deny
		}
		return nil
	}
	if err := verify(); err != nil {
		return "", err
	}
	if _, err := OpenLocalProjectStateStore(mount, binding); err == nil {
		if err := verify(); err != nil || dir.Sync() != nil {
			return "", deny
		}
		return "reopened", nil
	}
	if names, err := dir.Readdirnames(1); len(names) != 0 || !errors.Is(err, io.EOF) {
		return "", errors.New("project-state mount is nonempty or unreadable; refusing adoption")
	}
	data, err := json.Marshal(localProjectRoot{APIVersion: localProjectStoreVersion, Binding: binding})
	if err != nil || verify() != nil {
		return "", deny
	}
	// Publish the existing authority record last. A partial initialization
	// cannot be opened as a store or automatically adopted on retry.
	partial := errors.New("project-state bootstrap incomplete or uncertain; inspect mount before retry")
	if unix.Mkdirat(int(dir.Fd()), "environments", 0o700) != nil {
		return "", partial
	}
	if dir.Sync() != nil {
		return "", partial
	}
	if err := publishBootstrapAuthority(dir, data, verify); err != nil {
		return "", partial
	}
	if _, err := OpenLocalProjectStateStore(mount, binding); err != nil || verify() != nil {
		return "", partial
	}
	return "initialized", nil
}

// Stage on the held mount inode, sync bytes before commit, and never replace
// an existing authority file (even one with byte-for-byte identical contents).
func publishBootstrapAuthority(dir *os.File, data []byte, verify func() error) error {
	fd := int(dir.Fd())
	anchored := "/proc/self/fd/" + strconv.Itoa(fd)
	tmp, err := os.CreateTemp(anchored, ".bootstrap-*")
	if err != nil {
		return err
	}
	name := filepath.Base(tmp.Name())
	defer func() { _ = unix.Unlinkat(fd, name, 0) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := verify(); err != nil {
		return err
	}
	if err := unix.Renameat2(fd, name, fd, "authority.json", unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("publish bootstrap authority: %w", err)
	}
	return dir.Sync()
}
