import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

BOOTSTRAP = Path(__file__).resolve().parents[2] / "internal/gateway/guest/litellm_bootstrap.py"
POSTGRES = "ag-123e4567e89b42d3a456426614174010-pg"
TOKENS = {"postgres-password": b"synthetic-db", "master-key": b"synthetic-master", "salt-key": b"synthetic-salt"}

# Relocate only the guest filesystem anchor and stand in for the exec target.
# All file opens, metadata checks, read bounds and stdio redirection are real.
HARNESS = """
import os, resource, runpy, sys
from types import SimpleNamespace

script, root, scenario, *args = sys.argv[1:]
os.chdir(root)
root_fd = os.open(root, os.O_DIRECTORY)
original_open, original_fstat, original_chdir = os.open, os.fstat, os.chdir
original_out = os.dup(1)
parents = {original_fstat(root_fd).st_ino}
for path in ('run', 'run/aginctus'):
    parents.add(os.stat(path, dir_fd=root_fd, follow_symlinks=False).st_ino)

def guest_open(path, flags, *, dir_fd=None):
    return original_open(root if path == '/' else path, flags, dir_fd=dir_fd)

def guest_fstat(fd):
    info = original_fstat(fd)
    if info.st_ino in parents:
        # Test without root; model root ownership of protected ancestors.
        return SimpleNamespace(st_uid=0, st_mode=info.st_mode)
    if scenario == 'owner' and os.path.basename(os.readlink(f'/proc/self/fd/{fd}')) == 'salt-key':
        return SimpleNamespace(st_uid=os.geteuid()+1, st_mode=info.st_mode, st_nlink=1)
    return info

def guest_chdir(path):
    assert path == '/app'
    original_chdir(root)

def guest_exec(executable, argv, env):
    if scenario == 'exec-failed':
        raise OSError('synthetic-secret-in-exception')
    if scenario != 'valid':
        os._exit(98)
    assert executable == '/app/.venv/bin/python'
    assert argv == [executable, '-I', '/app/.venv/bin/litellm', '--port', '4000',
                    '--num_workers', '1', '--telemetry', 'False']
    assert env == {
        'PATH': '/app/.venv/bin:/usr/local/bin:/usr/bin:/bin',
        'PRISMA_BINARY_CACHE_DIR': '/opt/prisma/binaries',
        'PRISMA_CLI_PATH': '/opt/prisma/binaries/node_modules/.bin/prisma',
        'PRISMA_CLI_QUERY_ENGINE_TYPE': 'binary', 'PRISMA_OFFLINE_MODE': 'true',
        'LITELLM_MODE': 'PRODUCTION', 'LITELLM_LOG': 'ERROR', 'PYTHON_DOTENV_DISABLED': '1',
        'DATABASE_URL': 'postgresql://litellm:synthetic-db@' + args[0] + ':5432/litellm',
        'LITELLM_MASTER_KEY': 'synthetic-master', 'LITELLM_SALT_KEY': 'synthetic-salt',
    }
    assert resource.getrlimit(resource.RLIMIT_CORE) == (0, 0)
    assert os.read(0, 1) == b''
    os.write(1, b'synthetic-upstream-output')
    os.write(2, b'synthetic-upstream-error')
    os.write(original_out, b'checked')
    os._exit(0)

os.open, os.fstat, os.chdir, os.execve = guest_open, guest_fstat, guest_chdir, guest_exec
sys.argv = [script, *args]
runpy.run_path(script, run_name='__main__')
"""


class NativeLiteLLMBootstrapTests(unittest.TestCase):
    def exercise(self, scenario, *args):
        with tempfile.TemporaryDirectory() as root:
            directory = Path(root) / "run/aginctus/gateway"
            directory.mkdir(parents=True, mode=0o700)
            directory.chmod(0o700)
            for name, value in TOKENS.items():
                path = directory / name
                path.write_bytes(value + (b"\n" if name == "master-key" else b""))
                path.chmod(0o600 if name == "master-key" else 0o400)
            salt = directory / "salt-key"
            if scenario in {"missing", "symlink", "hardlink", "fifo", "directory"}:
                salt.unlink()
                if scenario == "symlink":
                    salt.symlink_to(directory / "master-key")
                elif scenario == "hardlink":
                    salt.hardlink_to(directory / "master-key")
                elif scenario == "fifo":
                    os.mkfifo(salt, 0o600)
                elif scenario == "directory":
                    salt.mkdir(mode=0o700)
            elif scenario in {"empty", "oversized", "multiline", "delimiter", "nul"}:
                salt.unlink()
                salt.write_bytes({
                    "empty": b"", "oversized": b"a" * 256, "multiline": b"a\nb\n",
                    "delimiter": b"a@b:c", "nul": b"a\0b",
                }[scenario])
                salt.chmod(0o400)
            elif scenario == "readable":
                salt.chmod(0o644)
            elif scenario == "directory-mode":
                directory.chmod(0o750)
            elif scenario == "ancestor-mode":
                directory.parent.chmod(0o775)
            elif scenario == "ancestor-link":
                moved = directory.parent.with_name("moved")
                directory.parent.rename(moved)
                directory.parent.symlink_to(moved, target_is_directory=True)

            result = subprocess.run(
                [sys.executable, "-I", "-S", "-c", HARNESS, str(BOOTSTRAP), root, scenario, *args],
                input=b"synthetic-inherited-stdin", capture_output=True, timeout=5, check=False,
                env={"DATABASE_URL": "synthetic-ambient", "DEBUG": "true", "USE_DDTRACE": "true",
                     "CONFIG_FILE_PATH": "/synthetic-config", "LITELLM_MASTER_KEY": "synthetic-ambient"},
            )
        self.assertFalse(result.stderr, "bootstrap emitted diagnostics")
        if scenario == "valid":
            self.assertEqual(result.returncode, 0)
            self.assertTrue(result.stdout == b"checked", "unexpected bootstrap output")
        else:
            self.assertEqual(result.returncode, 1)
            self.assertFalse(result.stdout, "bootstrap emitted output on denial")

    def test_fixed_exec_consumes_only_protected_files(self):
        self.exercise("valid", POSTGRES)

    def test_denies_bad_files_or_ancestors_and_redacts_exec_failure(self):
        for scenario in (
            "missing", "symlink", "hardlink", "fifo", "directory", "owner", "readable", "directory-mode",
            "ancestor-mode", "ancestor-link", "empty", "oversized", "multiline", "delimiter", "nul", "exec-failed",
        ):
            with self.subTest(scenario=scenario):
                self.exercise(scenario, POSTGRES)

    def test_rejects_override_arguments(self):
        for args in ((), (POSTGRES, "--debug"), ("postgres.example",), (POSTGRES + ":9999",)):
            with self.subTest(args=args):
                self.exercise("args", *args)
