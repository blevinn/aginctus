package environment

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// MountedProjectPublishedSource provides a read-only view of an EXISTING
// project-owned state volume attached to an unprivileged service instance.
// It uses the existing hash-validated, durable project-state file format,
// but exposes no local acceptance/CAS mutation capability.
//
// The mountpoint check is evidence of a real mount inside the service,
// NOT proof that Incus attached the correct owner project/volume. The
// independently authorized host preflight and disposable VM must establish
// exact project ownership, exclusive attachment and mount confinement.
// Never use this with an operator's home directory or the fixture socket.
type MountedProjectPublishedSource struct {
	store *LocalProjectStateStore
	root string
	binding Binding
	verifier BindingVerifier
	checkMount func(string) error
}

func checkProjectStateMountpoint(root string) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root ||
		root == "/" || strings.ContainsAny(root, " \t\r\n\\") {
		return errors.New("explicit normalized mountpoint required")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("project state volume mountpoint unavailable")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return errors.New("project state mountpoint redirects through a symlink")
	}
	mounts, err := os.Open("/proc/self/mountinfo")
	if err != nil { return errors.New("kernel mount information unavailable") }
	defer mounts.Close()
	scanner := bufio.NewScanner(mounts)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 6 && fields[4] == root { return nil }
	}
	if scanner.Err() != nil { return errors.New("kernel mount information unavailable") }
	return errors.New("project state root is not a dedicated mountpoint")
}

// OpenMountedProjectPublishedSource performs NO bootstrap, adoption, volume
// attachment, socket access, or filesystem writes. The caller must launch
// this inside the dedicated, nonroot service instance after authorized
// host-side attachment preflight; the store root is an existing mountpoint.
func OpenMountedProjectPublishedSource(
	ctx context.Context, root, project string, verifier BindingVerifier,
) (*MountedProjectPublishedSource, error) {
	fail := func() (*MountedProjectPublishedSource, error) {
		return nil, errors.New("authenticated mounted project-state volume unavailable")
	}
	if ctx == nil || ctx.Err() != nil || verifier == nil || os.Geteuid() == 0 {
		return fail()
	}
	binding, err := verifier.VerifyProject(ctx, project)
	if err != nil || binding.Project != project || binding.ServerID == "" { return fail() }
	if checkProjectStateMountpoint(root) != nil { return fail() }
	store, err := OpenLocalProjectStateStore(root, binding)
	if err != nil { return fail() }
	again, err := verifier.VerifyProject(ctx, project)
	if err != nil || again != binding || ctx.Err() != nil { return fail() }
	return &MountedProjectPublishedSource{
		store: store, root: root, binding: binding,
		verifier: verifier, checkMount: checkProjectStateMountpoint,
	}, nil
}

func (s *MountedProjectPublishedSource) ReadAccepted(
	ctx context.Context, enrolled Descriptor,
) (*AcceptedRevision, *Candidate, error) {
	deny := func() (*AcceptedRevision, *Candidate, error) {
		return nil, nil, errors.New("authenticated mounted accepted revision unavailable")
	}
	if ctx == nil || ctx.Err() != nil || s == nil || s.store == nil ||
		s.verifier == nil || s.checkMount == nil ||
		enrolled.Validate() != nil || enrolled.Binding != s.binding {
		return deny()
	}
	if s.checkMount(s.root) != nil { return deny() }
	binding, err := s.verifier.VerifyProject(ctx, s.binding.Project)
	if err != nil || binding != s.binding || ctx.Err() != nil { return deny() }
	rev, candidate, err := s.store.ReadLocalAccepted(ctx, enrolled)
	if err != nil || (rev == nil) != (candidate == nil) { return deny() }
	if rev != nil && (rev.ValidateAgainst(*candidate) != nil ||
		!candidate.Environment.SameEnvironment(enrolled)) { return deny() }
	if s.checkMount(s.root) != nil { return deny() }
	after, err := s.verifier.VerifyProject(ctx, s.binding.Project)
	if err != nil || after != s.binding || ctx.Err() != nil { return deny() }
	return rev, candidate, nil
}
