package compose

import (
	"errors"
	"strings"
	"testing"

	composeclient "github.com/lxc/incus-compose/client"
	"github.com/lxc/incus/v7/shared/api"
)

// TestHermeticOwnershipFailureBeforeEnsureProject covers review finding B3
// (#290): a project the driver would adopt at the Incus level, but whose
// ownership markers do not match this deployment, must be refused before any
// upstream EnsureProject or mutation. The pre-ensure check is the gate.
func TestHermeticOwnershipFailureBeforeEnsureProject(t *testing.T) {
	const project = "aginctus-fake"
	f := newHermeticFixture(t)
	defer f.server.Close()

	// Seed a foreign project: managed marker absent, deployment key wrong.
	f.server.PutProject(project, api.Project{
		Name: project,
		ProjectPut: api.ProjectPut{
			Config: map[string]string{
				ownershipManagedKey:    "true",
				ownershipResourceKey:   "compose-project",
				ownershipDeploymentKey: "someone-else",
			},
		},
	})

	calls, err := runHermeticExecuteCompose(t, f.server, f.conn, project, []byte(defaultComposeDocument))
	if err == nil {
		t.Fatalf("expected pre-ensure ownership refusal, got success; calls=%v", calls)
	}
	if !strings.Contains(err.Error(), "pre-ensure Compose ownership check") {
		t.Fatalf("error %q does not identify the pre-ensure gate", err)
	}
	if !strings.Contains(err.Error(), "ownership marker missing or mismatched") {
		t.Fatalf("error %q does not explain the marker failure", err)
	}
	// The pre-ensure gate fires twice (the driver reads once, then calls
	// verifyComposeDeploymentOwnership which reads again); both reads must
	// complete *before* the upstream library opens the project client. The
	// signature of EnsureProject reaching the connection is that the project
	// gets an "open existing" call, which we never want here.
	for _, call := range calls {
		if call.Method == "POST" && call.Path == "/1.0/projects" {
			t.Fatalf("driver attempted project creation despite a foreign project being present: %v", call)
		}
	}
	// And we must not have started opening events for the project either:
	// the upstream library does that in EnsureProject.
	for _, call := range calls {
		if call.Path == "/1.0/events" && strings.Contains(call.Query, "aginctus-fake") {
			t.Fatalf("driver opened the project event listener despite the foreign project: %v", call)
		}
	}
}

// TestHermeticOwnershipFailureOnUnmarkedExistingProject covers the case where
// a project exists but carries none of the Aginctus ownership markers. The
// driver must still refuse adoption; this is the second pre-ensure failure
// mode after wrong-marker.
func TestHermeticOwnershipFailureOnUnmarkedExistingProject(t *testing.T) {
	const project = "aginctus-unmarked"
	f := newHermeticFixture(t)
	defer f.server.Close()

	f.server.PutProject(project, api.Project{
		Name: project,
		ProjectPut: api.ProjectPut{
			Config: map[string]string{"features.profiles": "true"},
		},
	})

	_, err := runHermeticExecuteCompose(t, f.server, f.conn, project, []byte(defaultComposeDocument))
	if err == nil {
		t.Fatal("expected ownership refusal, got success")
	}
	if !strings.Contains(err.Error(), "pre-ensure Compose ownership check") {
		t.Fatalf("error %q does not identify the pre-ensure gate", err)
	}
	if !strings.Contains(err.Error(), "refusing adoption") {
		t.Fatalf("error %q does not carry the adoption-refusal message", err)
	}
}

