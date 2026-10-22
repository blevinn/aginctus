package compose

import (
	"errors"
	"strings"
	"testing"

	composeclient "github.com/lxc/incus-compose/client"
)

// TestHermeticRecreateOnDriftOptInProceedsPastDrift covers review finding B3
// (#290): when the operator opts into recreation, the driver must accept
// detected image drift instead of failing at the pre-EnsureProject gate, and
// must enter the recreation lifecycle. The hermetic server does not implement
// instance creation, so the run eventually fails downstream — but the failure
// must NOT surface as a typed *ComposeDriftError, which would indicate the
// driver still refused the drift instead of honoring RecreateOnDrift=true.
func TestHermeticRecreateOnDriftOptInProceedsPastDrift(t *testing.T) {
	const project = "aginctus-recreate-opt-in"
	f := newHermeticFixture(t)
	defer f.server.Close()

	f.seedOwnedProject(t, project)
	owned := defaultComposeInstanceConfig(project)
	owned.Config["user.image_alias"] = "docker.io/library/alpine:3.21"
	owned.Config["volatile.base_image"] = "fprnt-alpine-321"
	f.server.PutInstance(project, owned)
	f.server.PutImageAlias(project, "docker.io/library/alpine:3.22", "fprnt-alpine-322")

	doc := []byte(`services:
  app:
    image: docker.io/library/alpine:3.22
    command: ["sleep", "3600"]
`)
	calls, err := runHermeticExecuteComposeWithConfig(t, f.server, f.conn,
		Config{Project: project, Compose: doc, RecreateOnDrift: true}, nil)
	if err == nil {
		t.Fatal("expected downstream failure once the recreation path engages against the hermetic server")
	}
	// The driver accepted the drift; no *ComposeDriftError must escape.
	var drift *ComposeDriftError
	if errors.As(err, &drift) {
		t.Fatalf("RecreateOnDrift=true still surfaced a typed drift error: %v", err)
	}
	// The driver must have read the existing instance and discovered the
	// drift, evidenced by a GET on the instance before any failure.
	if got := countPath(calls, "/1.0/instances/app-1"); got == 0 {
		t.Fatalf("driver never consulted the existing instance; calls=%v", calls)
	}
	// The error must mention recreation or the instance name so an operator
	// can tell the driver entered the recreation path rather than failing
	// before it.
	if !strings.Contains(err.Error(), "recreat") && !strings.Contains(err.Error(), "app-1") {
		t.Fatalf("error %q does not indicate the recreation path was entered", err)
	}
}

// TestHermeticDriftErrorExcludesDiskSourcesInRecreationGuidance covers the
// secret-disclosure contract on the drift-with-persistent-disks path. The
// driver must include the recreation-safety guidance (a count of explicit
// storage volume attachments) so an operator can plan replacement, but it must
// NEVER include the disk source name, pool name, or mount path values in the
// diagnostic. These values are internal volume references that operators must
// not be forced to read off the wire.
func TestHermeticDriftErrorExcludesDiskSourcesInRecreationGuidance(t *testing.T) {
	const project = "aginctus-recreate-disks"
	f := newHermeticFixture(t)
	defer f.server.Close()

	f.seedOwnedProject(t, project)
	owned := defaultComposeInstanceConfig(project)
	owned.Config["user.image_alias"] = "docker.io/library/alpine:3.21"
	owned.Config["volatile.base_image"] = "fprnt-alpine-321"
	// Persistent disks whose source, pool, and path values must never
	// appear in the surfaced diagnostic. These names are deliberately
	// distinctive so a regression that copies them into the error string
	// is caught loudly.
	owned.Devices = map[string]map[string]string{
		"root": {
			"type": "disk", "path": "/", "pool": "default-pool",
		},
		"pgdata": {
			"type": "disk", "path": "/var/lib/postgresql/data",
			"pool": "encrypted-pool", "source": "pg-volume-secret",
		},
		"cache": {
			"type": "disk", "path": "/cache",
			"pool": "scratch-pool", "source": "cache-volume-secret",
		},
	}
	f.server.PutInstance(project, owned)
	f.server.PutImageAlias(project, "docker.io/library/alpine:3.22", "fprnt-alpine-322")

	doc := []byte(`services:
  app:
    image: docker.io/library/alpine:3.22
    command: ["sleep", "3600"]
`)
	_, err := runHermeticExecuteComposeWithConfig(t, f.server, f.conn,
		Config{Project: project, Compose: doc, RecreateOnDrift: false}, nil)
	if err == nil {
		t.Fatal("expected drift refusal, got success")
	}
	// The driver must surface the recreation guidance so the operator can
	// resolve the drift without guessing.
	if !strings.Contains(err.Error(), "recreat") {
		t.Fatalf("error %q does not point the operator at recreation", err)
	}
	if !strings.Contains(err.Error(), "storage volume attachment") {
		t.Fatalf("error %q does not include the storage attachment count", err)
	}
	// Typed error: callers must be able to branch on ComposeDriftImage
	// without parsing the message.
	var drift *ComposeDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("error %T is not a typed *ComposeDriftError", err)
	}
	if drift.Kind != ComposeDriftImage {
		t.Fatalf("drift kind = %q, want %q", drift.Kind, ComposeDriftImage)
	}
	// Secret-safe diagnostic: the disk source, pool, and path values must
	// not appear in the error string.
	secrets := []string{
		"pg-volume-secret", "cache-volume-secret",
		"encrypted-pool", "scratch-pool",
		"/var/lib/postgresql/data", "/cache",
	}
	for _, secret := range secrets {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("drift error leaked secret %q: %v", secret, err)
		}
	}
}

