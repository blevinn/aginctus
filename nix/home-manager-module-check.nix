{
  self,
  nixpkgs,
  system,
}:
let
  lib = nixpkgs.lib;
  pkgs = import nixpkgs { inherit system; };
  stub = pkgs.writeShellScriptBin "aginctus-hm-check" "exit 0";

  # Exercise the exported module without adding Home Manager as a flake input.
  # The fixture declares only the host option shapes used by the module;
  # a real standalone Home Manager activation remains a separate smoke test.
  evaluate =
    extra:
    (lib.evalModules {
      modules = [
        self.homeManagerModules.default
        (
          { lib, ... }:
          {
            options.home.packages = lib.mkOption {
              type = lib.types.listOf lib.types.package;
              default = [ ];
            };
            options.xdg.configFile = lib.mkOption {
              type = lib.types.attrsOf lib.types.anything;
              default = { };
            };
            options.assertions = lib.mkOption {
              type = lib.types.listOf (
                lib.types.submodule {
                  options = {
                    assertion = lib.mkOption { type = lib.types.bool; };
                    message = lib.mkOption { type = lib.types.str; };
                  };
                }
              );
              default = [ ];
            };
          }
        )
        extra
      ];
    }).config;

  disabled = evaluate { };
  enabled = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.incus.management.network.name = "operator-test";
    };
  };
  invalidNested = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.gateway.postgres.password = "not-a-valid-setting";
    };
  };
  invalidDotted = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.gateway."master.key" = "not-a-valid-setting";
    };
  };
  rejected =
    candidate:
    lib.any (
      assertion:
      !assertion.assertion
      && lib.hasPrefix "programs.aginctus.settings cannot contain runtime gateway secrets:" assertion.message
    ) candidate.assertions;
in
assert disabled.home.packages == [ ];
assert disabled.xdg.configFile == { };
assert disabled.assertions == [ ];
assert enabled.home.packages == [ stub ];
assert enabled.programs.aginctus.settings.incus.management.network.name == "operator-test";
assert enabled.programs.aginctus.settings.gateway.id == "local";
assert enabled.programs.aginctus.settings.firewall.zone == "aginctus-dns";
assert enabled.programs.aginctus.settings.workloads.dev.runtime.type == "opencode";
assert builtins.hasAttr "aginctus/config.json" enabled.xdg.configFile;
assert !rejected enabled;
assert rejected invalidNested;
assert rejected invalidDotted;
pkgs.runCommand "aginctus-home-manager-operator-contract" { } ''
  touch "$out"
''
