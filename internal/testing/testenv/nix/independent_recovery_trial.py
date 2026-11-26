"""Independent timer acceptance on a fresh, trusted disposable worker.

Called only by the pinned NixOS test driver after fixture provisioning.
No direct sweep or fake clock; use the installed recovery timer.
"""

import datetime
import json
import re
import shlex

RECOVERY_UNIT = "aginctus-testenv-recovery.service"
RECOVERY_TIMER = "aginctus-testenv-recovery.timer"
ACCEPTANCE_UNIT = "aginctus-testenv-acceptance.service"
PROJECT = "aginctus-testenv"
LIST = f"incus --project {PROJECT} list --format json"
VOLUMES = f"incus --project {PROJECT} storage volume list testenv --format json"


def _retained_instance(machine):
    query = (
        '[.[] | select(.name | startswith("testenv-vm-")) '
        '| select(.ephemeral == false and .status == "Running")] '
        '| select(length == 1) | .[0].name'
    )
    command = LIST + " | jq -er " + shlex.quote(query)
    machine.wait_until_succeeds(command, timeout=120)
    name = machine.succeed(command).strip()
    if not re.fullmatch(r"testenv-vm-[0-9a-f]{24}", name):
        raise RuntimeError("unexpected retained instance identity")
    expires = machine.succeed(
        f"incus --project {PROJECT} config get {name} user.aginctus.testenv.expires"
    ).strip()
    # Incus timestamps are RFC3339Nano; datetime truncates extra precision.
    expiry = datetime.datetime.fromisoformat(expires.replace("Z", "+00:00"))
    now = datetime.datetime.fromisoformat(
        machine.succeed("date --utc +%Y-%m-%dT%H:%M:%S+00:00").strip()
    )
    if (expiry - now).total_seconds() < 45:
        raise RuntimeError("retained VM too close to expiry for interruption")
    volume = 'any(.[]; .name == $name and .type == "virtual-machine")'
    machine.succeed(
        VOLUMES + " | jq -e --arg name " + shlex.quote(name) + " " + shlex.quote(volume)
    )
    return name


def _wait_for_recovery(machine, name):
    # Poll read-only daemon inventory and observe the installed unit's own
    # result. Never invoke the recovery service manually from this fixture.
    no_vm = 'all(.[]; .name != $name)'
    no_volume = 'all(.[]; .name != $name or .type != "virtual-machine")'
    machine.wait_until_succeeds(
        LIST + " | jq -e --arg name " + shlex.quote(name) + " " + shlex.quote(no_vm),
        timeout=360,
    )
    machine.wait_until_succeeds(
        VOLUMES + " | jq -e --arg name " + shlex.quote(name) + " " + shlex.quote(no_volume),
        timeout=60,
    )
    machine.wait_until_succeeds(
        f"journalctl -b -u {RECOVERY_UNIT} --no-pager -o cat "
        "| grep -E 'destroyed=1 failed=0 root_volumes_absent=1 root_volumes_unverified=0'",
        timeout=60,
    )
    machine.succeed(f"systemctl is-active --quiet {RECOVERY_TIMER}")


def _sentinel(machine):
    items = json.loads(machine.succeed(LIST))
    if len(items) != 1 or items[0]["name"] != "foreign-sentinel" or items[0]["status"] != "Stopped":
        raise RuntimeError("foreign sentinel or final worker inventory changed")
    if json.loads(machine.succeed("incus --project default list --format json")):
        raise RuntimeError("default project changed")
    machine.succeed(
        VOLUMES + " | jq -e 'any(.[]; .name == \"foreign-sentinel\" and .type == \"virtual-machine\")'"
    )
    return machine.succeed(
        f"incus --project {PROJECT} config show foreign-sentinel"
    )


def run_independent_recovery(machine, argv):
    baseline = _sentinel(machine)
    # Use a separate persistent report directory for each interrupted run.
    # No --wait/--pipe/--collect: the driver must observe the killed unit.
    async_argv = [arg for arg in argv if arg not in ("--wait", "--pipe", "--collect")]
    machine.succeed(f"systemctl start {RECOVERY_TIMER}")
    machine.succeed(shlex.join(async_argv))
    name = _retained_instance(machine)
    machine.succeed(f"systemctl kill --kill-whom=main --signal=SIGKILL {ACCEPTANCE_UNIT}")
    machine.wait_until_succeeds(
        f"test \"$(systemctl show -p Result --value {ACCEPTANCE_UNIT})\" = signal "
        f"&& test \"$(systemctl show -p ExecMainStatus --value {ACCEPTANCE_UNIT})\" = 9"
    )
    # Verify SIGKILL did not itself remove the retained resource.
    machine.succeed(f"incus --project {PROJECT} info {name}")
    _wait_for_recovery(machine, name)
    if _sentinel(machine) != baseline:
        raise RuntimeError("foreign sentinel configuration changed after crash")
    machine.succeed(f"systemctl reset-failed {ACCEPTANCE_UNIT}")

    # A different transient unit avoids adopting/restarting the failed one.
    reboot_argv = [
        arg.replace("aginctus-testenv-acceptance", "aginctus-testenv-reboot")
        if arg.startswith("--unit=") or arg.startswith("--property=StateDirectory=")
        or arg.startswith("/run/credentials/") or arg.startswith("/var/lib/aginctus-testenv-acceptance")
        else arg
        for arg in async_argv
    ]
    machine.succeed(shlex.join(reboot_argv))
    reboot_name = _retained_instance(machine)
    if reboot_name == name:
        raise RuntimeError("retained VM identity reused")
    # Kill the helper before reboot so its normal shutdown cannot perform
    # cleanup. Verify the resource exists at the reboot boundary.
    reboot_unit = "aginctus-testenv-reboot.service"
    machine.succeed(f"systemctl kill --kill-whom=main --signal=SIGKILL {reboot_unit}")
    machine.wait_until_succeeds(
        f"test \"$(systemctl show -p ExecMainStatus --value {reboot_unit})\" = 9"
    )
    machine.succeed(f"incus --project {PROJECT} info {reboot_name}")
    boot_id = machine.succeed("cat /proc/sys/kernel/random/boot_id").strip()
    machine.reboot()
    machine.wait_for_unit("incus.service")
    if machine.succeed("cat /proc/sys/kernel/random/boot_id").strip() == boot_id:
        raise RuntimeError("worker reboot not verified")
    _wait_for_recovery(machine, reboot_name)
    if _sentinel(machine) != baseline:
        raise RuntimeError("foreign sentinel configuration changed after reboot")
    machine.succeed(f"incus --project {PROJECT} delete foreign-sentinel")
    final_volumes = json.loads(machine.succeed(VOLUMES))
    if json.loads(machine.succeed(LIST)) or any(volume["type"] != "image" for volume in final_volumes):
        raise RuntimeError("worker instance or non-image volume inventory not empty")
    # This marker is a scoped fixture observation, not host teardown proof.
    machine.succeed(
        "printf '%s\\n' "
        + shlex.quote(json.dumps({
            "schema_version": 1,
            "profile": "independent_worker_recovery_fixture",
            "status": "pass",
            "sigkill_recovery": "pass",
            "reboot_recovery": "pass",
            "root_volume_absence": "pass",
            "foreign_preserved": "pass",
            "host_teardown": "not_verified",
            "physical_reclamation": "not_verified",
        }))
        + " > /tmp/testenv-recovery-receipt.json"
    )
    machine.copy_from_vm("/tmp/testenv-recovery-receipt.json")
