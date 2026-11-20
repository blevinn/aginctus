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
        aginctus = import ./nix/modules/nixos-module.nix { inherit self; };
        aginctusTestenvRecovery = import ./internal/testenv/nix/recovery-worker.nix { inherit self; };
        aginctusTestenvWorker = import ./internal/testenv/nix/installed-worker.nix { inherit self; };
        default = self.nixosModules.aginctus;
      };

      homeManagerModules = {
        aginctus = import ./nix/modules/home-manager-module.nix { inherit self; };
        default = self.homeManagerModules.aginctus;
      };

      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          herdrPackage = herdr.packages.${system}.herdr;
          herdrClientCommand = import ./nix/workloads/herdr-client-command.nix { inherit pkgs herdrPackage; };
          releaseVersion = pkgs.lib.strings.removeSuffix "\n" (builtins.readFile ./internal/cli/VERSION);
          aginctusCli = pkgs.buildGoModule {
            pname = "aginctus";
            version = "${releaseVersion}+${self.shortRev or "source"}";
            src = ./.;
            # Dirty trees and non-Git sources get content identity, not a false commit claim.
            ldflags = [
              "-X=github.com/blevinn/aginctus/internal/cli.Revision=${self.rev or "source-${self.narHash}"}"
            ];
            # Recalculate after Go module graph changes. A stale hash can reuse
            # an incompatible vendor tree even when go.mod is newer.
            vendorHash = "sha256-0k7OkHckPVEZEbFkQ1ojBbIEc7Lj0nvfWsYWOy18AdI=";
            subPackages = [ "./cmd/aginctus" ];
            nativeBuildInputs = [ pkgs.makeWrapper ];
            postInstall = ''
              wrapProgram "$out/bin/aginctus" --prefix PATH : "${pkgs.lib.makeBinPath [ pkgs.incus ]}"
            '';
          };
          oauthGatewayDaemon = pkgs.buildGoModule {
            pname = "aginctus-oauth-gateway";
            version = releaseVersion;
            src = ./.;
            vendorHash = "sha256-0k7OkHckPVEZEbFkQ1ojBbIEc7Lj0nvfWsYWOy18AdI=";
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
            text = builtins.readFile ./nix/guest/ssh/client-identity.sh;
          };
          sshMachineConfigHelper = pkgs.writeShellApplication {
            name = "aginctus-ssh-machine-config";
            runtimeInputs = [
              pkgs.coreutils
              pkgs.util-linux
            ];
            text = builtins.readFile ./nix/guest/ssh/machine-config.sh;
          };
          sshHostTrustHelper = pkgs.writeShellApplication {
            name = "aginctus-ssh-host-trust";
            runtimeInputs = [
              pkgs.coreutils
              pkgs.openssh
            ];
            text = builtins.readFile ./nix/guest/ssh/host-trust.sh;
          };
          sshAuthorizeHelper = pkgs.writeShellApplication {
            name = "aginctus-ssh-authorize";
            runtimeInputs = [
              pkgs.coreutils
              pkgs.openssh
            ];
            text = builtins.readFile ./nix/guest/ssh/authorize.sh;
          };

          herdrClientImage = import ./plugins/herdr/images/client.nix {
            inherit
              nixpkgs
              system
              pkgs
              herdrPackage
              herdrClientCommand
              sshClientIdentityHelper
              sshHostTrustHelper
              sshMachineConfigHelper
              ;
            guestStateBindings = import ./nix/workloads/guest-state-bindings.nix;
          };

          # Bundle the OpenCode guest Python scripts into a single store
          # directory so the shared validator module sits alongside
          # provision.py and launch.py and is importable at runtime.
          opencodeGuestScripts = pkgs.runCommand "aginctus-opencode-guest-scripts" { } ''
            mkdir -p $out
            cp ${./plugins/opencode/guest/provision.py} $out/provision.py
            cp ${./plugins/opencode/guest/launch.py} $out/launch.py
            cp ${./plugins/opencode/guest/gateway_endpoint.py} $out/gateway_endpoint.py
            cp ${./plugins/opencode/guest/go_session_plugin.js} $out/go_session_plugin.js
            cp ${./plugins/opencode/guest/siwc_plugin.js} $out/siwc_plugin.js
          '';

          # Scoped source patch for the flake-pinned CLI event-consumer join.
          aginctusPinnedOpenCode = import ./nix/workloads/patched-opencode.nix { inherit pkgs; };

          # Runtime-only gateway configuration: no endpoint, model or key is
          # materialized into the Nix store. Herdr's PATH resolves this wrapper.
          aginctusOpenCode = pkgs.writeShellScriptBin "opencode" ''
            exec ${pkgs.python3}/bin/python3 ${opencodeGuestScripts}/launch.py ${aginctusPinnedOpenCode}/bin/opencode "$@"
          '';

          gatewayProvisionHelper = pkgs.writeShellScriptBin "aginctus-opencode-gateway-provision" ''
            exec ${pkgs.python3}/bin/python3 ${opencodeGuestScripts}/provision.py
          '';
          # Root-owned, pinned source and interpreter; operator-only host
          # invocation remains separate from the guest's network capability.
          opencodeEgressProbe = pkgs.writeShellScriptBin "aginctus-egress-probe" ''
            exec ${pkgs.python3}/bin/python3 -I -B ${./plugins/opencode/guest/egress_probe.py} "$@"
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
                    opencodeEgressProbe
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
                    # Herdr's remote API bridge invokes '--session default'.
                    # Explicit sessions resolve their socket from config_dir()
                    # before consulting HERDR_SOCKET_PATH. Non-login SSH exec
                    # does not inherit the systemd daemon's XDG variables, so
                    # pin XDG_CONFIG_HOME to the daemon's directory as well.
                    # No user-writable symlinks, per-call overrides or relaxed SSH.
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

          opencodeWorkloadRootfs = opencodeWorkloadSystem.config.system.build.squashfs;
          opencodeWorkloadMetadata = opencodeWorkloadSystem.config.system.build.metadata;
        in
        {
          # Separate operator-only CLI; never registered by aginctus/MCP.
          testenv-cli = pkgs.buildGoModule {
            pname = "aginctus-testenv";
            version = releaseVersion;
            src = ./.;
            vendorHash = "sha256-0k7OkHckPVEZEbFkQ1ojBbIEc7Lj0nvfWsYWOy18AdI=";
            subPackages = [ "./cmd/aginctus-testenv" ];
            ldflags = [ "-X=main.revision=${self.rev or "source-${self.narHash}"}" ];
          };
          # Standalone opt-in accepted-state tools. Deliberately NOT installed
          # by NixOS/Home Manager modules and never exposed as an MCP command.
          # The service remains read-only; the local tool can promote only
          # explicit pinned workload candidates in its own private same-UID store.
          state-service = pkgs.buildGoModule {
            pname = "aginctus-state";
            version = releaseVersion;
            src = ./.;
            vendorHash = "sha256-0k7OkHckPVEZEbFkQ1ojBbIEc7Lj0nvfWsYWOy18AdI=";
            subPackages = [ "./cmd/aginctus-state" ];
          };
          state-local = pkgs.buildGoModule {
            pname = "aginctus-state-local";
            version = releaseVersion;
            src = ./.;
            vendorHash = "sha256-0k7OkHckPVEZEbFkQ1ojBbIEc7Lj0nvfWsYWOy18AdI=";
            subPackages = [ "./cmd/aginctus-state-local" ];
          };
          aginctus = aginctusCli;
          default = aginctusCli;
          # Expose exactly the runtime used by the workload image for contract tests.
          opencode-runtime = aginctusPinnedOpenCode;
          # Test-only Go toolchain; retain the production Herdr/OpenCode launcher.
          vm-harness-opencode-workload =
            let
              guest = opencodeWorkloadSystem.extendModules {
                modules = [
                  {
                    environment.systemPackages = [
                      pkgs.go
                      (pkgs.writeShellScriptBin "aginctus-vm-guest-credentials" ''
                        exec ${pkgs.python3}/bin/python3 ${./tests/vm_harness/guest_credentials.py} "$@"
                      '')
                    ];
                  }
                ];
              };
            in
            pkgs.runCommand "aginctus-full-stack-workload-image" { } ''
              mkdir -p "$out"
              ln -s ${guest.config.system.build.squashfs} "$out/rootfs"
              ln -s ${guest.config.system.build.metadata} "$out/metadata"
            '';

          oauth-gateway = pkgs.runCommand "aginctus-oauth-gateway-image" { } ''
            mkdir -p "$out"
            ln -s ${oauthGatewayRootfs} "$out/rootfs"
            ln -s ${oauthGatewayMetadata} "$out/metadata"
          '';

          herdr-client = herdrClientImage.image;

          opencode-workload = pkgs.runCommand "aginctus-opencode-workload-image" { } ''
            mkdir -p "$out"
            ln -s ${opencodeWorkloadRootfs} "$out/rootfs"
            ln -s ${opencodeWorkloadMetadata} "$out/metadata"
          '';
          # Separate Incus VM disk + metadata archive, built only when
          # explicitly selected; not a run-nixos-vm QEMU batch launcher.
        }
        // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
          testenv-incus-vm-image =
            (import ./internal/testenv/nix/incus-vm-image.nix {
              inherit self nixpkgs system;
            }).image;
        }
        # Keep VM integration tests explicitly buildable, outside the default
        # flake checks, so routine validation does not execute heavyweight VMs.
        // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
          firewall-iptables-integration = import ./tests/nix/firewall/firewall-iptables-test.nix {
            inherit self nixpkgs system;
          };
          firewall-firewalld-integration = import ./tests/nix/firewall/firewall-firewalld-test.nix {
            inherit self nixpkgs system;
          };
          firewall-egress-bootstrap = import ./tests/nix/firewall/firewall-egress-bootstrap-test.nix {
            inherit self nixpkgs system;
          };
          firewall-nixos-nft-integration = import ./tests/nix/firewall/firewall-nixos-nft-test.nix {
            inherit self nixpkgs system;
          };
          # Heavyweight: explicitly selected, never part of default flake checks.
          vm-harness-smoke = import ./tests/nix/vm-harness/smoke.nix {
            inherit self nixpkgs system;
          };
          vm-harness-storage-network = import ./tests/nix/vm-harness/storage-network.nix {
            inherit self nixpkgs system;
          };
          vm-harness-runtime-lifecycle = import ./tests/nix/vm-harness/runtime-lifecycle.nix {
            inherit self nixpkgs system;
          };
          vm-harness-gateway-topology = import ./tests/nix/vm-harness/gateway-topology.nix {
            inherit self nixpkgs system;
          };
          vm-harness-native-oci = import ./tests/nix/vm-harness/native-oci.nix {
            inherit self nixpkgs system;
          };
          vm-harness-native-instance = import ./tests/nix/vm-harness/native-instance.nix {
            inherit self nixpkgs system;
          };
          vm-harness-synthetic-gateway = import ./tests/nix/vm-harness/gateway-topology.nix {
            inherit self nixpkgs system;
            synthetic = true;
          };
          vm-harness-synthetic-tool-gateway = import ./tests/nix/vm-harness/gateway-topology.nix {
            inherit self nixpkgs system;
            synthetic = true;
            toolTransport = true;
          };
          vm-harness-opencode-tool-loopback = import ./tests/nix/vm-harness/opencode-tool-loopback.nix {
            inherit self nixpkgs system;
          };
          vm-harness-full-stack = import ./tests/nix/vm-harness/gateway-topology.nix {
            inherit self nixpkgs system;
            synthetic = true;
            fullStack = true;
          };
          vm-harness-ownership-recovery = import ./tests/nix/vm-harness/ownership-recovery.nix {
            inherit self nixpkgs system;
          };
          herdr-socket-integration = import ./nix/herdr-socket-vm-test.nix {
            herdrClientModule = herdrClientImage.guestModule;
            inherit
              self
              nixpkgs
              herdr
              system
              ;
          };
        }
      );

      # Explicit heavyweight VM tests whose evaluation may require an
      # import-from-derivation (e.g. Home Manager's packaged source). Keep
      # these out of packages/checks: nix flake check evaluates every package
      # even when its derivation is not built.
      # Run explicitly: nix build -L --no-link .#vmTests.x86_64-linux.home-manager-operator
      vmTests = forAllSystems (system: {
        home-manager-operator = import ./tests/nix/vm-harness/home-manager-operator.nix {
          inherit self nixpkgs system;
        };
        incus-project-authority = import ./tests/nix/vm-harness/incus-project-authority.nix {
          inherit self nixpkgs system;
        };
        accepted-state-incus = import ./tests/nix/vm-harness/accepted-state-incus.nix {
          inherit self nixpkgs system;
        };
      });

      checks = forAllSystems (
        system:
        {
          nixos-module = import ./tests/nix/module-check.nix { inherit self nixpkgs system; };
          herdr-image-selection = import ./tests/nix/herdr-image-check.nix {
            inherit self nixpkgs system;
          };
          home-manager-module = import ./tests/nix/home-manager-module-check.nix {
            inherit self nixpkgs system;
          };
          operator-settings-parity = import ./tests/nix/operator-settings-parity-check.nix {
            inherit self nixpkgs system;
          };
          gateway-config-parity = import ./tests/nix/gateway-config-parity-check.nix {
            inherit self nixpkgs system;
          };
          opencode-workload-socket-alignment = import ./tests/nix/opencode-workload-check.nix {
            inherit self nixpkgs system;
          };
        }
        // nixpkgs.lib.optionalAttrs (system == "x86_64-linux") {
          testenv-incus-vm-image-config = import ./internal/testenv/nix/incus-vm-image-check.nix {
            inherit self nixpkgs system;
          };
          testenv-recovery-worker-policy = import ./internal/testenv/nix/recovery-worker-check.nix {
            inherit self nixpkgs system;
          };
          testenv-installed-worker-policy = import ./internal/testenv/nix/installed-worker-check.nix {
            inherit self nixpkgs system;
          };
        }
      );

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
                golangci-lint
                incus
                jq
                just
                nixfmt
                (python3.withPackages (ps: [
                  ps.markdown-it-py
                  ps.pyjwt
                  ps.cryptography
                ]))
                ruff
                shellcheck
                shfmt
              ])
              ++ [ herdrCli ];

            shellHook = ''
              echo "Aginctus development shell" >&2
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
