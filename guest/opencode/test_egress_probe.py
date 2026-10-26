"""Hostless exact TCP verdict tests: no sockets are opened by this suite."""

import errno
import importlib.util
import io
import json
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "egress_probe", Path(__file__).with_name("egress_probe.py")
)
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)


class SocketCase:
    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False


class GuestNetworkProbeTests(unittest.TestCase):
    def test_positive_gateway_socket_only(self):
        calls = []

        def connected(address, timeout):
            calls.append((address, timeout))
            return SocketCase()

        self.assertEqual(probe.observe("gateway", "10.42.40.9", connected), "pass")
        self.assertEqual(calls, [(("10.42.40.9", 4000), 3)])
        with self.assertRaises(ValueError):
            probe.observe("gateway", "1.1.1.1", connected)
        self.assertEqual(len(calls), 1)

    def test_public_no_route_is_the_only_passing_negative_error(self):
        for code in (errno.ECONNREFUSED, errno.ETIMEDOUT, errno.EHOSTUNREACH):
            with self.subTest(errno=code):
                def fail(_address, timeout, errno_=code):
                    raise OSError(errno_, "sensitive network diagnostic")
                self.assertEqual(probe.observe("direct", "1.1.1.1", fail), "error")

        def no_route(_address, timeout):
            raise OSError(errno.ENETUNREACH, "sensitive")
        self.assertEqual(probe.observe("direct", "1.1.1.1", no_route), "pass")
        self.assertEqual(
            probe.observe("direct", "1.1.1.1", lambda *_args, **_kwargs: SocketCase()), "fail"
        )

    def test_target_allowlist_rejects_non_ip_and_private_direct(self):
        for mode, value in (
            ("direct", "10.0.0.1"), ("direct", "127.0.0.1"),
            ("direct", "192.168.1.1"), ("direct", "169.254.1.1"),
            ("direct", "localhost"), ("direct", "8.8.8.8:443"),
            ("direct", "1.1.1.1\n"),
            ("gateway", "0.0.0.0"), ("gateway", "127.0.0.1"),
            ("gateway", "8.8.8.8"), ("gateway", "10.00.0.1"),
            ("other", "10.0.0.1"),
        ):
            with self.subTest(mode=mode, value=value), self.assertRaises(ValueError):
                probe.valid_target(mode, value)

    def test_output_fixed_status_does_not_echo_error_or_address(self):
        output = io.StringIO()
        with (
            patch.object(probe, "observe", side_effect=OSError("PRIVATE SECRET")),
            redirect_stdout(output),
        ):
            self.assertEqual(probe.main(["gateway", "10.42.40.9"]), 1)
        record = json.loads(output.getvalue())
        self.assertEqual(record, {
            "schema_version": 1, "profile": "guest_tcp_probe",
            "check": "gateway_tcp", "status": "error",
        })
        self.assertNotIn("SECRET", output.getvalue())
        self.assertNotIn("10.42", output.getvalue())
        output = io.StringIO()
        with redirect_stdout(output):
            self.assertEqual(probe.main(["direct", "not-an-ip"]), 1)
        self.assertEqual(json.loads(output.getvalue())["status"], "error")

    def test_actual_socket_is_never_called_for_bad_target(self):
        with (
            patch.object(probe.socket, "create_connection") as connect,
            redirect_stdout(io.StringIO()),
        ):
            self.assertEqual(probe.main(["direct", "127.0.0.1"]), 1)
        connect.assert_not_called()


if __name__ == "__main__":
    unittest.main()
