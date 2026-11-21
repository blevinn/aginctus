package images

import (
	"context"
	"errors"
	"strings"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestCreateClaimedImageSafetyGates(t *testing.T) {
	scope, err := incusscope.NewProjectScope("tenant")
	if err != nil { t.Fatal(err) }
	owner, alias, fingerprint := "system", "target", strings.Repeat("a", 64)
	for _, deny := range []string{"lease", "verify-first", "inventory-first", "metadata", "rootfs", "fingerprint", "adopt", "verify-second", "inventory-second", "import", "verify-post", "claim-post", "fingerprint-post", ""} {
		t.Run("gate-"+deny, func(t *testing.T) {
			verifies, inventories, imports, releases := 0, 0, 0, 0
			actions := OwnedCreateActions{
				Lease: func(s incusscope.ProjectScope) (func(), error) {
					if s.Name != scope.Name { t.Fatal("wrong project lease") }
					if deny == "lease" { return nil, errors.New("private") }
					return func() { releases++ }, nil
				},
				Verify: func(context.Context, incusscope.ProjectScope) error {
					verifies++
					if (deny == "verify-first" && verifies == 1) || (deny == "verify-second" && verifies == 2) || (deny == "verify-post" && verifies == 3) {
						return errors.New("private")
					}
					return nil
				},
				Inventory: func(context.Context, incusscope.ProjectScope) ([]ObservedClaimedImage, error) {
					inventories++
					if (deny == "inventory-first" && inventories == 1) || (deny == "inventory-second" && inventories == 2) {
						return nil, errors.New("private")
					}
					if inventories == 3 {
						img := ObservedClaimedImage{Fingerprint: fingerprint, Properties: ExpectedOwnerClaim(scope, alias, owner)}
						img.Aliases = append(img.Aliases, struct{ Name string `json:"name"` }{Name: alias})
						if deny == "claim-post" { img.Properties["user.aginctus.owner"] = "foreign" }
						if deny == "fingerprint-post" { img.Fingerprint = strings.Repeat("b", 64) }
						return []ObservedClaimedImage{img}, nil
					}
					if deny == "adopt" && inventories == 1 {
						return []ObservedClaimedImage{{Fingerprint: fingerprint}}, nil
					}
					return []ObservedClaimedImage{}, nil
				},
				Artifact: func(_ string, kind, _ string) (string, error) {
					if kind == deny { return "", errors.New("private") }
					return kind, nil
				},
				Fingerprint: func(string, string) (string, error) {
					if deny == "fingerprint" { return "", errors.New("private") }
					return fingerprint, nil
				},
				Import: func(_ context.Context, s incusscope.ProjectScope, _, _, gotAlias, gotOwner string) error {
					if s.Name != scope.Name || gotAlias != alias || gotOwner != owner { t.Fatal("wrong mutation scope") }
					imports++
					if deny == "import" { return errors.New("private") }
					return nil
				},
			}
			err := CreateClaimedImage(context.Background(), scope, owner, alias, "/tmp/image", actions)
			if (err != nil) != (deny != "") { t.Fatalf("denial %q: %v", deny, err) }
			if err != nil && strings.Contains(err.Error(), "private") { t.Fatal("capability error leaked") }
			if deny == "lease" {
				if releases != 0 || imports != 0 { t.Fatal("import attempted without lease") }
			} else if releases != 1 { t.Fatalf("lease released %d times", releases) }
			if deny != "" && deny != "import" && deny != "verify-post" && deny != "claim-post" && deny != "fingerprint-post" && imports != 0 {
				t.Fatalf("mutation before authorization gate %s", deny)
			}
			if deny == "" && imports != 1 { t.Fatal("successful creation did not import once") }
		})
	}
}

func TestCreateClaimedImageRejectsMissingCapabilities(t *testing.T) {
	scope, err := incusscope.NewProjectScope("tenant")
	if err != nil { t.Fatal(err) }
	if err := CreateClaimedImage(context.Background(), scope, "system", "target", "dir", OwnedCreateActions{}); err == nil {
		t.Fatal("missing capabilities accepted")
	}
}
