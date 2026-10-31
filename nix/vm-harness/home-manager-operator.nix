{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };

  # Evaluate a real standalone Home Manager configuration, without linking
  # Home Manager into NixOS modules or granting the user Incus access.
  # Build the activation closure OUTSIDE the isolated VM: its in-guest Nix
  # daemon has no network and must not fetch a toolchain during activation.
  homeManager = import "${pkgs.home-manager.src}/lib" { lib = pkgs.lib; };
  makeGeneration =
    gatewayID:
    homeManager.homeManagerConfiguration {
      inherit pkgs;
      modules = [
        self.homeManagerModules.default
        {
          home.username = "operator";
          home.homeDirectory = "/home/operator";
          home.stateVersion = "26.05";
          programs.aginctus = {
            enable = true;
            settings.gateway.id = gatewayID;
          };
        }
      ];
    };
  operatorGeneration = makeGeneration "disposable-operator";
  upgradedGeneration = makeGeneration "upgraded-operator";
  mismatchedManifest = pkgs.writeText "mismatched-operator-package.json" (
    builtins.toJSON {
      schema = 1;
      authority = "user";
      package = "/nix/store/00000000000000000000000000000000-wrong-cli";
    }
  );
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-home-manager-operator";

  nodes.machine = {
    system.stateVersion = "26.05";
    # NixOS Incus requires nftables, even when this VM does not expose Aginctus host firewall services.
    networking.nftables.enable = true;
    boot.supportedFilesystems = [ "btrfs" ];
    virtualisation = {
      cores = 2;
      memorySize = 4096;
      diskSize = 12288;
      incus.enable = true;
    };
    users.users.operator = {
      isNormalUser = true;
      home = "/home/operator";
      createHome = true;
    };
    nix.settings.allowed-users = [
      "root"
      "operator"
    ];
    # Embed the full prebuilt generation and its closure in the guest store.
    # This does not activate Home Manager or touch the user's home as root.
    system.extraDependencies = [
      operatorGeneration.activationPackage
      upgradedGeneration.activationPackage
    ];
    environment.systemPackages = [
      pkgs.incus
      pkgs.jq
    ];
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    # The VM's Incus daemon is initialized solely to make zero-mutation
    # assertions meaningful. The user has no Incus permission or group grant.
    machine.succeed("incus admin init --auto --storage-backend btrfs --storage-create-loop 2")
    machine.succeed("incus storage list --format json | jq -e 'any(.[]; .name == \"default\" and .driver == \"btrfs\")'")
    machine.succeed("incus list --format json | jq -e 'length == 0'")
    machine.succeed("incus image list --format json | jq -e 'length == 0'")
    machine.succeed("test ! -e /etc/aginctus/config.json")
    machine.fail("systemctl list-unit-files --no-legend 'aginctus*' | grep -q '^aginctus'")

    # Compare complete nonsecret resource inventory, not just instance counts.
    inventories = ["list", "image list", "project list", "network list", "storage list", "profile list"]
    before = {command: machine.succeed(f"incus {command} --format json | jq -S .") for command in inventories}

    # Synthetic protected host state is not a source for operator credentials.
    machine.succeed("install -d -m 0700 /var/lib/aginctus/state")
    machine.succeed("printf synthetic-protected-state > /var/lib/aginctus/state/acceptance-sentinel; chmod 0600 /var/lib/aginctus/state/acceptance-sentinel")
    protected_before = machine.succeed("sha256sum /var/lib/aginctus/state/acceptance-sentinel; stat -c '%u:%g:%a' /var/lib/aginctus/state /var/lib/aginctus/state/acceptance-sentinel")

    # The generation was evaluated and built by the outer test derivation.
    # Exercise its real Home Manager activation as the unprivileged operator.
    # Neither the NixOS activation nor root invokes this script.
    activation = "runuser -u operator -- env HOME=/home/operator USER=operator LOGNAME=operator ${operatorGeneration.activationPackage}/activate"
    machine.succeed("test -x ${operatorGeneration.activationPackage}/activate")
    machine.succeed(activation, timeout=600)
    machine.succeed("runuser -u operator -- test -L /home/operator/.config/aginctus/config.json")
    machine.succeed("runuser -u operator -- jq -e '.gateway.id == \"disposable-operator\"' /home/operator/.config/aginctus/config.json")
    machine.succeed("runuser -u operator -- /home/operator/.nix-profile/bin/aginctus config get gateway.id | grep -q disposable-operator")
    operator = "runuser -u operator -- env HOME=/home/operator USER=operator LOGNAME=operator"
    cli = operator + " /home/operator/.nix-profile/bin/aginctus"
    machine.succeed(cli + " operator package-audit --owner user | grep -q 'observed running CLI path'")
    status, _ = machine.execute(cli + " operator preflight --project aginctus", timeout=30)
    assert status == 1, "operator preflight must fail for denied Incus access, not usage"
    machine.fail(operator + " test -r /var/lib/aginctus/state/acceptance-sentinel")

    # Repeated activation remains an operator-only package/config change.
    machine.succeed(activation, timeout=600)
    machine.succeed(cli + " operator package-audit --owner user")

    # A changed declaration must not make a mismatched running CLI acceptable.
    manifest = "/home/operator/.config/aginctus/operator-package.json"
    machine.succeed(operator + " rm -f " + manifest)
    machine.succeed(operator + " cp ${mismatchedManifest} " + manifest)
    status, mismatch = machine.execute(cli + " operator package-audit --owner user 2>&1")
    assert status == 1 and "differs from the selected package" in mismatch, "runtime/declaration mismatch must fail safely"
    machine.succeed(operator + " rm -f " + manifest)

    # Package/config generation switching does not imply resource reconciliation.
    upgrade = operator + " ${upgradedGeneration.activationPackage}/activate"
    machine.succeed(upgrade, timeout=600)
    machine.succeed(cli + " config get gateway.id | grep -q upgraded-operator")
    machine.succeed(cli + " operator package-audit --owner user")

    # Starting a lingering user manager does not start Aginctus units or timers.
    machine.succeed("loginctl enable-linger operator")
    machine.wait_for_unit("user@1000.service")
    user_units = machine.succeed(operator + " env XDG_RUNTIME_DIR=/run/user/1000 systemctl --user list-unit-files --no-pager --no-legend")
    assert not any(line.startswith("aginctus") for line in user_units.splitlines()), "Aginctus user units or timers were installed"
    machine.succeed("loginctl disable-linger operator")
    machine.succeed("systemctl stop user@1000.service")

    for command in inventories:
        assert machine.succeed(f"incus {command} --format json | jq -S .") == before[command], f"HM activation or user-manager lifecycle changed Incus {command}"
    assert machine.succeed("sha256sum /var/lib/aginctus/state/acceptance-sentinel; stat -c '%u:%g:%a' /var/lib/aginctus/state /var/lib/aginctus/state/acceptance-sentinel") == protected_before
    machine.succeed("incus list --format json | jq -e 'length == 0'")
    machine.succeed("incus image list --format json | jq -e 'length == 0'")
    machine.succeed("test ! -e /etc/aginctus/config.json")
    machine.fail("systemctl list-unit-files --no-legend 'aginctus*' | grep -q '^aginctus'")
    machine.succeed("test ! -e /home/operator/.config/systemd/user/aginctus-reconcile.service")
    machine.succeed("test ! -e /home/operator/.config/systemd/user/aginctus-image-update.service")
  '';
}
