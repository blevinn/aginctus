package environment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

type bootstrapVerifier func(context.Context, string) (Binding, error)

func (v bootstrapVerifier) VerifyProject(ctx context.Context, project string) (Binding, error) {
	return v(ctx, project)
}

func TestBootstrapProjectStoreInitializesAndReopensRetainedState(t *testing.T) {
	ctx := t.Context()
	c := candidateFixture()
	root := filepath.Join(t.TempDir(), "mount")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	verifier := &testProjectVerifier{binding: c.Environment.Binding}
	check := func(string) error { return nil } // No mount operation in the test.
	if got, err := bootstrapProjectStore(ctx, root, c.Environment.Binding, verifier, check); err != nil || got != "initialized" {
		t.Fatalf("initialize: %q %v", got, err)
	}
	s, err := OpenLocalProjectStateStore(root, c.Environment.Binding)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := s.CommitLocalCandidate(ctx, c.Environment, c.Environment.Binding, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	anchor := filepath.Join(root, "authority.json")
	before, err := os.Stat(anchor)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := bootstrapProjectStore(ctx, root, c.Environment.Binding, verifier, check); err != nil || got != "reopened" {
		t.Fatalf("reopen: %q %v", got, err)
	}
	after, err := os.Stat(anchor)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("reopen replaced authority")
	}
	if got, _, err := s.ReadLocalAccepted(ctx, c.Environment); err != nil || got == nil || *got != rev {
		t.Fatal("reopen did not retain accepted revision")
	}
	foreign := c.Environment.Binding
	foreign.Project = "foreign"
	if got, err := bootstrapProjectStore(ctx, root, foreign, &testProjectVerifier{binding: foreign}, check); err == nil || got != "" {
		t.Fatal("foreign retained mount was adopted")
	}
}

func TestBootstrapProjectStoreRefusesUnsafeAndPartialMounts(t *testing.T) {
	for _, scenario := range []string{"ordinary directory", "public directory", "foreign binding", "nonempty", "busy", "lost authority before publication"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "mount")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			binding := candidateFixture().Environment.Binding
			calls := 0
			verify := bootstrapVerifier(func(_ context.Context, _ string) (Binding, error) {
				calls++
				if scenario == "foreign binding" || scenario == "lost authority before publication" && calls == 3 {
					return Binding{}, errors.New("synthetic private diagnostic")
				}
				return binding, nil
			})
			check := func(string) error { return nil }
			switch scenario {
			case "ordinary directory":
				check = checkProjectStateMountpoint
			case "public directory":
				if err := os.Chmod(root, 0o755); err != nil {
					t.Fatal(err)
				}
			case "nonempty":
				if err := os.WriteFile(filepath.Join(root, "foreign"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "busy":
				dir, err := os.Open(root)
				if err != nil {
					t.Fatal(err)
				}
				defer dir.Close()
				if err := unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := bootstrapProjectStore(t.Context(), root, binding, verify, check); err == nil || got != "" {
				t.Fatal("unsafe bootstrap succeeded")
			}
			if _, err := os.Lstat(filepath.Join(root, "authority.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed bootstrap published authority")
			}
			if scenario == "lost authority before publication" {
				if got, err := bootstrapProjectStore(t.Context(), root, binding, verify, check); err == nil || got != "" {
					t.Fatal("retry adopted the partially initialized mount")
				}
			}
		})
	}
}

func TestBootstrapConfigRequiresProtectedExactSelection(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bootstrap.json")
	c := projectStateBootstrapConfig{bootstrapConfigVersion, candidateFixture().Environment.Binding, 1234, "/srv/state", "/run/state/read.json"}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Geteuid())
	if got, err := readProjectStateBootstrapConfig(p, uid); err != nil || got != c {
		t.Fatalf("authorized config: %v", err)
	}
	if _, err := readProjectStateBootstrapConfig(p, uid+1); err == nil {
		t.Fatal("foreign config owner accepted")
	}
	if err := os.Chmod(p, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectStateBootstrapConfig(p, uid); err == nil {
		t.Fatal("writable config accepted")
	}
}
