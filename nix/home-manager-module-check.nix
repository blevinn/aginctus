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
      # evalModules is lower-level than Home Manager and does not supply pkgs.
      # Provide the same package set as a standalone HM evaluation would.
      specialArgs = { inherit pkgs; };
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
  alternateProject = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.incus.project = "aginctus-user";
    };
  };
  invalidProjectDefault = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.incus.project = "default";
    };
  };
  invalidProjectPath = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.incus.project = "../escape";
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
  rejectedMCP =
    candidate:
    lib.any (
      assertion: !assertion.assertion && lib.hasPrefix "programs.aginctus.settings.mcp." assertion.message
    ) candidate.assertions;
  withMCP = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.mcp = {
        servers.docs = {
          transport = "streamable-http";
          url = "https://docs.example.com/mcp";
          credentialRef = "docs-token";
          allowedTools = [
            "fetch"
            "search"
          ];
        };
        servers.local = {
          transport = "stdio";
          command = "/usr/bin/mcp-local";
        };
        capabilities.coding = {
          protocol = "responses";
          requires = [ "mcp-tools" ];
        };
        grants.dev = {
          capability = "coding";
          servers.docs = [ "search" ];
        };
        gateway.enabled = true;
      };
    };
  };
  unauthenticatedGateway = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.mcp.gateway = {
        enabled = true;
        requireAuthenticatedWorkload = false;
      };
    };
  };
  remoteWithoutURL = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.mcp.servers.docs = {
        transport = "streamable-http";
      };
    };
  };
  forbiddenCredentialField = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.mcp.servers.docs = {
        transport = "stdio";
        command = "/bin/mcp-local";
        headers = {
          Authorization = "Bearer forbidden";
        };
      };
    };
  };
in
assert disabled.home.packages == [ ];
assert disabled.xdg.configFile == { };
assert !(builtins.hasAttr "aginctus/operator-package.json" disabled.xdg.configFile);
assert disabled.assertions == [ ];
assert enabled.home.packages == [ stub ];
assert enabled.programs.aginctus.settings.incus.management.network.name == "operator-test";
assert enabled.programs.aginctus.settings.incus.project == "aginctus";
assert alternateProject.programs.aginctus.settings.incus.project == "aginctus-user";
assert !(builtins.tryEval invalidProjectDefault.programs.aginctus.settings.incus.project).success;
assert !(builtins.tryEval invalidProjectPath.programs.aginctus.settings.incus.project).success;
assert enabled.programs.aginctus.settings.gateway.id == "local";
assert enabled.programs.aginctus.settings.firewall.zone == "aginctus-dns";
assert enabled.programs.aginctus.settings.workloads.dev.runtime.type == "opencode";
assert builtins.hasAttr "aginctus/config.json" enabled.xdg.configFile;
assert builtins.hasAttr "aginctus/operator-package.json" enabled.xdg.configFile;
assert lib.hasInfix "aginctus-user-package.json" (
  toString enabled.xdg.configFile."aginctus/operator-package.json".source
);
assert !rejected enabled;
assert rejected invalidNested;
assert rejected invalidDotted;
# The MCP subtree stays deny by default and never creates a grant.
assert enabled.programs.aginctus.settings.mcp.gateway.enabled == false;
assert enabled.programs.aginctus.settings.mcp.gateway.requireAuthenticatedWorkload;
assert enabled.programs.aginctus.settings.mcp.servers == { };
assert enabled.programs.aginctus.settings.mcp.grants == { };
assert withMCP.programs.aginctus.settings.mcp.servers.docs.transport == "streamable-http";
assert withMCP.programs.aginctus.settings.mcp.servers.docs.credentialRef == "docs-token";
assert withMCP.programs.aginctus.settings.mcp.servers.local.command == "/usr/bin/mcp-local";
assert withMCP.programs.aginctus.settings.mcp.grants.dev.servers.docs == [ "search" ];
assert rejectedMCP unauthenticatedGateway;
assert rejectedMCP remoteWithoutURL;
# The MCP subtree is strict, so a credential-bearing field is an evaluation
# error instead of a field in the generated user configuration.
assert
  !(builtins.tryEval forbiddenCredentialField.programs.aginctus.settings.mcp.servers.docs).success;
pkgs.runCommand "aginctus-home-manager-operator-contract" { } ''
  touch "$out"
''
