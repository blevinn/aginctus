"""No-daemon tests for real-gateway scripted function-call transport checks."""
import copy
import json
import unittest

from scripts.vm_harness.gateway_tool_transport import (
    TransportFailure,
    completed_item,
    probe_gateway_tool_transport,
)
from scripts.vm_harness.tool_sequence import ToolSequence, stream

NONCE = "transport_nonce_1234"


def fixture_stream(nonce=NONCE, step=0):
    from scripts.vm_harness.tool_sequence import fixture_response
    return stream(fixture_response(nonce, step, "/workspace", "synthetic-test"))


class TransportTests(unittest.TestCase):
    def test_valid_response_supports_event_prefixed_and_data_only(self):
        for step in range(4):
            raw = fixture_stream(step=step)
            identity, call_id = completed_item(raw, NONCE, step)
            self.assertEqual(identity, f"resp_{NONCE}_{step}")
            self.assertEqual(call_id, f"call_{NONCE}_{step}" if step < 3 else None)
            data_only = b"\n".join(
                line for line in raw.split(b"\n") if not line.startswith(b"event:")
            )
            self.assertEqual(completed_item(data_only, NONCE, step)[0], identity)

    def test_missing_completion_wrong_identity_and_forgery_rejected(self):
        raw = fixture_stream()
        invalid = (
            raw.replace(b"response.completed", b"response.incomplete"),
            raw.replace(f"call_{NONCE}_0".encode(), b"call_spoofed_0"),
            raw.replace(f"resp_{NONCE}_0".encode(), b"bad-response-id"),
            raw.replace(b'"name":"read"', b'"name":"bash"'),
            raw.replace(b'data: [DONE]', b'data: {}\n\ndata: [DONE]'),
            raw + b"data: {}\n\n",
        )
        for item in invalid:
            with self.subTest(input=item[:50]), self.assertRaises(TransportFailure):
                completed_item(item, NONCE, 0)
        with self.assertRaises(TransportFailure):
            completed_item(raw, NONCE, 3)

    def test_four_turns_forward_opaque_previous_identity_unchanged(self):
        sequence = ToolSequence()
        turns = []
        def request(_gateway, path, token, body=None):
            if path == "/_observed/" + NONCE:
                self.assertEqual(token, "dummy")
                return json.dumps({"count": 4}).encode()
            self.assertEqual(path, "/v1/responses")
            self.assertEqual(token, "scoped")
            if not turns:
                self.assertEqual(body["model"], "synthetic-alias")
                self.assertEqual(len(body["tools"]), 3)
            else:
                self.assertEqual(body["previous_response_id"],
                                 f"resp_gatewayOpaque_{len(turns)-1}")
                self.assertEqual(body["input"][0]["call_id"],
                                 f"call_{NONCE}_{len(turns)-1}")
                self.assertIn("no tool executed", body["input"][0]["output"])
            upstream_body = copy.deepcopy(body)
            upstream_body["model"] = "synthetic-test"
            if turns:
                upstream_body["previous_response_id"] = f"resp_{NONCE}_{len(turns)-1}"
            nonce, response = sequence.next(upstream_body)
            self.assertEqual(nonce, NONCE)
            turns.append(response)
            gateway_response = copy.deepcopy(response)
            gateway_response["id"] = f"resp_gatewayOpaque_{len(turns)-1}"
            return stream(gateway_response)
        result = probe_gateway_tool_transport(
            request, "http://gateway", "scoped", "http://upstream",
            "dummy", "synthetic-alias", NONCE,
        )
        self.assertTrue(result)
        self.assertEqual(len(turns), 4)
        self.assertEqual(turns[-1]["output"][0]["type"], "message")

    def test_count_is_not_a_guest_tool_receipt(self):
        seq = ToolSequence()
        def request(_base, path, _token, body=None):
            if path.startswith("/_observed/"):
                return b'{"count":3}'
            body = {**body, "model": "synthetic-test"}
            return stream(seq.next(body)[1])
        with self.assertRaises(TransportFailure):
            probe_gateway_tool_transport(
                request, "http://gateway", "scoped", "http://upstream",
                "dummy", "synthetic-alias", NONCE,
            )


if __name__ == "__main__":
    unittest.main()
