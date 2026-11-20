package images

import (
	"context"
	"errors"
	"strings"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestCreateScopedImageChecksAndLease(t *testing.T) {
	scope, err := incusscope.NewProjectScope("tenant")
	if err != nil { t.Fatal(err) }
	for _, deny := range []string{"lease", "verify", "alias", "metadata", "rootfs", "fingerprint", "verify-second", "alias-second", "import", "verify-after", "fingerprint-after", ""} {
		t.Run("deny-"+deny, func(t *testing.T) {
			writes, releases, verifies, reads := 0, 0, 0, 0
			actions := CreateActions{
				Lease: func(got incusscope.ProjectScope) (func(), error) {
					if got.Name != "tenant" { t.Fatal("wrong project") }
					if deny == "lease" { return nil, errors.New("secret") }
					return func() { releases++ }, nil
				},
				Verify: func(context.Context, incusscope.ProjectScope) error {
					verifies++
					if (deny == "verify" && verifies == 1) ||
						(deny == "verify-second" && verifies == 2) ||
						(deny == "verify-after" && verifies == 3) { return errors.New("secret") }
					return nil
				},
				ReadAlias: func(context.Context, incusscope.ProjectScope, string) (string, bool, error) {
					reads++
					if (deny == "alias" && reads == 1) || (deny == "alias-second" && reads == 2) {
						return "foreign", true, nil
					}
					if reads == 3 {
						if deny == "fingerprint-after" { return "foreign", true, nil }
						return "expected", true, nil
					}
					return "", false, nil
				},
				Artifact: func(_ string, role, _ string) (string, error) {
					if deny == role { return "", errors.New("secret") }
					return role, nil
				},
				Fingerprint: func(string, string) (string, error) {
					if deny == "fingerprint" { return "", errors.New("secret") }
					return "expected", nil
				},
				Import: func(context.Context, incusscope.ProjectScope, string, string, string) error {
					writes++
					if deny == "import" { return errors.New("secret") }
					return nil
				},
			}
			err := CreateScopedImage(context.Background(), scope, "new", "/tmp/test", actions)
			if (err != nil) != (deny != "") { t.Fatalf("deny=%q err=%v", deny, err) }
			if err != nil && strings.Contains(err.Error(), "secret") { t.Fatal("leaked capability error") }
			if deny == "lease" {
				if releases != 0 || writes != 0 { t.Fatal("wrote without lease") }
			} else if releases != 1 { t.Fatalf("release count %d", releases) }
			if deny != "" && deny != "import" && deny != "verify-after" && deny != "fingerprint-after" && writes != 0 {
				t.Fatalf("mutation before authorization for %s", deny)
			}
		})
	}
}

func TestCreateScopedImageMissingCapabilities(t *testing.T) {
	scope, err := incusscope.NewProjectScope("tenant")
	if err != nil { t.Fatal(err) }
	if err := CreateScopedImage(context.Background(), scope, "new", "dir", CreateActions{}); err == nil {
		t.Fatal("accepted missing capabilities")
	}
}
