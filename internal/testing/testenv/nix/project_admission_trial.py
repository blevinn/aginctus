"""Actual provider admission probes inside the fixed disposable worker.

Only synthetic stopped instances and a local unnumbered bridge are used.
These are aggregate admission and device policy tests, not packet, runtime
resource, host isolation, or agent authorization evidence.
"""

import json as admission_json
import shlex as admission_shlex


ADMISSION_PROJECT = "aginctus-testenv"
ADMISSION_PREFIX = "incus --project " + ADMISSION_PROJECT
ADMISSION_PROBE = "policy-probe"
ADMISSION_SENTINEL = "foreign-sentinel"


def _admission_inventory(machine):
    instances = admission_json.loads(machine.succeed(ADMISSION_PREFIX + " list --format json"))
    volumes = admission_json.loads(
        machine.succeed(ADMISSION_PREFIX + " storage volume list testenv --format json")
    )
    states = sorted((item["name"], item["status"]) for item in instances)
    disks = sorted((item["type"], item["name"]) for item in volumes)
    configs = [
        (name, machine.succeed(ADMISSION_PREFIX + " config show " + admission_shlex.quote(name)))
        for name, _ in states
    ]
    return states, disks, configs


def _admission_init(name, cpu=1, memory="512MiB", disk="8GiB"):
    return ADMISSION_PREFIX + " " + admission_shlex.join([
        "init", "--empty", name, "--vm", "--no-profiles", "--storage", "testenv",
        "-c", f"limits.cpu={cpu}", "-c", f"limits.memory={memory}", "-d", f"root,size={disk}",
    ])


def _admission_denied(machine, label, command, expected):
    before = _admission_inventory(machine)
    status, output = machine.execute(command + " 2>&1")
    if status != 1 or expected not in output:
        raise RuntimeError(f"admission probe {label}: expected Incus policy denial; exit {status}")
    if _admission_inventory(machine) != before:
        raise RuntimeError(f"admission probe {label}: rejected request changed inventory or configuration")
    print("FIXTURE PROJECT ADMISSION:", label, "pass")


def run_project_admission(machine):
    baseline = _admission_inventory(machine)
    if baseline[0] != [(ADMISSION_SENTINEL, "Stopped")]:
        raise RuntimeError("project admission requires only the stopped foreign sentinel")

    # One existing 1 CPU/512 MiB/8 GiB VM plus each candidate exceeds
    # exactly one aggregate budget; a free instance slot remains.
    for label, command, limit, value in (
        ("cpu", _admission_init(ADMISSION_PROBE, cpu=4), "limits.cpu", "4"),
        ("memory", _admission_init(ADMISSION_PROBE, memory="10GiB"), "limits.memory", "10GiB"),
        ("disk", _admission_init(ADMISSION_PROBE, disk="64GiB"), "limits.disk", "64GiB"),
    ):
        # The pinned Incus emits a specific aggregate limit error. A CLI
        # usage error, storage failure, or missing device is not a pass.
        expected = f'Reached maximum aggregate value "{value}" for "{limit}" in project "{ADMISSION_PROJECT}"'
        _admission_denied(machine, label, command, expected)
    machine.succeed(_admission_init(ADMISSION_PROBE))
    if _admission_inventory(machine)[0] != [
        (ADMISSION_SENTINEL, "Stopped"), (ADMISSION_PROBE, "Stopped")
    ]:
        raise RuntimeError("valid second instance admission unverified")
    _admission_denied(
        machine, "instance_count", _admission_init("policy-third"),
        f'Reached maximum number of instances in project "{ADMISSION_PROJECT}"',
    )

    # The parent bridge and directory are real synthetic worker resources.
    # The request itself is valid except for the restricted project policy.
    machine.succeed("incus --project default network create te-probe0 ipv4.address=none ipv6.address=none")
    machine.succeed("install -d -m 0755 /run/testenv-policy-probe")
    prefix = ADMISSION_PREFIX + " config device add " + ADMISSION_PROBE
    _admission_denied(
        machine, "nic", prefix + " denied-nic nic network=te-probe0",
        "Network devices are forbidden",
    )
    _admission_denied(
        machine, "host_directory", prefix + " denied-disk disk source=/run/testenv-policy-probe path=/probe readonly=true",
        "Disk devices are forbidden",
    )
    _admission_denied(
        machine, "raw_qemu", ADMISSION_PREFIX + " config set " + ADMISSION_PROBE + " raw.qemu=-nodefaults",
        'Use of low-level config "raw.qemu"',
    )
    machine.succeed(ADMISSION_PREFIX + " delete " + ADMISSION_PROBE)
    machine.succeed("incus --project default network delete te-probe0")
    machine.succeed("rmdir /run/testenv-policy-probe")
    if _admission_inventory(machine) != baseline:
        raise RuntimeError("project admission cleanup did not restore baseline")
    if admission_json.loads(machine.succeed("incus --project default list --format json")):
        raise RuntimeError("default project instance inventory changed")
    return [
        {"id": "project_device_denials", "status": "pass"},
        {"id": "project_aggregate_limits", "status": "pass"},
    ]