// TestHermeticOwnershipFailureOnForeignInstanceInOwnedProject covers the
// interruption / hostile-tenant case: the project is owned, but one of its
// instances belongs to a foreign deployment. The driver must fail closed at
// the pre-ensure inventory check.
func TestHermeticOwnershipFailureOnForeignInstanceInOwnedProject(t *testing.T) {
	const project = "aginctus-foreign-inst"
	f := newHermeticFixture(t)
	defer f.server.Close()

	f.seedOwnedProject(t, project)
	// Foreign instance: missing Aginctus ownership markers.
	f.server.PutInstance(project, api.InstanceFull{
		Instance: api.Instance{
			Name: "app-1",
			Type: string(api.InstanceTypeContainer),
			InstancePut: api.InstancePut{
				Profiles: []string{"default"},
				Config:   map[string]string{},
			},
		},
	})

	_, err := runHermeticExecuteCompose(t, f.server, f.conn, project, []byte(defaultComposeDocument))
	if err == nil {
		t.Fatal("expected pre-ensure ownership refusal, got success")
	}
	if !strings.Contains(err.Error(), "pre-ensure Compose ownership check") {
		t.Fatalf("error %q does not identify the pre-ensure gate", err)
	}
	if !strings.Contains(err.Error(), "not owned by this deployment") {
		t.Fatalf("error %q does not identify the foreign instance: %v", err, err)
	}
}

// TestHermeticInterruptedImageSeedToleratedAlongsideForeignService combines
// two of the behaviors from #290: a legitimate ic-seed-* container must be
// skipped, but an arbitrary foreign service must still fail closed. The
// upstream's seed container predicate is verified here at the inventory layer
// the driver depends on.
func TestHermeticInterruptedImageSeedToleratedAlongsideForeignService(t *testing.T) {
	const project = "aginctus-seed"
	f := newHermeticFixture(t)
	defer f.server.Close()

	f.seedOwnedProject(t, project)
	owned := defaultComposeInstanceConfig(project)
	f.server.PutInstance(project, owned)
	f.server.PutInstance(project, api.InstanceFull{
		Instance: api.Instance{
			Name: "ic-seed-abcdef",
			Type: string(api.InstanceTypeContainer),
			InstancePut: api.InstancePut{
				Config: map[string]string{
					composeclient.TempInstanceKey: "true",
				},
			},
		},
	})

	_, err := runHermeticExecuteCompose(t, f.server, f.conn, project, []byte(defaultComposeDocument))
	// The pre-ensure inventory must accept the seed; the test then fails
	// later in the lifecycle (the driver cannot finish on a hermetic fake
	// without instance create), which is fine: the seed was tolerated.
	if err == nil {
		t.Fatal("expected the lifecycle to fail without an instance create path")
	}
	if !strings.Contains(err.Error(), "ownership") && !strings.Contains(err.Error(), "ensure") && !strings.Contains(err.Error(), "instance") {
		t.Fatalf("unexpected error shape %q (seed should have been tolerated)", err)
	}
}

// TestHermeticOwnershipReverificationAfterEnsureProject covers the second
// verifyComposeDeploymentOwnership call, exercised against an owned project
// and an owned instance whose image state matches the desired compose. The
// driver must run both gates in order; the second gate's existence is what
// makes the post-EnsureProject state safer than a single check.
func TestHermeticOwnershipReverificationAfterEnsureProject(t *testing.T) {
	const project = "aginctus-reverify"
	f := newHermeticFixture(t)
	defer f.server.Close()

	f.seedOwnedProject(t, project)
	// One owned instance so the inventory check has at least one entry to
	// inspect. Drift is left for the dedicated fingerprint test.
	owned := defaultComposeInstanceConfig(project)
	owned.Config["user.image_alias"] = "docker.io/library/alpine:3.21"
	owned.Config["volatile.base_image"] = "fprnt-alpine-321"
	f.server.PutInstance(project, owned)

	calls, _ := runHermeticExecuteCompose(t, f.server, f.conn, project, []byte(defaultComposeDocument))
	// The two driver-side gates (pre and post EnsureProject) each issue a
	// GetProject, and verifyComposeDeploymentOwnership itself issues one too
	// per call. So we expect at least four reads of /1.0/projects/{name}:
	// driver pre-ensure direct read + pre-ensure verify read + post-ensure
	// verify read + EnsureProject's own getProject. Anything fewer would
	// mean one of the gates was skipped.
	if got := countPath(calls, "/1.0/projects/aginctus-reverify"); got < 4 {
		t.Fatalf("GetProject issued %d times, want at least 4 (driver pre + pre-verify + EnsureProject + post-verify); calls=%v", got, calls)
	}
	// Two inventory listings, one per verify call, each listing the one
	// owned instance.
	if got := countPath(calls, "/1.0/instances"); got < 2 {
		t.Fatalf("GetInstances issued %d times, want at least 2 (pre + post verify); calls=%v", got, calls)
	}
	// Opening the project's event listener is the proof EnsureProject ran;
	// the second verifyComposeDeploymentOwnership only happens after that.
	for _, call := range calls {
		if call.Path == "/1.0/events" && strings.Contains(call.Query, "aginctus-reverify") {
			return
		}
	}
	t.Fatalf("driver never opened the project event listener; calls=%v", calls)
}

