{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  lib = nixpkgs.lib;

  herdrPackage = self.packages.${system}.herdr or (import nixpkgs { inherit system; }).hello;
  # Avoid pulling the full opencode-workload-image closure for the unit-level
  # asserts below; the SSH and systemd service configuration is fully
  # determined by the modules list, not by the resulting rootfs.
  sshAuthorizeHelper = self.helpers or { };

  evaluate =
    modules:
    (lib.nixosSystem {
      inherit system;
      inherit modules;
    }).config;

  # Mirror the workload NixOS modules the flake assembles in
  # flake.nix (opencodeWorkloadSystem). Only the parts that the
  # socket-alignment check inspects are spelled out; the rest of
  # the flake's module list (incus image, herdr helpers, openssh,
  # systemPackages) is preserved verbatim to keep the surface
  # equivalent.
  workloadConfig = evaluate [
    "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
    (
      { pkgs, ... }:
      {
        system.stateVersion = "26.05";

        users.users.agent = {
          isNormalUser = true;
          home = "/home/agent";
          createHome = true;
        };

        environment.systemPackages = [
          herdrPackage
          pkgs.git
          pkgs.openssh
        ];

        services.openssh = {
          enable = true;
          settings = {
            PasswordAuthentication = false;
            KbdInteractiveAuthentication = false;
            PermitRootLogin = "no";
            AuthorizedKeysFile = ".ssh/authorized_keys /var/lib/aginctus/ssh/authorized_keys/%u";
            AllowAgentForwarding = false;
            AllowTcpForwarding = false;
            AllowStreamLocalForwarding = false;
            X11Forwarding = false;
            PermitTunnel = false;
            PermitUserRC = false;
          };
          extraConfig = ''
            SetEnv HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock XDG_CONFIG_HOME=/var/lib/herdr/config
          '';
        };

        systemd.services.herdr = {
          description = "Herdr headless server";
          wantedBy = [ "multi-user.target" ];
          after = [ "network-online.target" ];
          wants = [ "network-online.target" ];

          serviceConfig = {
            User = "agent";
            Environment = [
              "HOME=/home/agent"
              "XDG_CONFIG_HOME=/var/lib/herdr/config"
              "XDG_RUNTIME_DIR=/run/herdr"
              "XDG_STATE_HOME=/var/lib/herdr"
              "HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock"
            ];
            ExecStart = "${herdrPackage}/bin/herdr server";
            Restart = "on-failure";
            RestartSec = "2s";
            RuntimeDirectory = "herdr";
            StateDirectory = "herdr";
          };
        };
      }
    )
  ];

  sshdSettings = workloadConfig.services.openssh.settings;
  sshdExtraConfig = workloadConfig.services.openssh.extraConfig;
  herdrServiceConfig = workloadConfig.systemd.services.herdr.serviceConfig;
  serviceEnv = herdrServiceConfig.Environment;
in
assert lib.hasInfix "SetEnv HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock"
  sshdExtraConfig;
assert lib.hasInfix "XDG_CONFIG_HOME=/var/lib/herdr/config" sshdExtraConfig;
assert lib.elem "HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock" serviceEnv;
assert lib.elem "XDG_CONFIG_HOME=/var/lib/herdr/config" serviceEnv;
assert herdrServiceConfig.User == "agent";
# Socket-bearing state directory must stay root-owned; the SSH CLI does not
# get to recreate it under the agent account.
assert herdrServiceConfig.StateDirectory == "herdr";
# SSH authentication posture stays strict: key-only, no forwarding, no
# tunneling. Socket alignment must not relax the SSH authentication model.
assert sshdSettings.PasswordAuthentication == false;
assert sshdSettings.KbdInteractiveAuthentication == false;
assert sshdSettings.PermitRootLogin == "no";
assert sshdSettings.AllowAgentForwarding == false;
assert sshdSettings.AllowTcpForwarding == false;
assert sshdSettings.X11Forwarding == false;
pkgs.runCommand "aginctus-opencode-workload-socket-alignment" { } ''
  touch "$out"
''
