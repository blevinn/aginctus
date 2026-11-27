package testenv

import "testing"

func TestNativeScenarioStateStrict(t *testing.T) {
	const good = "LoadState=loaded\nActiveState=active\nSubState=exited\nResult=success\nExecMainCode=1\nExecMainStatus=0\n"
	state, err := parseNativeScenarioState(good)
	if err != nil || state.active != "active" || state.status != "0" {
		t.Fatalf("state: %+v %v", state, err)
	}
	for _, bad := range []string{"", good+"ActiveState=failed\n", good+"Extra=field\n", good[:len(good)-17], "LoadState=not-found\n" + good} {
		if _, err := parseNativeScenarioState(bad); err == nil {
			t.Errorf("malformed state accepted")
		}
	}
}

func TestNativeScenarioTerminalFailure(t *testing.T) {
	for _, tc := range []struct {
		code, status, want string
	}{
		{"1", "64", "native_instance_scenario_step_0"},
		{"1", "76", "native_instance_scenario_step_12"},
		{"1", "127", "native_instance_scenario_step_63"},
		{"1", "128", "native_instance_scenario_unit_failed"},
		{"2", "76", "native_instance_scenario_unit_failed"},
		{"1", "private", "native_instance_scenario_unit_failed"},
	} {
		if got := nativeScenarioFailureStage(nativeScenarioTerminalFailure(nativeScenarioState{code: tc.code, status: tc.status})); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}

func TestNativeAssertionLineStrict(t *testing.T) {
	if line, ok := parseNativeAssertionLine("134\n"); !ok || line != 134 {
		t.Fatalf("valid assertion line rejected: %d %v", line, ok)
	}
	for _, bad := range []string{"", "0\n", "10000\n", "01\n", "+1\n", "1", "1\n2\n", "private output\n"} {
		if _, ok := parseNativeAssertionLine(bad); ok {
			t.Fatalf("invalid assertion line accepted: %q", bad)
		}
	}
}
