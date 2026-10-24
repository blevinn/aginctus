{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };

  # Standalone Home Manager consumes the exact exported Aginctus module and
  # CLI package. Neither Home Manager nor Aginctus is wired into NixOS services.
  operatorHome = pkgs.writeText "aginctus-operator-home.nix" ''
    { pkgs, ... }:
    {
      imports = [
        (import ${../home-manager-module.nix} {
          self.packages.${system}.aginctus = ${self.packages.${system}.aginctus};
        })
      ];
      home.username = "operator";
      home.homeDirectory = "/home/operator";
      home.stateVersion = "26.05";
      programs.aginctus = {
        enable = true;
        settings.gateway.id = "disposable-operator";
      };
    }
  '';

  # The Home Manager CLI/source are supplied by the pinned nixpkgs input.
  # No additional flake input, ambient channel, or network fetch is required.
  homeCommand = ''
    runuser -u operator -- env \
      HOME=/home/operator USER=operator LOGNAME=operator \
      NIX_PATH=nixpkgs=${nixpkgs}:home-manager=${pkgs.home-manager.src} \
      ${pkgs.home-manager}/bin/home-manager
  '';
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-home-manager-operator";

  nodes.machine = {
    system.stateVersion = "26.05";
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
    environment.systemPackages = [
      pkgs.incus
      pkgs.home-manager
      pkgs.jq
    ];
    environment.etc."aginctus/operator-home.nix".source = operatorHome;
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

    # Build and activate with the actual standalone Home Manager CLI, as the
    # unprivileged operator. No root-based HM integration is involved.
    operator = "${homeCommand}".strip()
    machine.succeed(f"{operator} build -f /etc/aginctus/operator-home.nix", timeout=600)
    machine.succeed(f"{operator} switch -f /etc/aginctus/operator-home.nix", timeout=600)
    machine.succeed("runuser -u operator -- test -L /home/operator/.config/aginctus/config.json")
    machine.succeed("runuser -u operator -- jq -e '.gateway.id == \"disposable-operator\"' /home/operator/.config/aginctus/config.json")
    machine.succeed("runuser -u operator -- /home/operator/.nix-profile/bin/aginctus config get gateway.id | grep -q disposable-operator")

    # Repeated activation remains an operator-only package/config change.
    machine.succeed(f"{operator} switch -f /etc/aginctus/operator-home.nix", timeout=600)
    machine.succeed("incus list --format json | jq -e 'length == 0'")
    machine.succeed("incus image list --format json | jq -e 'length == 0'")
    machine.succeed("test ! -e /etc/aginctus/config.json")
    machine.fail("systemctl list-unit-files --no-legend 'aginctus*' | grep -q '^aginctus'")
    machine.succeed("test ! -e /home/operator/.config/systemd/user/aginctus-reconcile.service")
    machine.succeed("test ! -e /home/operator/.config/systemd/user/aginctus-image-update.service")
  '';
}
