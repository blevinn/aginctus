package operatorops

import "testing"

func unitPropertiesFixture(load, active, file string) []byte {
	return []byte("LoadState=" + load + "\nActiveState=" + active + "\nUnitFileState=" + file + "\n")
}

// Fail-closed unit classification lives with the operator evidence.
func TestClassifyOperatorUnit(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want UnitEvidence
	}{
		{"missing", unitPropertiesFixture("not-found", "inactive", ""), UnitAbsent},
		{"disabled but installed", unitPropertiesFixture("loaded", "inactive", "disabled"), UnitPresent},
		{"static but installed", unitPropertiesFixture("loaded", "inactive", "static"), UnitPresent},
		{"failed", unitPropertiesFixture("loaded", "failed", "enabled"), UnitPresent},
		{"active", unitPropertiesFixture("loaded", "active", "enabled"), UnitActive},
		{"activating", unitPropertiesFixture("loaded", "activating", "enabled"), UnitActive},
		{"missing with contradictory file", unitPropertiesFixture("not-found", "inactive", "enabled"), UnitUnknown},
		{"masked", unitPropertiesFixture("masked", "inactive", "masked"), UnitUnknown},
		{"invalid active", unitPropertiesFixture("loaded", "mystery", "enabled"), UnitUnknown},
		{"missing property", []byte("LoadState=loaded\nActiveState=active\n"), UnitUnknown},
		{"duplicate property", []byte("LoadState=loaded\nLoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), UnitUnknown},
		{"unexpected output", []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\nExecStart=secret\n"), UnitUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyUnit(tc.data); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
