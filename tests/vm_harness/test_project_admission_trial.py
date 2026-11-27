"""Hostless checks for provider admission denial classification."""

import importlib.util
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

SPEC = importlib.util.spec_from_file_location(
    "project_admission_trial",
    Path(__file__).resolve().parents[2] / "internal/testing/testenv/nix/project_admission_trial.py",
)
admission = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(admission)


class ProjectAdmissionDenialTest(unittest.TestCase):
    def test_count_denials_and_fail_closed(self):
        project = admission.ADMISSION_PROJECT
        messages = (
            f'Reached maximum number of instances of type "virtual-machine" in project "{project}"',
            f'Reached maximum number of instances in project "{project}"',
        )
        for status, output, after, allowed in (
            (1, messages[0], "baseline", True),
            (1, messages[1], "baseline", True),
            (1, "storage failure", "baseline", False),
            (1, messages[0].replace(project, "other-project"), "baseline", False),
            (0, messages[0], "baseline", False),
            (1, messages[0], "changed", False),
        ):
            with self.subTest(status=status, output=output, after=after):
                machine = Mock()
                machine.execute.return_value = status, output
                with patch.object(admission, "_admission_inventory", side_effect=["baseline", after]):
                    if allowed:
                        admission._admission_denied(machine, "count", "command", messages)
                    else:
                        with self.assertRaises(RuntimeError):
                            admission._admission_denied(machine, "count", "command", messages)

    def test_single_expected_message(self):
        machine = Mock()
        machine.execute.return_value = 1, "Network devices are forbidden"
        with patch.object(admission, "_admission_inventory", return_value="baseline"):
            admission._admission_denied(machine, "nic", "command", "Network devices are forbidden")


if __name__ == "__main__":
    unittest.main()
