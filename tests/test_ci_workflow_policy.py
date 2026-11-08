"""Static regressions for the GitHub Actions concurrency trust boundary."""

import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
HERMETIC = ROOT / ".github" / "workflows" / "hermetic.yml"


class WorkflowConcurrencyTests(unittest.TestCase):
    def test_main_and_dispatch_runs_are_not_superseded(self):
        workflow = HERMETIC.read_text(encoding="utf-8")
        # PR numbers are stable across commits; each main push/dispatch
        # receives a unique run_id even when its previous run is unfinished.
        self.assertIn("github.event.pull_request.number || github.run_id", workflow)
        self.assertIn(
            "cancel-in-progress: ${{ github.event_name == 'pull_request' }}",
            workflow,
        )
        self.assertNotIn("cancel-in-progress: true", workflow)

    def test_parallel_suites_do_not_require_nix_develop(self):
        workflow = HERMETIC.read_text(encoding="utf-8")
        for name in ("go-fast:", "static-fast:"):
            self.assertIn("  " + name, workflow)
        fast = workflow.split("  go-fast:", 1)[1]
        self.assertNotIn("nix develop", fast)
        self.assertIn("GOPROXY: \"off\"", fast)
        self.assertIn("AGINCTUS_INCUS_INTEGRATION: \"0\"", fast)

    def test_required_gate_is_fail_closed_over_parallel_jobs(self):
        workflow = HERMETIC.read_text(encoding="utf-8")
        self.assertIn("name: hermetic validation (ubuntu)", workflow)
        self.assertIn("needs: [go-fast, static-fast, changes, nix-contracts]", workflow)
        self.assertIn("always() && github.event_name == 'pull_request'", workflow)
        self.assertIn('test "$CHANGES_RESULT" = success', workflow)
        self.assertIn('true) test "$NIX_RESULT" = success', workflow)
        self.assertIn('false) test "$NIX_RESULT" = skipped', workflow)
        self.assertIn('git diff --check "$BASE_SHA" HEAD', workflow)

    def test_deep_regression_is_durable_non_pr(self):
        workflow = HERMETIC.read_text(encoding="utf-8")
        self.assertIn("name: comprehensive hermetic regression (ubuntu)", workflow)
        self.assertIn("github.event_name != 'pull_request'", workflow)
        self.assertIn("schedule:", workflow)
        self.assertIn("nix --quiet develop --no-update-lock-file -c just check report-json", workflow)

    def test_required_status_name_is_preserved(self):
        self.assertIn("name: hermetic validation (ubuntu)", HERMETIC.read_text())


if __name__ == "__main__":
    unittest.main()