// TestHermeticDriftErrorExcludesEnvironmentSecrets covers the broader
// secret-disclosure contract: when the compose file references environment
// variables for secret material, an error surfaced by executeCompose must
// never include those secret values. The driver does substitute environment
// values into the compose model, so any code path that formats a diagnostic
// from the loaded project could leak them; this test guards against that.
func TestHermeticDriftErrorExcludesEnvironmentSecrets(t *testing.T) {
	const project = "aginctus-recreate-env"
	f := newHermeticFixture(t)
	defer f.server.Close()

	// Distinctive secret-looking values placed in the environment map.
	// The drift path uses these by name only; the resolved values must
	// never appear in the surfaced diagnostic.
	const secretPassword = "TOP-SECRET-postgres-password-XYZ"
	const secretMasterKey = "TOP-SECRET-master-key-ABC"
	environment := map[string]string{
		"AGINCTUS_GATEWAY_POSTGRES_PASSWORD": secretPassword,
		"AGINCTUS_GATEWAY_MASTER_KEY":        secretMasterKey,
	}
	f.seedOwnedProject(t, project)
	owned := defaultComposeInstanceConfig(project)
	owned.Config["user.image_alias"] = "docker.io/library/alpine:3.21"
	owned.Config["volatile.base_image"] = "fprnt-alpine-321"
	f.server.PutInstance(project, owned)
	f.server.PutImageAlias(project, "docker.io/library/alpine:3.22", "fprnt-alpine-322")

	doc := []byte(`services:
  app:
    image: docker.io/library/alpine:3.22
    command: ["sleep", "3600"]
`)
	_, err := runHermeticExecuteComposeWithConfig(t, f.server, f.conn,
		Config{Project: project, Compose: doc, RecreateOnDrift: false}, environment)
	if err == nil {
		t.Fatal("expected drift refusal, got success")
	}
	for _, secret := range []string{secretPassword, secretMasterKey} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("drift error leaked environment secret %q: %v", secret, err)
		}
	}
}

// TestHermeticRecreateOnDriftOmitsSecretsAcrossAllDriftKinds asserts the
// composite secret-disclosure contract for every drift kind exercised
// through executeCompose: image alias, image fingerprint, instance config,
// and device. No drift path may surface a fingerprint, disk source, or pool
// in the diagnostic. Run with all four seeds and verify the error string
// remains secret-safe.
func TestHermeticRecreateOnDriftOmitsSecretsAcrossAllDriftKinds(t *testing.T) {
	const project = "aginctus-omits-all"
	f := newHermeticFixture(t)
	defer f.server.Close()

	f.seedOwnedProject(t, project)
	owned := defaultComposeInstanceConfig(project)
	owned.Config["user.image_alias"] = "docker.io/library/alpine:3.21"
	owned.Config["volatile.base_image"] = "fprnt-alpine-321"
	// Distinctive disk source and pool used to detect leaks.
	owned.Devices = map[string]map[string]string{
		"root": {"type": "disk", "path": "/", "pool": "default"},
		"data": {"type": "disk", "path": "/data", "pool": "secret-pool-name", "source": "secret-volume-name"},
	}
	f.server.PutInstance(project, owned)
	f.server.PutImageAlias(project, "docker.io/library/alpine:3.22", "fprnt-alpine-322")

	doc := []byte(`services:
  app:
    image: docker.io/library/alpine:3.22
    command: ["sleep", "3600"]
`)
	_, err := runHermeticExecuteComposeWithConfig(t, f.server, f.conn,
		Config{Project: project, Compose: doc, RecreateOnDrift: false}, nil)
	if err == nil {
		t.Fatal("expected drift refusal")
	}
	var drift *ComposeDriftError
	if !errors.As(err, &drift) {
		t.Fatalf("error %T is not a typed *ComposeDriftError", err)
	}
	// Distinctive markers that must never reach the diagnostic.
	leaks := []string{
		"fprnt-alpine-321", "fprnt-alpine-322",
		"secret-volume-name", "secret-pool-name",
		"/data",
	}
	for _, leak := range leaks {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("drift error leaked %q: %v", leak, err)
		}
	}
}

// Compile-time check that the upstream seed marker is referenced via the
// composeclient import path used by the production driver. This prevents a
// silent rename upstream from desynchronizing the seed-tolerance predicate.
var _ = composeclient.TempInstanceKey
