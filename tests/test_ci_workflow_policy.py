"""Static regressions for the GitHub Actions concurrency trust boundary."""

from pathlib import Path
import unittest


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

    def test_required_status_name_is_preserved(self):
        self.assertIn("name: hermetic validation (ubuntu)", HERMETIC.read_text())


if __name__ == "__main__":
    unittest.main()
