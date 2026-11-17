"""Independent disposable workspace checks; no actual agent needed here."""
import tempfile
import unittest
from pathlib import Path

from test_real_runtime import verify_tool_workspace

NONCE = "fixture_abc123XYZ"


class WorkspaceProofTests(unittest.TestCase):
    def fixture(self, root):
        (root / "calc.go").write_text(
            "package calc\nfunc Add(a, b int) int { return a + b }\n")
        (root / ".aginctus-tool-test-ok").write_text(NONCE)

    def test_requires_file_change_bash_marker_and_all_continuations(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            continuations = [(f"call_{NONCE}_{n}",) for n in range(3)]
            verify_tool_workspace(root, NONCE, 3, continuations)
            cumulative = [
                (f"call_{NONCE}_0",),
                (f"call_{NONCE}_0", f"call_{NONCE}_1"),
                (f"call_{NONCE}_0", f"call_{NONCE}_1", f"call_{NONCE}_2"),
            ]
            verify_tool_workspace(root, NONCE, 3, cumulative)
            bad = [
                (2, continuations),
                (3, continuations[:2]),
                (3, [(f"call_other_{n}",) for n in range(3)]),
            ]
            for steps, events in bad:
                with self.subTest(steps=steps, events=events), self.assertRaises(AssertionError):
                    verify_tool_workspace(root, NONCE, steps, events)
            (root / "calc.go").write_text(
                "package calc\nfunc Add(a,b int) int { return a-b }\n")
            with self.assertRaises(AssertionError):
                verify_tool_workspace(root, NONCE, 3, continuations)
            self.fixture(root)
            (root / ".aginctus-tool-test-ok").write_text("wrong-run")
            with self.assertRaises(AssertionError):
                verify_tool_workspace(root, NONCE, 3, continuations)

    def test_symlinked_marker_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            self.fixture(root)
            (root / ".aginctus-tool-test-ok").unlink()
            (root / "elsewhere").write_text(NONCE)
            (root / ".aginctus-tool-test-ok").symlink_to(root / "elsewhere")
            with self.assertRaises(AssertionError):
                verify_tool_workspace(
                    root, NONCE, 3, [(f"call_{NONCE}_{i}",) for i in range(3)]
                )


if __name__ == "__main__":
    unittest.main()
