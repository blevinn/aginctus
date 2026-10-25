"""Pure smoke contract regression tests: no Incus daemon or LiteLLM."""
import json
import unittest
from unittest.mock import patch

from scripts.vm_harness import synthetic_gateway_probe as probe
from scripts.vm_harness import synthetic_responses as upstream


class SyntheticGatewayProbeTests(unittest.TestCase):
    def test_upstream_credential_matches_mock_only(self):
        self.assertEqual(probe.TOKEN, upstream.TOKEN)
        self.assertNotIn("sk-", probe.TOKEN)

    def test_private_networks_and_unique_litellm_address(self):
        with patch.object(probe, "incus", return_value="10.234.1.1/24"):
            mgmt = probe.network_ipv4("aginctus-mgmt")
        self.assertEqual(str(mgmt.ip), "10.234.1.1")
        with patch.object(probe, "incus", return_value="0.0.0.0/0"):
            with self.assertRaises(probe.Failure):
                probe.network_ipv4("aginctus-egress")
        info = {"network": {
            "eth0": {"addresses": [
                {"family": "inet", "address": "10.234.1.5"},
                {"family": "inet6", "address": "fe80::1"},
            ]},
            "lo": {"addresses": [{"family": "inet", "address": "127.0.0.1"}]},
        }}
        with patch.object(probe, "incus", return_value=json.dumps(info)):
            self.assertEqual(probe.lite_ipv4(mgmt), "10.234.1.5")
        info["network"]["eth1"] = {
            "addresses": [{"family": "inet", "address": "10.234.1.10"}]}
        with patch.object(probe, "incus", return_value=json.dumps(info)):
            with self.assertRaises(probe.Failure):
                probe.lite_ipv4(mgmt)

    def test_stream_requires_real_terminal_and_unique_nonce(self):
        nonce = "vmfixture_123456789abc"
        probe.verify_stream(upstream.sse(nonce), nonce)
        for broken in (
            upstream.sse(nonce).replace(b"completed", b"incomplete"),
            upstream.sse(nonce).replace(b"data: [DONE]", b"data: []"),
            upstream.sse(nonce).replace(nonce.encode(), b"othernonce"),
            b"event: response.completed\ndata: {}\n\n",
        ):
            with self.subTest(fragment=broken[:60]), self.assertRaises(probe.Failure):
                probe.verify_stream(broken, nonce)

    def test_model_listing_rejects_invalid_schema(self):
        self.assertEqual(
            probe.model_ids(b'{"data":[{"id":"aginctus-vm-synthetic"}]}'),
            {"aginctus-vm-synthetic"},
        )
        for bad in (b"{}", b'{"data":"wrong"}', b'{"data":[{"id":3}]}'):
            with self.subTest(doc=bad), self.assertRaises(probe.Failure):
                probe.model_ids(bad)


if __name__ == "__main__":
    unittest.main()
