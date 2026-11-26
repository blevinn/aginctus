package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

func gatewayProvenanceFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLocalGatewayImageProvenanceRejectsUntrustedRecords(t *testing.T) {
	ctx := t.Context()
	top, _, _, base := environmentGatewayCreateFixture(t)
	rev := fixtureGatewayRevision(t, startFixtureDescriptor(top))
	name, err := gatewayProvenanceName(rev, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	evidence := base.imagesForCreate.Postgres.Evidence
	evidence.AliasMarker = newGatewayAliasMarker()
	for _, scenario := range []string{"mode", "symlink", "hardlink", "oversize", "duplicate-key", "wrong-role", "pending-only"} {
		t.Run(scenario, func(t *testing.T) {
			root := gatewayProvenanceFixtureDir(t)
			store, err := OpenLocalGatewayImageProvenance(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.record(ctx, rev, "postgres", evidence); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, name)
			alter := func() error {
				switch scenario {
				case "mode":
					return os.Chmod(path, 0o644)
				case "hardlink":
					return os.Link(path, path+"-link")
				case "symlink":
					if err := os.Rename(path, path+"-target"); err != nil {
						return err
					}
					return os.Symlink(path+"-target", path)
				case "oversize":
					return os.WriteFile(path, []byte(strings.Repeat(" ", gatewayProvenanceLimit+1)), 0o600)
				case "pending-only":
					return os.Rename(path, filepath.Join(root, ".pending-test"))
				default:
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					if scenario == "duplicate-key" {
						data = append([]byte(`{"version":1,`), data[1:]...)
					} else {
						data = []byte(strings.Replace(string(data), `"role":"postgres"`, `"role":"litellm"`, 1))
					}
					return os.WriteFile(path, data, 0o600)
				}
			}
			if err := alter(); err != nil {
				t.Fatal(err)
			}
			wantErr := errGatewayProvenanceUnverified
			if scenario == "pending-only" {
				wantErr = errGatewayProvenanceAbsent
			}
			got, err := store.ExistingImageEvidence(ctx, rev, "postgres")
			if !errors.Is(err, wantErr) || got != (native.GatewayImageEvidence{}) {
				t.Fatalf("untrusted record returned evidence: %v", err)
			}
		})
	}
}

func TestLocalGatewayImageProvenanceNeverOverwritesOrCrossesScope(t *testing.T) {
	ctx := t.Context()
	top, _, _, base := environmentGatewayCreateFixture(t)
	desc := startFixtureDescriptor(top)
	rev := fixtureGatewayRevision(t, desc)
	store, err := OpenLocalGatewayImageProvenance(gatewayProvenanceFixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	evidence := base.imagesForCreate.Postgres.Evidence
	evidence.AliasMarker = newGatewayAliasMarker()
	if err := store.record(ctx, rev, "postgres", evidence); err != nil {
		t.Fatal(err)
	}
	conflict := evidence
	conflict.Identity.ConfigDigest = "sha256:" + strings.Repeat("d", 64)
	if err := store.record(ctx, rev, "postgres", conflict); !errors.Is(err, errGatewayProvenanceUnverified) {
		t.Fatalf("overwriting record did not fail: %v", err)
	}
	if got, err := store.ExistingImageEvidence(ctx, rev, "postgres"); got != evidence || err != nil {
		t.Fatalf("original receipt not preserved: %v", err)
	}
	desc.Binding.ServerID += "-other"
	other := fixtureGatewayRevision(t, desc)
	for _, key := range []struct {
		revision environment.AcceptedRevision
		role     string
	}{{rev, "litellm"}, {other, "postgres"}} {
		if _, err := store.ExistingImageEvidence(ctx, key.revision, key.role); !errors.Is(err, errGatewayProvenanceAbsent) {
			t.Fatalf("cross-scope receipt lookup did not fail: %v", err)
		}
	}
}
