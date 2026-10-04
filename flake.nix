{
  description = "Aginctus development environment";

  inputs = {
    # Stable NixOS 26.05. The generated flake.lock pins the exact revision.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
  };

  outputs =
    { nixpkgs, ... }:
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

          herdrSource =
            if system == "x86_64-linux" then
              pkgs.fetchurl {
                url = "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-linux-x86_64";
                hash = "sha256-GKjcZfHC+khYhDRDVt6hz9kRxvBs9G+njhk/QIf026c=";
              }
            else
              pkgs.fetchurl {
                url = "https://github.com/herdrdev/herdr/releases/download/v0.9.3/herdr-linux-aarch64";
                hash = "sha256-TeeqPiVniBLpKWDeZPfCqqG8ofD4CjxeVZg34jHh9cA=";
              };

          herdrPackage = pkgs.stdenvNoCC.mkDerivation {
            pname = "herdr";
            version = "0.9.3";
            dontUnpack = true;
            installPhase = ''
              mkdir -p "$out/bin"
              install -m 0755 ${herdrSource} "$out/bin/herdr"
            '';
          };

          herdrClientSystem = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
              (
                { ... }:
                {
                  system.stateVersion = "26.05";

                  environment.systemPackages = [ herdrPackage ];
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
