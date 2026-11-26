package worker

import "testing"

func TestAcceptanceFailureStageAllowlist(t *testing.T) {
	for _, stage := range []string{"configuration", "worker_identity", "worker_marker", "worker_credential", "report_root", "report_lock", "report_initialize", "evidence", "worker_daemon", "preflight", "ephemeral_create", "cleanup"} {
		if got := acceptanceFailureStage(stage); got != stage {
			t.Fatalf("stage %q mapped to %q", stage, got)
		}
	}
	for _, stage := range []string{"", "private-credential-secret", "/private/path"} {
		if got := acceptanceFailureStage(stage); got != "unknown" {
			t.Fatal("unexpected stage disclosure", got)
		}
	}
}