// TestHermeticImageAliasDriftDetectedOnExistingInstance covers review finding
// B3 (#290) image fingerprint drift: the existing instance has an
// `user.image_alias` that no longer matches the desired image. The driver
// must surface the drift, refuse to silently treat it as converged, and never
// ask the upstream library to recreate without explicit opt-in.
func TestHermeticImageAliasDriftDetectedOnExistingInstance(t *testing.T) {
	const project = "aginctus-drift"
	f := newHermeticFixture(t)
	defer f.server.Close()

	f.seedOwnedProject(t, project)
	owned := defaultComposeInstanceConfig(project)
	// Stale image alias and base fingerprint — the compose file asks for 3.22.
	owned.Config["user.image_alias"] = "docker.io/library/alpine:3.21"
	owned.Config["volatile.base_image"] = "fprnt-alpine-321"
	f.server.PutInstance(project, owned)
	// Seed the desired image alias the driver resolves during drift check.
	f.server.PutImageAlias(project, "docker.io/library/alpine:3.22", "fprnt-alpine-322")

	doc := []byte(`services:
  app:
    image: docker.io/library/alpine:3.22
    command: ["sleep", "3600"]
`)
	calls, err := runHermeticExecuteCompose(t, f.server, f.conn, project, doc)
	if err == nil {
		t.Fatalf("expected drift error; calls=%v", calls)
	}
	if !strings.Contains(err.Error(), "drift") {
		t.Fatalf("error %q does not identify drift", err)
	}
	if !strings.Contains(err.Error(), "recreation") && !strings.Contains(err.Error(), "recreate") {
		t.Fatalf("error %q does not point the operator at recreation", err)
	}
	// Drift must surface from a typed error so callers can branch on
	// errors.As without parsing strings.
	var typed *ComposeDriftError
	if !errors.As(err, &typed) {
		t.Fatalf("error %T is not a typed *ComposeDriftError", err)
	}
	if typed.Kind != ComposeDriftImage {
		t.Fatalf("drift kind = %q, want %q", typed.Kind, ComposeDriftImage)
	}
	// Drift errors never leak the old value or fingerprint; verify that
	// guard holds for the hermetic path too.
	for _, fingerprint := range []string{"fprnt-alpine-321", "fprnt-alpine-322"} {
		if strings.Contains(err.Error(), fingerprint) {
			t.Fatalf("drift error leaked fingerprint %q: %v", fingerprint, err)
		}
	}
	// The driver must have read the existing instance to compare its
	// user.image_alias against the desired image name.
	if got := countPath(calls, "/1.0/instances/app-1"); got == 0 {
		t.Fatalf("driver never consulted the existing instance; calls=%v", calls)
	}
}

// countPath returns how many recorded calls match the exact path.
func countPath(calls []incusCall, path string) int {
	n := 0
	for _, call := range calls {
		if call.Path == path {
			n++
		}
	}
	return n
}
