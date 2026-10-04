{
  description = "Aginctus development environment";

  inputs = {
    # Stable NixOS 26.05. The generated flake.lock pins the exact revision.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

    # Herdr is pinned to the release used in the generated client image.
    herdr.url = "github:herdrdev/herdr/v0.9.1";
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

          herdrClientSystem = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
              (
                { ... }:
                {
                  system.stateVersion = "26.05";

                  environment.systemPackages = [ herdrPackage ];

                  systemd.services.herdr = {
                    description = "Herdr headless server";
                    wantedBy = [ "multi-user.target" ];
                    after = [ "network-online.target" ];
                    wants = [ "network-online.target" ];

                    serviceConfig = {
                      ExecStart = "${herdrPackage}/bin/herdr server";
                      Restart = "on-failure";
                      RestartSec = "2s";
                    };
                  };
                }
              )
            ];
          };

          rootfs = herdrClientSystem.config.system.build.squashfs;
          metadata = herdrClientSystem.config.system.build.metadata;
        in
        {
          herdr-client = pkgs.runCommand "aginctus-herdr-client-image" { } ''
            mkdir -p "$out"
            ln -s ${rootfs} "$out/rootfs"
            ln -s ${metadata} "$out/metadata"
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
        pkgs.nixfmt
      );
    };
}
