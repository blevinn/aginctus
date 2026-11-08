"""Static regressions for the GitHub Actions concurrency trust boundary."""

import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
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

    def test_required_status_name_is_preserved(self):
        self.assertIn("name: hermetic validation (ubuntu)", HERMETIC.read_text())


if __name__ == "__main__":
    unittest.main()
