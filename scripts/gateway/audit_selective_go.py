"""Explicit read-only delivery observation, never an in-memory hook attestation.

Use a Go TestExportBundle export from the reviewed checkout running this tool.
The caller must independently verify the OCI-to-Incus fingerprint mapping.
Matching labels/fingerprints is not provenance authenticity. No hook is imported,
no inference is requested, and no credentials or response bodies are inspected.
"""

import argparse
import base64
import hashlib
import json
import os
import re
import selectors
import stat
import subprocess
import time
from pathlib import Path
from urllib.parse import quote

SOURCES = {
    "config.yaml": "selective-go.yaml",
    "litellm_opencode_go_hook.py": "litellm_opencode_go_hook.py",
    "opencode_go_forwarding.py": "opencode_go_forwarding.py",
}
ENVIRONMENT = {
    "CONFIG_FILE_PATH": "/app/aginctus-go/config.yaml",
    "PYTHONPATH": "/app/aginctus-go",
}
IMAGE = re.compile(
    r"ghcr\.io/berriai/litellm(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?"
    r"@sha256:[0-9a-f]{64}"
)
NAME = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}")
HEX = re.compile(r"[0-9a-f]{64}")
LABELS = frozenset({
    "PASS", "FAIL_INPUT", "FAIL_BUNDLE", "FAIL_QUERY", "FAIL_OWNERSHIP",
    "FAIL_IMAGE", "FAIL_ENV", "FAIL_GUEST", "FAIL_FILES", "FAIL_VERSION",
    "FAIL_HEALTH",
})

# Fixed paths and loopback URLs only. Isolated Python avoids PYTHONPATH hooks;
# metadata inspection does not import LiteLLM or the delivered callback.
GUEST_SCRIPT = r'''
import hashlib
import importlib.metadata
import json
import os
import stat
import urllib.request

def observe():
    expected_env = {"CONFIG_FILE_PATH": "/app/aginctus-go/config.yaml",
                    "PYTHONPATH": "/app/aginctus-go"}
    if any(os.environ.get(k) != v for k, v in expected_env.items()):
        return {"result": "FAIL_ENV"}
    hashes = {}
    try:
        # Opening each component without following links also rejects a symlink
        # in /app or the delivery directory. Root ownership prevents replacement.
        parent = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
        try:
            for part in ("app", "aginctus-go"):
                child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                dir_fd=parent)
                os.close(parent)
                parent = child
                info = os.fstat(parent)
                if info.st_uid != 0 or info.st_mode & 0o022:
                    return {"result": "FAIL_FILES"}
            for name in ("config.yaml", "litellm_opencode_go_hook.py",
                         "opencode_go_forwarding.py"):
                fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                             dir_fd=parent)
                with os.fdopen(fd, "rb") as source:
                    info = os.fstat(source.fileno())
                    if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0
                            or info.st_mode & 0o022 or info.st_size > 131072):
                        return {"result": "FAIL_FILES"}
                    data = source.read(131073)
                    if len(data) > 131072:
                        return {"result": "FAIL_FILES"}
                    hashes[name] = hashlib.sha256(data).hexdigest()
        finally:
            os.close(parent)
    except Exception:
        return {"result": "FAIL_FILES"}
    try:
        if importlib.metadata.version("litellm") != "1.103.0":
            return {"result": "FAIL_VERSION"}
    except Exception:
        return {"result": "FAIL_VERSION"}
    try:
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                return None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        for path in ("liveliness", "readiness"):
            with opener.open("http://127.0.0.1:4000/health/" + path, timeout=3) as response:
                if response.status != 200:
                    return {"result": "FAIL_HEALTH"}
    except Exception:
        return {"result": "FAIL_HEALTH"}
    return {"result": "PASS", "hashes": hashes}

try:
    result = observe()
except Exception:
    result = {"result": "FAIL_GUEST"}
print(json.dumps(result, separators=(",", ":")))
'''


class AuditFailure(Exception):
    def __init__(self, label):
        self.label = label


def incus_image_alias(image):
    """Match the pinned importer's normalized, bounded local alias exactly."""
    normalized = "ghcr.io/berriai/litellm@" + image.split("@")[1]
    if len(normalized) <= 64:
        return normalized
    digest = hashlib.sha256(normalized.encode("utf-8")).digest()
    return "ic-image-" + base64.b32encode(digest).decode("ascii").rstrip("=").lower()


def read_public(path):
    """Bound reads and reject non-regular files without following final links."""
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise AuditFailure("FAIL_BUNDLE")
        data = source.read(131073)
    if len(data) > 131072:
        raise AuditFailure("FAIL_BUNDLE")
    return data


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError
        result[key] = value
    return result


def decode(data):
    return json.loads(data, object_pairs_hook=unique_object)


