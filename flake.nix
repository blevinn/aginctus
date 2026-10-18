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
      self,
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
      nixosModules = {
        aginctus = import ./nix/nixos-module.nix { inherit self; };
        default = self.nixosModules.aginctus;
      };

      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          herdrPackage = herdr.packages.${system}.herdr;
          aginctusCli = pkgs.buildGoModule {
            pname = "aginctus";
            version = "0.0.0-dev";
            src = ./.;
            # Recalculate after Go module graph changes. A stale hash can reuse
            # an incompatible vendor tree even when go.mod is newer.
            vendorHash = "sha256-ao0Q95mYJ8xWhKWwhBQsBjtdF8bWMAXRG+c/kNOZkSQ=";
            subPackages = [ "./cmd/aginctus" ];
            nativeBuildInputs = [ pkgs.makeWrapper ];
            postInstall = ''
              wrapProgram "$out/bin/aginctus" --prefix PATH : "${pkgs.lib.makeBinPath [ pkgs.incus ]}"
            '';
          };
          oauthGatewayDaemon = pkgs.buildGoModule {
            pname = "aginctus-oauth-gateway";
            version = "0.0.0-dev";
            src = ./.;
            vendorHash = "sha256-ao0Q95mYJ8xWhKWwhBQsBjtdF8bWMAXRG+c/kNOZkSQ=";
            subPackages = [ "./cmd/aginctus-oauth-gateway" ];
          };
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
          sshMachineConfigHelper = pkgs.writeShellApplication {
            name = "aginctus-ssh-machine-config";
            runtimeInputs = [
              pkgs.coreutils
              pkgs.util-linux
            ];
            text = builtins.readFile ./guest/ssh/machine-config.sh;
          };
          sshHostTrustHelper = pkgs.writeShellApplication {
            name = "aginctus-ssh-host-trust";
            runtimeInputs = [
              pkgs.coreutils
              pkgs.openssh
            ];
            text = builtins.readFile ./guest/ssh/host-trust.sh;
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
              (import ./nix/guest-state-bindings.nix {
                bindings = [
                  {
                    class = "system";
                    subdir = "herdr";
                    target = "/var/lib/herdr";
                  }
                  {
                    class = "system";
                    subdir = "ssh";
                    target = "/var/lib/aginctus/ssh";
                  }
                ];
              })
              (
                { ... }:
                {
                  system.stateVersion = "26.05";

                  environment.systemPackages = [
                    herdrPackage
                    pkgs.openssh
                    sshClientIdentityHelper
                    sshHostTrustHelper
                    sshMachineConfigHelper
                  ];

                  # Only the managed Herdr client consumes this guest-owned
                  # persistent SSH config. All identity and host-key settings
                  # are recorded under the protected state-system volume.
                  programs.ssh.extraConfig = ''
                    Include /var/lib/aginctus/ssh/machines/*.conf
                  '';

                  environment.variables = {
                    XDG_CONFIG_HOME = "/var/lib/herdr/config";
                    XDG_RUNTIME_DIR = "/run/herdr";
                    XDG_STATE_HOME = "/var/lib/herdr";
                  };

                  systemd.services.herdr = {
                    description = "Herdr headless server";
                    wantedBy = [ "multi-user.target" ];
                    after = [
                      "network-online.target"
                      "var-lib-herdr.mount"
                      "var-lib-aginctus-ssh.mount"
                    ];
                    wants = [ "network-online.target" ];
                    requires = [
                      "var-lib-herdr.mount"
                      "var-lib-aginctus-ssh.mount"
                    ];

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

          # Bundle the OpenCode guest Python scripts into a single store
          # directory so the shared validator module sits alongside
          # provision.py and launch.py and is importable at runtime.
          opencodeGuestScripts = pkgs.runCommand "aginctus-opencode-guest-scripts" { } ''
            mkdir -p $out
            cp ${./guest/opencode/provision.py} $out/provision.py
            cp ${./guest/opencode/launch.py} $out/launch.py
            cp ${./guest/opencode/gateway_endpoint.py} $out/gateway_endpoint.py
          '';

          # Runtime-only gateway configuration: no endpoint, model or key is
          # materialized into the Nix store. Herdr's PATH resolves this wrapper.
          aginctusOpenCode = pkgs.writeShellScriptBin "opencode" ''
            exec ${pkgs.python3}/bin/python3 ${opencodeGuestScripts}/launch.py ${pkgs.opencode}/bin/opencode "$@"
          '';

          gatewayProvisionHelper = pkgs.writeShellScriptBin "aginctus-opencode-gateway-provision" ''
            exec ${pkgs.python3}/bin/python3 ${opencodeGuestScripts}/provision.py
          '';

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
                    aginctusOpenCode
                    gatewayProvisionHelper
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
                    # The remote Herdr CLI resolves its API socket from
                    # HERDR_SOCKET_PATH; declare it once via sshd's SetEnv so
                    # every SSH session (including Herdr's `herdr --machine
                    # <label> workspace list`) reaches the systemd-managed
                    # server socket without a per-call wrapper, shell export
                    # or user-writable symlink. The path mirrors the systemd
                    # service's XDG_CONFIG_HOME (see below).
                    extraConfig = ''
                      SetEnv HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock
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
                        # Belt-and-braces: the server is a managed daemon and
                        # must answer the same socket the SSH CLI declares
                        # above. Without this, `HERDR_SOCKET_PATH` would be
                        # ignored on the server side and the CLI would probe
                        # an unrelated path on first contact.
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
          };

          # This image is not yet included in live gateway orchestration.
          oauthGatewaySystem = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
              (
                { ... }:
                {
                  system.stateVersion = "26.05";
                  users.groups.aginctus-oauth = { };
                  users.users.aginctus-oauth = {
                    isSystemUser = true;
                    group = "aginctus-oauth";
                    home = "/var/lib/aginctus/oauth";
                  };
                  environment.systemPackages = [ oauthGatewayDaemon ];
                  systemd.services.aginctus-oauth-gateway = {
                    description = "Aginctus isolated OAuth provider backend (not inference-ready)";
                    wantedBy = [ "multi-user.target" ];
                    requires = [ "var-lib-aginctus-oauth.mount" ];
                    after = [ "var-lib-aginctus-oauth.mount" ];
                    unitConfig.AssertPathIsMountPoint = "/var/lib/aginctus/oauth";
                    serviceConfig = {
                      Type = "simple";
                      User = "aginctus-oauth";
                      Group = "aginctus-oauth";
                      WorkingDirectory = "/var/lib/aginctus/oauth";
                      ExecStart = "${oauthGatewayDaemon}/bin/aginctus-oauth-gateway";
                      Restart = "on-failure";
                      NoNewPrivileges = true;
                      PrivateTmp = true;
                      ProtectSystem = "strict";
                      ProtectHome = true;
                      ReadWritePaths = [ "/var/lib/aginctus/oauth" ];
                    };
                  };
                }
              )
            ];
          };
          oauthGatewayRootfs = oauthGatewaySystem.config.system.build.squashfs;
          oauthGatewayMetadata = oauthGatewaySystem.config.system.build.metadata;

          herdrClientRootfs = herdrClientSystem.config.system.build.squashfs;
          herdrClientMetadata = herdrClientSystem.config.system.build.metadata;
          opencodeWorkloadRootfs = opencodeWorkloadSystem.config.system.build.squashfs;
          opencodeWorkloadMetadata = opencodeWorkloadSystem.config.system.build.metadata;
        in
        {
          aginctus = aginctusCli;
          default = aginctusCli;
          # Expose exactly the runtime used by the workload image for contract tests.
          opencode-runtime = pkgs.opencode;

          oauth-gateway = pkgs.runCommand "aginctus-oauth-gateway-image" { } ''
            mkdir -p "$out"
            ln -s ${oauthGatewayRootfs} "$out/rootfs"
            ln -s ${oauthGatewayMetadata} "$out/metadata"
          '';

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
        # Keep VM integration tests explicitly buildable, outside the default
        # flake checks, so routine validation does not execute heavyweight VMs.
        // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
          firewall-iptables-integration = import ./nix/firewall-iptables-test.nix {
            inherit self nixpkgs system;
          };
          firewall-firewalld-integration = import ./nix/firewall-firewalld-test.nix {
            inherit self nixpkgs system;
          };
          firewall-nixos-nft-integration = import ./nix/firewall-nixos-nft-test.nix {
            inherit self nixpkgs system;
          };
          herdr-socket-integration = import ./nix/herdr-socket-vm-test.nix {
            inherit
              self
              nixpkgs
              herdr
              system
              ;
          };
        }
      );

      checks = forAllSystems (system: {
        nixos-module = import ./nix/module-check.nix { inherit self nixpkgs system; };
        opencode-workload-socket-alignment = import ./nix/opencode-workload-check.nix {
          inherit self nixpkgs system;
        };
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          herdrCli = herdr.packages.${system}.herdr;
        in
        {
          default = pkgs.mkShell {
            packages =
              (with pkgs; [
                git
                go
                incus
                jq
                just
                nixfmt
                python3
                shellcheck
                shfmt
              ])
              ++ [ herdrCli ];

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
