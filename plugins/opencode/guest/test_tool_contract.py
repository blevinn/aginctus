"""No provider or external binary needed; tools are synthetic test declarations."""
import unittest

from tool_contract import ContractError, declarations, results


class ToolContractTests(unittest.TestCase):
    def test_declared_schema_names_and_fingerprints_only(self):
        data = {"tools": [
            {"type": "function", "name": "read", "description": "SECRET_PROMPT",
             "parameters": {"type": "object", "properties": {"filePath": {"type": "string"}}}},
            {"type": "function", "name": "bash", "parameters": {"type": "object"}},
        ]}
        out = declarations(data)
        self.assertEqual([name for name, _ in out], ["bash", "read"])
        self.assertTrue(all(len(digest) == 64 for _, digest in out))
        self.assertNotIn("SECRET_PROMPT", str(out))
        self.assertEqual(out, declarations(data))

    def test_refuses_untyped_duplicates_and_oversized_schemas(self):
        good = {"type": "function", "name": "read", "parameters": {"type": "object"}}
        for bad in (None, [], [good, good], [{"type": "text", "name": "read"}],
                    [{"type": "function", "name": "sh", "parameters": {"type": "string"}}],
                    [{"type": "function", "name": "read", "parameters": {"type": "object",
                      "description": "x" * 65536}}]):
            with self.subTest(bad=str(bad)[:40]), self.assertRaises(ContractError):
                declarations({"tools": bad})

    def test_tool_result_continuations_contain_only_call_ids(self):
        request = {"input": [
            {"type": "message", "content": "ignore"},
            {"type": "function_call_output", "call_id": "call_abc", "output": "SECRET"},
        ]}
        self.assertEqual(results(request), ("call_abc",))
        self.assertNotIn("SECRET", str(results(request)))
        for bad in ("../../pilot", "abc", "call_"):
            with self.assertRaises(ContractError):
                results({"input": [{"type": "function_call_output", "call_id": bad}]})
        with self.assertRaises(ContractError):
            results({"input": [request["input"][1], request["input"][1]]})


if __name__ == "__main__":
    unittest.main()