def bundle_hashes(directory):
    try:
        directory = Path(directory)
        if directory.is_symlink() or not directory.is_dir():
            raise ValueError
        if {p.name for p in directory.iterdir()} != {*SOURCES, "manifest.json"}:
            raise ValueError
        hashes = {}
        for name, source in SOURCES.items():
            data = read_public(directory / name)
            if data != read_public(Path(__file__).parent / source):
                raise ValueError
            hashes[name] = hashlib.sha256(data).hexdigest()
        if decode(read_public(directory / "manifest.json")) != hashes:
            raise ValueError
        return hashes
    except (OSError, ValueError, AuditFailure):
        raise AuditFailure("FAIL_BUNDLE") from None


def command_json(command, limit, failure):
    """Bound stdout in memory and time; discard stderr without reflecting it."""
    try:
        with subprocess.Popen(command, stdin=subprocess.DEVNULL,
                              stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                              shell=False) as process:
            try:
                deadline = time.monotonic() + 20
                output = bytearray()
                with selectors.DefaultSelector() as selector:
                    selector.register(process.stdout, selectors.EVENT_READ)
                    while True:
                        remaining = deadline - time.monotonic()
                        if remaining <= 0 or not selector.select(remaining):
                            raise ValueError
                        chunk = os.read(process.stdout.fileno(), min(4096, limit + 1 - len(output)))
                        output.extend(chunk)
                        if len(output) > limit:
                            raise ValueError
                        if not chunk:
                            break
                if process.wait(timeout=max(0.001, deadline - time.monotonic())) != 0:
                    raise ValueError
                return decode(output)
            finally:
                if process.poll() is None:
                    process.kill()
                process.wait()
    except (OSError, ValueError, subprocess.SubprocessError):
        raise AuditFailure(failure) from None


def audit(args):
    if (not NAME.fullmatch(args.project) or not NAME.fullmatch(args.instance)
            or not IMAGE.fullmatch(args.expected_image)
            or not HEX.fullmatch(args.expected_incus_fingerprint)):
        raise AuditFailure("FAIL_INPUT")
    hashes = bundle_hashes(args.bundle_dir)
    endpoint = "/1.0/instances/" + quote(args.instance, safe="") + "?project=" + quote(args.project, safe="")
    instance = command_json(["incus", "query", endpoint], 1048576, "FAIL_QUERY")
    if not isinstance(instance, dict):
        raise AuditFailure("FAIL_QUERY")
    config = instance.get("config")
    if not isinstance(config, dict):
        raise AuditFailure("FAIL_OWNERSHIP")
    required = {
        "user.aginctus.managed": "true",
        "user.aginctus.resource": "compose-instance",
        "user.aginctus.deployment": args.project,
        "user.label.incus-compose.service": "litellm",
    }
    if (instance.get("name") != args.instance or instance.get("status") != "Running"
            or any(config.get(k) != v for k, v in required.items())):
        raise AuditFailure("FAIL_OWNERSHIP")
    if (config.get("user.image_alias") != incus_image_alias(args.expected_image)
            or config.get("volatile.base_image") != args.expected_incus_fingerprint):
        raise AuditFailure("FAIL_IMAGE")
    expanded = instance.get("expanded_config")
    if (not isinstance(expanded, dict)
            or any(expanded.get("environment." + k) != v for k, v in ENVIRONMENT.items())):
        raise AuditFailure("FAIL_ENV")
    result = command_json([
        "incus", "exec", "--project", args.project, args.instance, "--",
        "/app/.venv/bin/python", "-I", "-B", "-c", GUEST_SCRIPT,
    ], 4096, "FAIL_GUEST")
    if not isinstance(result, dict):
        raise AuditFailure("FAIL_GUEST")
    label = result.get("result")
    if label in ("FAIL_ENV", "FAIL_FILES", "FAIL_VERSION", "FAIL_HEALTH", "FAIL_GUEST"):
        if set(result) != {"result"}:
            raise AuditFailure("FAIL_GUEST")
        raise AuditFailure(label)
    if label != "PASS" or set(result) != {"result", "hashes"}:
        raise AuditFailure("FAIL_GUEST")
    if result["hashes"] != hashes:
        raise AuditFailure("FAIL_FILES")
    return "PASS"


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise AuditFailure("FAIL_INPUT")


def main(argv=None):
    parser = Parser(description=__doc__)
    for flag in ("project", "instance", "bundle-dir", "expected-incus-fingerprint", "expected-image"):
        parser.add_argument("--" + flag, required=True)
    try:
        label = audit(parser.parse_args(argv))
    except AuditFailure as error:
        label = error.label
    except Exception:
        label = "FAIL_GUEST"
    print(json.dumps({"result": label if label in LABELS else "FAIL_GUEST"}))
    return 0 if label == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
