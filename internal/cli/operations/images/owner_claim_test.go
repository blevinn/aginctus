package imageops

import (
	"encoding/json"
	"strings"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// Ownership claim classification is diagnostic evidence, never authorization.
func ownerClaimFixtureJSON(aliases []string, properties map[string]string) []byte {
	type imgAlias struct {
		Name string `json:"name"`
	}
	type image struct {
		Fingerprint string            `json:"fingerprint"`
		Aliases     []imgAlias        `json:"aliases"`
		Properties  map[string]string `json:"properties"`
	}
	var names []imgAlias
	for _, name := range aliases {
		names = append(names, imgAlias{Name: name})
	}
	raw, _ := json.Marshal([]image{{
		Fingerprint: strings.Repeat("a", 64),
		Aliases:     names,
		Properties:  properties,
	}})
	return raw
}

func TestImageOwnerClaimClassification(t *testing.T) {
	scope, err := incusscope.NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	claim := ExpectedOwnerClaim(scope, "test-image", "system")
	copied := func() map[string]string {
		v := make(map[string]string, len(claim))
		for k, x := range claim {
			v[k] = x
		}
		return v
	}
	foreign := copied()
	foreign["user.aginctus.owner"] = "user"
	incomplete := copied()
	delete(incomplete, "user.aginctus.alias")
	wrongProject := copied()
	wrongProject["user.aginctus.deployment"] = "other"
	unmarked := map[string]string{"release": "26.05"}
	cases := []struct {
		name       string
		payload    []byte
		wantStatus string
	}{
		{"complete claim", ownerClaimFixtureJSON([]string{"test-image"}, copied()), "claimed"},
		{"unmarked image", ownerClaimFixtureJSON([]string{"test-image"}, unmarked), "unmarked"},
		{"incomplete claim", ownerClaimFixtureJSON([]string{"test-image"}, incomplete), "foreign-or-incomplete"},
		{"other owner", ownerClaimFixtureJSON([]string{"test-image"}, foreign), "foreign-or-incomplete"},
		{"other project", ownerClaimFixtureJSON([]string{"test-image"}, wrongProject), "foreign-or-incomplete"},
		{"shared fingerprint", ownerClaimFixtureJSON([]string{"test-image", "other-alias"}, copied()), "shared"},
		{"missing alias", ownerClaimFixtureJSON([]string{"other-alias"}, copied()), "absent"},
		{"no image", []byte("[]"), "absent"},
		{"duplicate same image", ownerClaimFixtureJSON([]string{"test-image", "test-image"}, copied()), "ambiguous"},
		{"duplicate across images", []byte("[" +
			strings.Trim(string(ownerClaimFixtureJSON([]string{"test-image"}, copied())), "[]") + "," +
			strings.Trim(string(ownerClaimFixtureJSON([]string{"test-image"}, copied())), "[]") + "]"), "ambiguous"},
		{"malformed fingerprint", []byte(`[{"fingerprint":"not-a-hash","aliases":[{"name":"test-image"}],"properties":{"user.aginctus.managed":"true"}}]`), "unverified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var observed []ObservedClaimedImage
			if err := json.Unmarshal(tc.payload, &observed); err != nil {
				t.Fatalf("invalid test fixture: %v", err)
			}
			if got := ClassifyOwnerClaim(observed, scope, "test-image", "system"); got != tc.wantStatus {
				t.Fatalf("claim = %q, want %q", got, tc.wantStatus)
			}
		})
	}
}

func TestImageOwnerAuditOtherLifecycleOwnerNotClaimed(t *testing.T) {
	scope, _ := incusscope.NewProjectScope("aginctus")
	properties := ExpectedOwnerClaim(scope, "test-image", "system")
	var images []ObservedClaimedImage
	if err := json.Unmarshal(ownerClaimFixtureJSON([]string{"test-image"}, properties), &images); err != nil {
		t.Fatal(err)
	}
	if status := ClassifyOwnerClaim(images, scope, "test-image", "user"); status != "foreign-or-incomplete" {
		t.Fatalf("user audit may not claim a system-owned image: %q", status)
	}
}
