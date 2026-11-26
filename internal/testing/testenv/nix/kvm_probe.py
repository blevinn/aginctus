"""Fixed nested x86 KVM execution probe, packaged only in the testenv VM.

A boot sector exits via isa-debug-exit after executing guest instructions.
This is not a nested NixOS boot, Incus acceptance, or host isolation proof.
"""
import os
import subprocess
import sys
import tempfile
from pathlib import Path

# mov dx, 0xf4; mov al, 0x2a; out dx, al; cli; hlt; jmp back to hlt
BOOT_CODE = bytes.fromhex("baf400b02aee faf4ebfd")
EXPECTED_EXIT = (0x2A << 1) | 1


def boot_sector():
    return BOOT_CODE.ljust(510, b"\0") + b"\x55\xaa"


def probe(qemu):
    if not os.path.isabs(qemu):
        return False
    with tempfile.TemporaryDirectory(prefix="testenv-kvm-") as directory:
        disk = Path(directory) / "probe.img"
        # A conventional 1.44 MB floppy with an x86 boot signature.
        disk.write_bytes(boot_sector() + bytes(1440 * 1024 - 512))
        argv = [
            qemu, "-no-user-config", "-nodefaults",
            "-machine", "pc,accel=kvm", "-cpu", "host",
            "-m", "128", "-smp", "1",
            "-display", "none", "-serial", "none", "-monitor", "none",
            "-nic", "none", "-no-reboot",
            "-device", "isa-debug-exit,iobase=0xf4,iosize=0x04",
            "-drive", f"file={disk},format=raw,if=floppy,readonly=on",
            "-boot", "a",
        ]
        try:
            result = subprocess.run(
                argv, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL, timeout=15, check=False,
                env={"LC_ALL": "C"},
            )
        except (OSError, subprocess.TimeoutExpired):
            return False
        return result.returncode == EXPECTED_EXIT


def main():
    if len(sys.argv) != 2 or not probe(sys.argv[1]):
        print("nested KVM guest instruction execution not verified", file=sys.stderr)
        return 1
    print("nested_kvm_guest_execution_verified")
    return 0


if __name__ == "__main__":
    sys.exit(main())
