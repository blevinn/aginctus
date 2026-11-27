"""Observe actual guest resource presentation in a fixed disposable worker.

The trusted test driver may manage its own synthetic probe. This is not a
new signed worker API, nor evidence of host-level throttling or hard quotas.
"""

import re as resource_re
import shlex as resource_shlex

RESOURCE_PROBE = "resource-probe"
RESOURCE_CPU = 2
RESOURCE_MEMORY_KIB = 4096 * 1024
RESOURCE_DISK_BYTES = 32 * 1024**3


def validate_resource_observations(cpu, memory, disk, last_sector, beyond_disk):
    observations = (cpu, memory, disk, last_sector, beyond_disk)
    if any(not resource_re.fullmatch(r"[0-9]{1,12}", item.strip()) for item in observations):
        raise RuntimeError("resource probe returned malformed numeric observations")
    cpu, memory, disk, last_sector, beyond_disk = (int(item.strip()) for item in observations)
    if cpu != RESOURCE_CPU:
        raise RuntimeError(f"guest CPU count mismatch: {cpu}")
    # MemTotal excludes kernel/firmware reserved memory. Allow that overhead,
    # but reject a guest with a materially smaller or larger allocation.
    if not 3 * 1024**2 <= memory <= RESOURCE_MEMORY_KIB:
        raise RuntimeError(f"guest memory capacity mismatch (KiB): {memory}")
    if disk != RESOURCE_DISK_BYTES or last_sector != 512 or beyond_disk != 0:
        raise RuntimeError(
            f"guest root disk boundary mismatch: size={disk} last={last_sector} beyond={beyond_disk}"
        )
    return [
        {"id": "guest_cpu_count", "status": "pass"},
        {"id": "guest_memory_capacity", "status": "pass"},
        {"id": "guest_root_disk_capacity", "status": "pass"},
    ]


def run_runtime_resources(machine, fingerprint, inventory, project_prefix, sentinel):
    if not resource_re.fullmatch(r"[0-9a-f]{64}", fingerprint):
        raise RuntimeError("resource probe requires the pinned image fingerprint")
    baseline = inventory(machine)
    if baseline[0] != [(sentinel, "Stopped")]:
        raise RuntimeError("resource probe requires only the stopped foreign sentinel")
    prefix = project_prefix + " "
    machine.succeed(prefix + resource_shlex.join([
        "init", fingerprint, RESOURCE_PROBE, "--vm", "--no-profiles", "--storage", "testenv",
        "-c", "limits.cpu=2", "-c", "limits.memory=4096MiB", "-c", "security.nesting=true",
        "-d", "root,size=32GiB",
    ]), timeout=240)
    machine.succeed(prefix + "start " + RESOURCE_PROBE, timeout=120)
    execute = prefix + "exec --disable-stdin " + RESOURCE_PROBE + " -- "
    machine.wait_until_succeeds("timeout 15 " + execute + "/run/current-system/sw/bin/true", timeout=120)
    cpu = machine.succeed("timeout 15 " + execute + "/run/current-system/sw/bin/nproc --all")
    memory = machine.succeed(
        "timeout 15 " + execute + resource_shlex.join([
            "/run/current-system/sw/bin/awk", "/^MemTotal:/ {print $2}", "/proc/meminfo",
        ])
    )
    # Resolve the real mounted root's parent block device; never guess /dev/sda.
    # No writes: verify capacity and read exactly one sector at and beyond EOF.
    disk_script = """set -euo pipefail
export PATH=/run/current-system/sw/bin
root=$(findmnt --noheadings --nofsroot --output SOURCE --target /)
root=$(readlink -f "$root")
test -b "$root"
parent=$(lsblk --nodeps --noheadings --output PKNAME "$root")
if [ -n "$parent" ]; then
    root="/dev/$parent"
fi
test "$(lsblk --nodeps --noheadings --output TYPE "$root")" = disk
size=$(blockdev --getsize64 "$root")
printf '%s\n' "$size"
test "$size" = 34359738368
dd if="$root" bs=512 skip=67108863 count=1 status=none | wc -c
dd if="$root" bs=512 skip=67108864 count=1 status=none | wc -c
"""
    disk_observations = machine.succeed(
        "timeout 15 " + execute + resource_shlex.join([
            "/run/current-system/sw/bin/bash", "-c", disk_script,
        ])
    ).splitlines()
    if len(disk_observations) != 3:
        raise RuntimeError("resource probe returned malformed disk observations")
    checks = validate_resource_observations(cpu, memory, *disk_observations)
    print("FIXTURE RUNTIME RESOURCES:", cpu.strip(), memory.strip(), *disk_observations)
    machine.succeed(prefix + "stop " + RESOURCE_PROBE + " --force", timeout=120)
    machine.succeed(prefix + "delete " + RESOURCE_PROBE, timeout=120)
    if inventory(machine) != baseline:
        raise RuntimeError("resource probe cleanup did not restore baseline")
    return checks
