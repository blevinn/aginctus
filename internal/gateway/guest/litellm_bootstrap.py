"""Unwired native LiteLLM guest bootstrap for the reviewed v1.103.0 image.

Run with the pinned image's /app/.venv/bin/python -I -S. The sole argument
is the PostgreSQL guest name derived by the trusted launcher from its pin.
This program does not authenticate that pin or deliver any material.
"""

import os
import re
import resource
import stat
import sys
from contextlib import ExitStack

PYTHON = "/app/.venv/bin/python"
LITELLM = "/app/.venv/bin/litellm"
DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
TOKEN = re.compile(rb"[A-Za-z0-9_-]{1,255}\n?")


def read_token(directory, name, uid):
    flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
    with os.fdopen(os.open(name, flags, dir_fd=directory), "rb") as file:
        info = os.fstat(file.fileno())
        if (
            not stat.S_ISREG(info.st_mode)
            or info.st_uid != uid
            or info.st_nlink != 1
            or stat.S_IMODE(info.st_mode) not in (0o400, 0o600)
        ):
            raise ValueError
        data = file.read(257)
    if not TOKEN.fullmatch(data):
        raise ValueError
    return data.removesuffix(b"\n").decode("ascii")


def load_credentials():
    uid = os.geteuid()
    with ExitStack() as stack:
        directory = None
        for component in ("/", "run", "aginctus", "gateway"):
            directory = os.open(component, DIRECTORY_FLAGS, dir_fd=directory)
            stack.callback(os.close, directory)
            info = os.fstat(directory)
            if component == "gateway":
                if info.st_uid != uid or stat.S_IMODE(info.st_mode) != 0o700:
                    raise ValueError
            elif info.st_uid != 0 or info.st_mode & 0o022:
                raise ValueError
        return tuple(read_token(directory, name, uid) for name in ("postgres-password", "master-key", "salt-key"))


def launch(argv):
    if len(argv) != 1 or not re.fullmatch(r"ag-[0-9a-f]{32}-pg", argv[0]):
        raise ValueError

    # Limit conventional core files; the guest/host crash collector and procfs
    # policy still require independent review. Never launch the tracing wrapper.
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    os.umask(0o077)
    os.chdir("/app")
    password, master, salt = load_credentials()
    # Tokens are strictly URL-unreserved ASCII; the only dynamic URL field
    # other than the password is the canonical, independently bound guest name.
    env = {
        "PATH": "/app/.venv/bin:/usr/local/bin:/usr/bin:/bin",
        "PRISMA_BINARY_CACHE_DIR": "/opt/prisma/binaries",
        "PRISMA_CLI_PATH": "/opt/prisma/binaries/node_modules/.bin/prisma",
        "PRISMA_CLI_QUERY_ENGINE_TYPE": "binary",
        "PRISMA_OFFLINE_MODE": "true",
        "LITELLM_MODE": "PRODUCTION",
        "LITELLM_LOG": "ERROR",
        "PYTHON_DOTENV_DISABLED": "1",
        "DATABASE_URL": f"postgresql://litellm:{password}@{argv[0]}:5432/litellm",
        "LITELLM_MASTER_KEY": master,
        "LITELLM_SALT_KEY": salt,
    }
    # Upstream errors may contain the URL. Do not forward process output to
    # Incus logs (even at ERROR); use independently redacted readiness instead.
    null = os.open("/dev/null", os.O_RDWR | os.O_NOFOLLOW)
    try:
        for fd in (0, 1, 2):
            os.dup2(null, fd)
    finally:
        if null > 2:
            os.close(null)
    os.execve(PYTHON, [PYTHON, "-I", LITELLM, "--port", "4000", "--num_workers", "1", "--telemetry", "False"], env)


def main():
    try:
        launch(sys.argv[1:])
    except (OSError, ValueError):
        # Fixed status only; an input or exec exception can include secrets.
        return 1
    return 1  # A successful exec never returns.


if __name__ == "__main__":
    sys.exit(main())
