package incus

import (
	"maps"
	"strings"
	"testing"
)

func TestProposedRestrictedOperatorConfigHasNarrowExplicitGrants(t *testing.T) {
	scope, err := NewProjectScope("aginctus")
	if err != nil {
		t.Fatal(err)
	}
	base := scope.DesiredConfig()
	cfg, err := scope.ProposedRestrictedOperatorConfig("hm-mgmt", "default")
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"user.aginctus.managed": "true",
		"user.aginctus.resource": "deployment-project",
		"user.aginctus.deployment": "aginctus",
		"restricted": "true",
		"restricted.devices.nic": "managed",
		"restricted.networks.access": "hm-mgmt",
		"restricted.storage-pools.access": "default",
		"features.images": "true",
		"features.profiles": "true",
		"features.storage.volumes": "true",
		"features.networks": "false",
	} {
		if cfg[k] != want {
			t.Fatalf("%s: got %q, want %q", k, cfg[k], want)
		}
	}
	if base["restricted"] != "" {
		t.Fatal("candidate unexpectedly changed legacy default project policy")
	}
	if got := scope.AssessProposedRestrictedOperatorConfig("aginctus", cfg, "hm-mgmt", "default"); got != ProjectCompatible {
		t.Fatalf("candidate mismatch: %s", got)
	}
}

func TestProposedRestrictedOperatorConfigRefusesImplicitOrBroadAccess(t *testing.T) {
	scope, _ := NewProjectScope("aginctus")
	for _, tc := range []struct{ bridge, pool string }{
		{"", "default"}, {"*", "default"}, {"hm-mgmt,other", "default"},
		{"../admin", "default"}, {"hm/mgmt", "default"},
		{strings.Repeat("n", 16), "default"}, {"hm-mgmt", ""},
		{"hm-mgmt", "*"}, {"hm-mgmt", "default,other"},
		{"hm-mgmt", "../default"}, {"hm-mgmt", "invalid pool"},
	} {
		if _, err := scope.ProposedRestrictedOperatorConfig(tc.bridge, tc.pool); err == nil {
			t.Errorf("accepted broad/invalid bridge %q pool %q", tc.bridge, tc.pool)
		}
	}
	if _, err := (ProjectScope{Name: "default"}).ProposedRestrictedOperatorConfig("hm-mgmt", "default"); err == nil {
		t.Fatal("accepted default project delegation")
	}
}

func TestRestrictedOperatorAssessmentRejectsPolicyWeakeningAndOwnershipChanges(t *testing.T) {
	scope, _ := NewProjectScope("aginctus")
	candidate, _ := scope.ProposedRestrictedOperatorConfig("hm-mgmt", "default")
	for _, tc := range []struct {
		name, key, value string
		want ProjectAssessment
	}{
		{"unrestricted", "restricted", "false", ProjectDrift},
		{"different bridge", "restricted.networks.access", "other-bridge", ProjectDrift},
		{"wildcard pool", "restricted.storage-pools.access", "*", ProjectDrift},
		{"broad NIC", "restricted.devices.nic", "allow", ProjectDrift},
		{"inherited features drift", "features.networks", "true", ProjectDrift},
		{"owner rewritten", "user.aginctus.deployment", "foreign", ProjectForeign},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := maps.Clone(candidate)
			observed[tc.key] = tc.value
			if got := scope.AssessProposedRestrictedOperatorConfig("aginctus", observed, "hm-mgmt", "default"); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	otherName := scope.AssessProposedRestrictedOperatorConfig("foreign", candidate, "hm-mgmt", "default")
	if otherName != ProjectForeign {
		t.Fatal("accepted a foreign project name")
	}
	for _, extra := range []string{"restricted.networks.uplinks", "features.networks.zones"} {
		observed := maps.Clone(candidate)
		observed[extra] = "true"
		if got := scope.AssessProposedRestrictedOperatorConfig("aginctus", observed, "hm-mgmt", "default"); got != ProjectDrift {
			t.Fatalf("accepted unexpected scope policy %s", extra)
		}
	}
	if got := scope.AssessProposedRestrictedOperatorConfig("aginctus", candidate, "other-bridge", "default"); got != ProjectDrift {
		t.Fatalf("accepted mismatched external approved bridge: %s", got)
	}
}
