package testenv

import (
	"errors"
	"strings"
	"testing"
)

func TestVMRefInstanceNameIncusBoundaries(t *testing.T) {
	// Incus instance.ValidName uses validate.IsHostname: 1-63 characters,
	// with no leading or trailing hyphen. Admission is lowercase letter-first.
	for _, tc := range []struct {
		name     string
		instance string
		valid    bool
	}{
		{"single letter", "a", true},
		{"final digit", "vm-1", true},
		{"internal hyphen", "testenv-vm", true},
		{"63 characters", "a" + strings.Repeat("b", 62), true},
		{"63 characters with internal hyphens", "a" + strings.Repeat("-", 61) + "1", true},
		{"empty", "", false},
		{"trailing hyphen", "testenv-vm-", false},
		{"63 characters with trailing hyphen", "a" + strings.Repeat("b", 61) + "-", false},
		{"64 characters", "a" + strings.Repeat("b", 63), false},
		{"leading hyphen", "-vm", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := VMRef{
				Backend: IncusBackend, Project: DefaultVMProject,
				Instance:         tc.instance,
				RunID:            "vm-" + strings.Repeat("a", 24),
				ImageFingerprint: strings.Repeat("b", 64),
			}
			err := ref.Validate()
			if tc.valid && err != nil {
				t.Fatalf("rejected valid instance name %q: %v", tc.instance, err)
			}
			if !tc.valid && !errors.Is(err, ErrOwnershipMismatch) {
				t.Fatalf("invalid instance name %q: got %v, want ErrOwnershipMismatch", tc.instance, err)
			}
		})
	}
}
