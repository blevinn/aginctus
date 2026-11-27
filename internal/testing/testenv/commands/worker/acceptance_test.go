package worker

import "testing"

func TestAcceptanceFailureStageAllowlist(t *testing.T) {
	for _, stage := range []string{"configuration", "worker_identity", "worker_marker", "worker_credential", "report_root", "report_lock", "report_initialize", "evidence", "worker_daemon", "preflight", "ephemeral_create", "ephemeral_create_wait_size", "retained_create_ownership", "retained_create_start_wait_timeout", "native_instance_scenario_initial_parse", "native_instance_scenario_step_12", "native_instance_scenario_assertion_line_134", "native_instance_scenario_unit_failed", "cleanup"} {
		if got := acceptanceFailureStage(stage); got != stage {
			t.Fatalf("stage %q mapped to %q", stage, got)
		}
	}
	for _, stage := range []string{"", "private-credential-secret", "/private/path", "native_instance_scenario_step_64", "native_instance_scenario_step_01", "native_instance_scenario_step_secret", "native_instance_scenario_assertion_line_0", "native_instance_scenario_assertion_line_10000", "native_instance_scenario_assertion_line_0134", "native_instance_scenario_assertion_line_private"} {
		if got := acceptanceFailureStage(stage); got != "unknown" {
			t.Fatal("unexpected stage disclosure", got)
		}
	}
}
