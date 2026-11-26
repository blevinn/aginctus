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
        aginctusTestenvRecovery = import ./internal/testing/testenv/nix/recovery-worker.nix {
          inherit self;
        };
        aginctusTestenvWorker = import ./internal/testing/testenv/nix/installed-worker.nix {
          inherit self;
        };
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
          goPackages = import ./nix/packages/go.nix {
            inherit pkgs releaseVersion self;
            sourceRoot = ./.;
          };
          aginctusCli = goPackages.aginctusCli;
          oauthGatewayDaemon = goPackages.oauthGatewayDaemon;
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

          opencodeBuild = import ./nix/workloads/opencode-image.nix {
            inherit
              pkgs
              nixpkgs
              system
              herdrPackage
              sshAuthorizeHelper
              ;
          };
          aginctusPinnedOpenCode = opencodeBuild.runtime;
          opencodeWorkloadSystem = opencodeBuild.system;

          opencodeWorkloadRootfs = opencodeWorkloadSystem.config.system.build.squashfs;
          opencodeWorkloadMetadata = opencodeWorkloadSystem.config.system.build.metadata;
        in
        {
          # Separate operator-only CLI; never registered by aginctus/MCP.
          testenv-cli = goPackages.testenv-cli;
          state-service = goPackages.state-service;
          state-project-fixture = goPackages.state-project-fixture;
          state-local = goPackages.state-local;
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

          oauth-gateway = import ./nix/workloads/oauth-gateway.nix {
            inherit
              pkgs
              nixpkgs
              system
              oauthGatewayDaemon
              ;
          };

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
          testenv-managed-native-instance =
            import ./internal/testing/testenv/nix/disposable-worker-fixture.nix
              {
                inherit self nixpkgs system;
                nativeInstance = true;
              };
          testenv-independent-recovery = import ./internal/testing/testenv/nix/disposable-worker-fixture.nix {
            inherit self nixpkgs system;
            independentRecovery = true;
          };
          testenv-disposable-worker-fixture =
            import ./internal/testing/testenv/nix/disposable-worker-fixture.nix
              {
                inherit self nixpkgs system;
              };
          testenv-native-instance-vm-image =
            (import ./internal/testing/testenv/nix/incus-vm-image.nix {
              inherit self nixpkgs system;
              nativeInstance = true;
            }).image;
          testenv-incus-vm-image =
            (import ./internal/testing/testenv/nix/incus-vm-image.nix {
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
          vm-harness-native-gateway-stopped-pair = import ./tests/nix/vm-harness/native-oci.nix {
            inherit self nixpkgs system;
            scenario = "stopped-pair";
          };
          vm-harness-native-gateway-stop-retention = import ./tests/nix/vm-harness/native-oci.nix {
            inherit self nixpkgs system;
            scenario = "stop-retention";
          };
          vm-harness-acceptance-project-image = import ./tests/nix/vm-harness/acceptance-project-image.nix {
            inherit self nixpkgs system;
          };
          vm-harness-acceptance-nft = import ./tests/nix/firewall/firewall-nixos-nft-test.nix {
            inherit self nixpkgs system;
          };
          vm-harness-acceptance-egress-denial =
            import ./tests/nix/firewall/firewall-egress-bootstrap-test.nix
              {
                inherit self nixpkgs system;
              };
          vm-harness-acceptance-firewalld = import ./tests/nix/firewall/firewall-firewalld-test.nix {
            inherit self nixpkgs system;
          };
          vm-harness-acceptance-state-boundary = import ./tests/nix/vm-harness/acceptance-state-boundary.nix {
            inherit self nixpkgs system;
          };
          vm-harness-native-instance = import ./tests/nix/vm-harness/native-instance.nix {
            inherit self nixpkgs system;
          };
          vm-harness-native-accepted-incus-observation =
            import ./tests/nix/vm-harness/native-accepted-incus-observation.nix
              {
                inherit self nixpkgs system;
              };
          vm-harness-native-joined-lifecycle =
            import ./tests/nix/vm-harness/native-accepted-incus-observation.nix
              {
                inherit self nixpkgs system;
                joinedLifecycle = true;
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
          # The Compose full-stack fixture is suspended pending the native port (#1115).
          vm-harness-ownership-recovery = import ./tests/nix/vm-harness/ownership-recovery.nix {
            inherit self nixpkgs system;
          };
          herdr-socket-integration = import ./tests/nix/herdr-socket-vm-test.nix {
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
        environment-project-state-incus =
          import ./tests/nix/vm-harness/environment-project-state-incus.nix
            {
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
          environment-candidate-parity = import ./tests/nix/environment-candidate-parity-check.nix {
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
          testenv-incus-vm-image-config = import ./internal/testing/testenv/nix/incus-vm-image-check.nix {
            inherit self nixpkgs system;
          };
          testenv-recovery-worker-policy = import ./internal/testing/testenv/nix/recovery-worker-check.nix {
            inherit self nixpkgs system;
          };
          testenv-installed-worker-policy = import ./internal/testing/testenv/nix/installed-worker-check.nix {
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
