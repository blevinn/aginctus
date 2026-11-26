# Explicit, heavyweight NixOS test-driver fixture. All Incus and disk
# mutations occur in the test node, never on the calling host. This is a
# reviewed fixture, not a general worker launcher or agent admission service.
{
  self,
  nixpkgs,
  system,
  nativeInstance ? false,
  independentRecovery ? false,
}:
let
  pkgs = import nixpkgs { inherit system; };
  image =
    if nativeInstance then
      self.packages.${system}.testenv-native-instance-vm-image
    else
      self.packages.${system}.testenv-incus-vm-image;
  workerTimeout = if nativeInstance then "43min" else "13min";
  trialTimeout = if nativeInstance then 2700 else 900;
  worker = self.packages.${system}.testenv-cli;
  product = self.packages.${system}.aginctus;
in
assert !(nativeInstance && independentRecovery);
pkgs.testers.runNixOSTest {
  name =
    if independentRecovery then
      "aginctus-testenv-independent-recovery"
    else if nativeInstance then
      "aginctus-testenv-managed-native-instance"
    else
      "aginctus-testenv-disposable-worker";
  nodes.machine = { lib, ... }: {
    imports = lib.optional independentRecovery self.nixosModules.aginctusTestenvRecovery;
    services.aginctusTestenvRecovery.enable = lib.mkIf independentRecovery true;
    virtualisation = {
      cores = 4;
      memorySize = 8192;
      diskSize = 16384;
      emptyDiskImages = [ 73728 ];
      qemu.options = [ "-cpu host" ];
      incus.enable = true;
      incus.package = pkgs.incus;
    };
    environment.systemPackages = [
      pkgs.incus
      pkgs.lvm2
      pkgs.util-linux
      pkgs.jq
      worker
      product
    ];
    users.groups.aginctus-testenv = { };
    users.users.aginctus-testenv = {
      isSystemUser = true;
      group = "aginctus-testenv";
      extraGroups = [ "incus-admin" ];
    };
    networking.nftables.enable = true;
    system.stateVersion = "26.05";
  };
  testScript = (if independentRecovery then builtins.readFile ./independent_recovery_trial.py else "") + ''
    import json
    import re
    import shlex

    start_all()
    machine.wait_for_unit("incus.service")
    ${pkgs.lib.optionalString independentRecovery ''
      machine.succeed("systemctl stop aginctus-testenv-recovery.timer aginctus-testenv-recovery.service")
    ''}
    # Fail before provisioning if nested KVM or the dedicated test disk is
    # absent. The disk is declared above, not selected by a caller or
    # discovered from host storage.
    machine.succeed("test -c /dev/kvm")
    machine.succeed("test -b /dev/vdb")
    assert machine.succeed("blockdev --getsize64 /dev/vdb").strip() == str(72 * 1024**3)
    assert machine.succeed("lsblk -nr -o MOUNTPOINT /dev/vdb").strip() == ""
    machine.succeed("printf 'config: {}\\nnetworks: []\\nstorage_pools: []\\nprofiles: []\\n' | incus admin init --preseed")
    machine.succeed("incus storage create testenv lvm source=/dev/vdb")

    policy = json.loads(machine.succeed("aginctus testenv vm policy"))
    assert policy["name"] == "aginctus-testenv"
    argv = ["incus", "project", "create", policy["name"]]
    for key, value in sorted(policy["config"].items()):
        argv += ["-c", key + "=" + value]
    machine.succeed(shlex.join(argv))
    machine.succeed("incus --project aginctus-testenv image import ${image}/metadata.tar.xz ${image}/disk.qcow2", timeout=240)
    images = json.loads(machine.succeed("incus --project aginctus-testenv image list --format json"))
    assert len(images) == 1 and images[0]["type"] == "virtual-machine"
    fingerprint = images[0]["fingerprint"]
    assert re.fullmatch(r"[0-9a-f]{64}", fingerprint)

    # Empty, stopped, unsigned sentinel in the second instance slot. No
    # shared data, user credentials or host mounts.
    machine.succeed("incus --project aginctus-testenv init --empty foreign-sentinel --vm --no-profiles --storage testenv -c limits.cpu=1 -c limits.memory=512MiB -d root,size=8GiB")
    machine.succeed("install -d -m 0755 /etc/aginctus-testenv")
    machine.succeed("printf 'aginctus-dedicated-disposable-worker-v1\\n' > /etc/aginctus-testenv/disposable-worker; chmod 0444 /etc/aginctus-testenv/disposable-worker")
    machine.succeed("install -d -m 0700 /var/lib/aginctus-testenv; umask 077; head -c 32 /dev/urandom > /var/lib/aginctus-testenv/worker-policy.key")
    unit = "aginctus-testenv-acceptance"
    argv = [
        "systemd-run", "--unit=" + unit, "--wait", "--pipe", "--collect",
        "--service-type=exec", "--uid=aginctus-testenv", "--gid=aginctus-testenv",
        "--property=SupplementaryGroups=incus-admin",
        "--property=LoadCredential=policy-key:/var/lib/aginctus-testenv/worker-policy.key",
        "--property=StateDirectory=aginctus-testenv-acceptance",
        "--property=StateDirectoryMode=0700",
        "--property=UMask=0077", "--property=NoNewPrivileges=yes",
        "--property=ProtectSystem=strict", "--property=ProtectHome=yes",
        "--property=PrivateTmp=yes", "--property=PrivateDevices=yes",
        "--property=RestrictAddressFamilies=AF_UNIX", "--property=IPAddressDeny=any",
        "--property=TimeoutStartSec=${workerTimeout}", "--property=TimeoutStopSec=150s",
        "${worker}/bin/aginctus-testenv", "worker", "acceptance",
        "--disposable-worker", "DEDICATED_DISPOSABLE_WORKER",
        "--socket", "/var/lib/incus/unix.socket", "--project", "aginctus-testenv",
        "--image", fingerprint,
        "--policy-key-file", "/run/credentials/" + unit + ".service/policy-key",
        "--report-root", "/var/lib/aginctus-testenv-acceptance",
        "--foreign-instance", "foreign-sentinel",
    ]
    # The worker itself is nested under the NixOS test driver. Requiring
    # another guest inside the Incus VM would add a third VM layer; keep
    # that distinct from this two-layer lifecycle fixture.
    ${pkgs.lib.optionalString nativeInstance ''
      argv += ["--scenario", "native-instance"]
    ''}
  '' + (if independentRecovery then ''
    run_independent_recovery(machine, argv)
  '' else ''
    machine.succeed(shlex.join(argv), timeout=${toString trialTimeout})
    reports = machine.succeed("find /var/lib/aginctus-testenv-acceptance -mindepth 2 -maxdepth 2 -name result.json").splitlines()
    assert len(reports) == 1
    report = json.loads(machine.succeed("cat " + shlex.quote(reports[0])))
    assert report["status"] == "pass"
    checks = {check["id"]: check["status"] for check in report["results"]}
    for key in ("ephemeral_lifecycle", "retained_lifecycle", "instance_cleanup", "root_volumes", "foreign_preserved", "scoped_ttl_recovery"):
        assert checks[key] == "pass", key
    ${pkgs.lib.optionalString nativeInstance ''
      assert report["scenario"] == "native-instance"
      assert checks["native_instance_scenario"] == "pass"
    ''}
    assert checks["nested_kvm_guest_execution"] == "not_verified"
    assert checks["nested_isolation"] == "not_verified"
    remaining = json.loads(machine.succeed("incus --project aginctus-testenv list --format json"))
    assert len(remaining) == 1 and remaining[0]["name"] == "foreign-sentinel" and remaining[0]["status"] == "Stopped"
    assert json.loads(machine.succeed("incus --project default list --format json")) == []
    volumes = json.loads(machine.succeed("incus --project aginctus-testenv storage volume list testenv --format json"))
    assert not any(volume["name"].startswith("testenv-vm-") for volume in volumes)
    machine.succeed("incus --project aginctus-testenv delete foreign-sentinel")
  '');
}
