{
  description = "Aginctus development environment";

  inputs = {
    # Stable NixOS 26.05. The generated flake.lock pins the exact revision.
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
  };

  outputs = { nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              git
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
