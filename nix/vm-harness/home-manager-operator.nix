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
  operatorGeneration = homeManager.homeManagerConfiguration {
    inherit pkgs;
    modules = [
      self.homeManagerModules.default
      {
        home.username = "operator";
        home.homeDirectory = "/home/operator";
        home.stateVersion = "26.05";
        programs.aginctus = {
          enable = true;
          settings.gateway.id = "disposable-operator";
        };
      }
    ];
  };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-home-manager-operator";

  nodes.machine = {
    system.stateVersion = "26.05";
    # NixOS Incus requires nftables, even when this VM does not expose Aginctus host firewall services.
    networking.nftables.enable = true;
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
    system.extraDependencies = [ operatorGeneration.activationPackage ];
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
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus list --format json | jq -e 'length == 0'")
    machine.succeed("incus image list --format json | jq -e 'length == 0'")
    machine.succeed("test ! -e /etc/aginctus/config.json")
    machine.fail("systemctl list-unit-files --no-legend 'aginctus*' | grep -q '^aginctus'")

    # The generation was evaluated and built by the outer test derivation.
    # Exercise its real Home Manager activation as the unprivileged operator.
    # Neither the NixOS activation nor root invokes this script.
    activation = "runuser -u operator -- env HOME=/home/operator USER=operator LOGNAME=operator ${operatorGeneration.activationPackage}/activate"
    machine.succeed("test -x ${operatorGeneration.activationPackage}/activate")
    machine.succeed(activation, timeout=600)
    machine.succeed("runuser -u operator -- test -L /home/operator/.config/aginctus/config.json")
    machine.succeed("runuser -u operator -- jq -e '.gateway.id == \"disposable-operator\"' /home/operator/.config/aginctus/config.json")
    machine.succeed("runuser -u operator -- /home/operator/.nix-profile/bin/aginctus config get gateway.id | grep -q disposable-operator")

    # Repeated activation remains an operator-only package/config change.
    machine.succeed(activation, timeout=600)
    machine.succeed("incus list --format json | jq -e 'length == 0'")
    machine.succeed("incus image list --format json | jq -e 'length == 0'")
    machine.succeed("test ! -e /etc/aginctus/config.json")
    machine.fail("systemctl list-unit-files --no-legend 'aginctus*' | grep -q '^aginctus'")
    machine.succeed("test ! -e /home/operator/.config/systemd/user/aginctus-reconcile.service")
    machine.succeed("test ! -e /home/operator/.config/systemd/user/aginctus-image-update.service")
  '';
}
