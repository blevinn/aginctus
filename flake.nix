{
  description = "Aginctus development environment";

  inputs = {
    # Stable NixOS 26.05. The generated flake.lock pins the exact revision.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

    # Herdr's published flake is pinned through Aginctus's flake.lock.
    herdr.url = "github:herdrdev/herdr";
  };

  outputs =
    {
      nixpkgs,
      herdr,
      ...
    }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          herdrPackage = herdr.packages.${system}.herdr;
          sshClientIdentityHelper = pkgs.writeShellApplication {
            name = "aginctus-ssh-client-identity";
            runtimeInputs = [
              pkgs.coreutils
              pkgs.openssh
              pkgs.util-linux
              pkgs.gnused
              pkgs.gawk
            ];
            text = builtins.readFile ./guest/ssh/client-identity.sh;
          };
          sshAuthorizeHelper = pkgs.writeShellApplication {
            name = "aginctus-ssh-authorize";
            runtimeInputs = [
              pkgs.coreutils
              pkgs.openssh
            ];
            text = builtins.readFile ./guest/ssh/authorize.sh;
          };

          herdrClientSystem = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
              (
                { ... }:
                {
                  system.stateVersion = "26.05";

                  environment.systemPackages = [
                    herdrPackage
                    pkgs.openssh
                    sshClientIdentityHelper
                  ];
                  environment.variables = {
                    XDG_CONFIG_HOME = "/var/lib/herdr/config";
                    XDG_RUNTIME_DIR = "/run/herdr";
                    XDG_STATE_HOME = "/var/lib/herdr";
                  };

                  systemd.services.herdr = {
                    description = "Herdr headless server";
                    wantedBy = [ "multi-user.target" ];
                    after = [ "network-online.target" ];
                    wants = [ "network-online.target" ];

                    serviceConfig = {
                      Environment = [
                        "HOME=/root"
                        "XDG_CONFIG_HOME=/var/lib/herdr/config"
                        "XDG_RUNTIME_DIR=/run/herdr"
                        "XDG_STATE_HOME=/var/lib/herdr"
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
          };

          opencodeWorkloadSystem = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
              (
                { ... }:
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
                    pkgs.opencode
                    pkgs.openssh
                    sshAuthorizeHelper
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
          };

          herdrClientRootfs = herdrClientSystem.config.system.build.squashfs;
          herdrClientMetadata = herdrClientSystem.config.system.build.metadata;
          opencodeWorkloadRootfs = opencodeWorkloadSystem.config.system.build.squashfs;
          opencodeWorkloadMetadata = opencodeWorkloadSystem.config.system.build.metadata;
        in
        {
          herdr-client = pkgs.runCommand "aginctus-herdr-client-image" { } ''
            mkdir -p "$out"
            ln -s ${herdrClientRootfs} "$out/rootfs"
            ln -s ${herdrClientMetadata} "$out/metadata"
          '';

          opencode-workload = pkgs.runCommand "aginctus-opencode-workload-image" { } ''
            mkdir -p "$out"
            ln -s ${opencodeWorkloadRootfs} "$out/rootfs"
            ln -s ${opencodeWorkloadMetadata} "$out/metadata"
          '';
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              git
              go
              incus
              jq
              just
              nixfmt
              shellcheck
              shfmt
            ];

            shellHook = ''
              echo "Aginctus development shell"
            '';
          };
        }
      );

      formatter = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        pkgs.nixfmt-tree
      );
    };
}
