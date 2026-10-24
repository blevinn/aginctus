package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func fakeOperatorUnitProperties(load, active, file string) []byte {
	return []byte("LoadState=" + load + "\nActiveState=" + active + "\nUnitFileState=" + file + "\n")
}

func stubOperatorUnitProbe(t *testing.T, probe func(string, string) ([]byte, error)) {
	t.Helper()
	before := inspectOperatorUnit
	inspectOperatorUnit = func(_ context.Context, manager, unit string) ([]byte, error) {
		return probe(manager, unit)
	}
	t.Cleanup(func() { inspectOperatorUnit = before })
}

func TestClassifyOperatorUnit(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want unitEvidence
	}{
		{"missing", fakeOperatorUnitProperties("not-found", "inactive", ""), unitAbsent},
		{"disabled but installed", fakeOperatorUnitProperties("loaded", "inactive", "disabled"), unitPresent},
		{"static but installed", fakeOperatorUnitProperties("loaded", "inactive", "static"), unitPresent},
		{"failed", fakeOperatorUnitProperties("loaded", "failed", "enabled"), unitPresent},
		{"active", fakeOperatorUnitProperties("loaded", "active", "enabled"), unitActive},
		{"activating", fakeOperatorUnitProperties("loaded", "activating", "enabled"), unitActive},
		{"missing with contradictory file", fakeOperatorUnitProperties("not-found", "inactive", "enabled"), unitUnknown},
		{"masked", fakeOperatorUnitProperties("masked", "inactive", "masked"), unitUnknown},
		{"invalid active", fakeOperatorUnitProperties("loaded", "mystery", "enabled"), unitUnknown},
		{"missing property", []byte("LoadState=loaded\nActiveState=active\n"), unitUnknown},
		{"duplicate property", []byte("LoadState=loaded\nLoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), unitUnknown},
		{"unexpected output", []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\nExecStart=secret\n"), unitUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyOperatorUnit(tc.data); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOperatorUnitsAuditReport(t *testing.T) {
	missing := fakeOperatorUnitProperties("not-found", "inactive", "")
	active := fakeOperatorUnitProperties("loaded", "active", "enabled")
	installed := fakeOperatorUnitProperties("loaded", "inactive", "disabled")
	cases := []struct {
		name          string
		owner         string
		system        []byte
		user          []byte
		otherError    bool
		wantCode      int
		wantUnitState string
	}{
		{"empty user owner", "user", missing, missing, false, 0, "system=absent user=absent"},
		{"empty system owner", "system", missing, missing, false, 0, "system=absent user=absent"},
		{"correct user manager", "user", missing, active, false, 0, "system=absent user=active"},
		{"correct system manager", "system", active, missing, false, 0, "system=active user=absent"},
		{"user conflicts with installed system", "user", installed, missing, false, 1, "system=installed user=absent"},
		{"system conflicts with active user", "system", missing, active, false, 1, "system=absent user=active"},
		{"both owners configured", "system", installed, installed, false, 1, "system=installed user=installed"},
		{"user bus denied", "system", missing, missing, true, 1, "system=absent user=unverified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			stubOperatorUnitProbe(t, func(manager, unit string) ([]byte, error) {
				calls = append(calls, manager+":"+unit)
				if !strings.HasSuffix(unit, ".service") {
					t.Errorf("unexpected unit %q", unit)
				}
				if manager == "system" {
					return tc.system, nil
				}
				if manager == "user" {
					if tc.otherError {
						return nil, errors.New("private-unit-env-value-must-never-be-printed")
					}
					return tc.user, nil
				}
				t.Fatalf("unexpected manager %q", manager)
				return nil, nil
			})
			var stdout, stderr bytes.Buffer
			code := renderOperatorUnitsAudit(context.Background(), tc.owner, &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("got code %d, want %d; out=%q err=%q", code, tc.wantCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantUnitState) {
				t.Errorf("missing %q in %q", tc.wantUnitState, stdout.String())
			}
			if len(calls) != 2*len(operatorLifecycleUnits) {
				t.Errorf("probe call count %d, want %d", len(calls), 2*len(operatorLifecycleUnits))
			}
			if strings.Contains(stdout.String()+stderr.String(), "private-unit-env-value") {
				t.Fatal("raw systemctl error leaked")
			}
		})
	}
}

func TestOperatorUnitsAuditInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"--owner"}, {"--owner=admin"}, {"--owner=user", "--force"},
		{"--owner", "root"}, {"--owner", "user", "--fix"},
	} {
		var stdout, stderr bytes.Buffer
		if got := runOperatorUnitsAudit(context.Background(), args, &stdout, &stderr); got != 2 {
			t.Errorf("args=%q returned code=%d, want 2", args, got)
		}
	}
	for _, args := range [][]string{{"--owner=user"}, {"--owner=system"}, {"--owner", "user"}, {"--owner", "system"}} {
		owner, ok := parseOperatorUnitsAuditArgs(args)
		if !ok || owner == "" {
			t.Errorf("did not parse valid owner %q", args)
		}
	}
}
