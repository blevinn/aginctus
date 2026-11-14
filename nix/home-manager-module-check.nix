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
  withAssets = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.gateway = {
        providers.go = {
          plugin = "opencode-go";
          config.models.coding.source = "openai/gpt-6-luna";
        };
        catalogs.dev.models.coding = {
          provider = "go";
          name = "coding";
        };
      };
    };
  };
  assetCredential = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.gateway.providers.go = {
        plugin = "opencode-go";
        config.api_key = "forbidden";
      };
    };
  };
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
  userPolicyIntent = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.firewall = {
        backend = "firewalld";
        zone = "trusted";
      };
      settings.gateway.egress.network.name = "user-selected";
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
  candidateFingerprint = lib.concatStrings (lib.replicate 64 "a");
  candidateWorkload = {
    id = "dev";
    instanceName = "hm-dev";
    instanceType = "container";
    imageFingerprint = candidateFingerprint;
    profiles = [ "base" ];
    ensureRunning = false;
  };
  withCandidate = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      settings.incus.project = "hm-operator";
      workloadCandidate = {
        deploymentID = "hm-deployment";
        workloads = [ candidateWorkload ];
      };
    };
  };
  duplicateCandidate = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      workloadCandidate = {
        deploymentID = "hm-deployment";
        workloads = [
          candidateWorkload
          (candidateWorkload // { instanceName = "other-instance"; })
        ];
      };
    };
  };
  duplicateProfiles = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      workloadCandidate = {
        deploymentID = "hm-deployment";
        workloads = [
          (
            candidateWorkload
            // {
              profiles = [
                "base"
                "base"
              ];
            }
          )
        ];
      };
    };
  };
  invalidFingerprint = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      workloadCandidate = {
        deploymentID = "hm-deployment";
        workloads = [ (candidateWorkload // { imageFingerprint = "latest"; }) ];
      };
    };
  };
  invalidSecretField = evaluate {
    programs.aginctus = {
      enable = true;
      package = stub;
      workloadCandidate = {
        deploymentID = "hm-deployment";
        workloads = [ (candidateWorkload // { credential = "forbidden"; }) ];
      };
    };
  };
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
assert !(builtins.hasAttr "aginctus/workload-candidate.json" disabled.xdg.configFile);
assert !(builtins.hasAttr "aginctus/workload-candidate.json" enabled.xdg.configFile);
assert withCandidate.programs.aginctus.workloadCandidate.deploymentID == "hm-deployment";
assert withCandidate.programs.aginctus.settings.incus.project == "hm-operator";
assert withCandidate.home.packages == [ stub ];
assert builtins.hasAttr "aginctus/workload-candidate.json" withCandidate.xdg.configFile;
assert lib.hasInfix "aginctus-hm-workload-candidate.json" (
  toString withCandidate.xdg.configFile."aginctus/workload-candidate.json".source
);
assert lib.all (a: a.assertion) withCandidate.assertions;
assert lib.any (a: !a.assertion) duplicateCandidate.assertions;
assert lib.any (a: !a.assertion) duplicateProfiles.assertions;
assert
  !(builtins.tryEval (
    builtins.deepSeq invalidFingerprint.programs.aginctus.workloadCandidate.workloads true
  )).success;
assert
  !(builtins.tryEval (
    builtins.deepSeq invalidSecretField.programs.aginctus.workloadCandidate.workloads true
  )).success;
assert
  withAssets.programs.aginctus.settings.gateway.providers.go.config.models.coding.source
  == "openai/gpt-6-luna";
assert withAssets.programs.aginctus.settings.gateway.catalogs.dev.models.coding.provider == "go";
assert
  !(builtins.tryEval assetCredential.programs.aginctus.settings.gateway.providers.go.config).success;
assert disabled.xdg.configFile == { };
assert !(builtins.hasAttr "aginctus/operator-package.json" disabled.xdg.configFile);
assert disabled.assertions == [ ];
assert enabled.home.packages == [ stub ];
# Even permissive user CLI intent emits only user package/config declarations.
# The fixture has no system policy options: a privileged output fails evaluation.
assert userPolicyIntent.home.packages == [ stub ];
assert
  builtins.attrNames userPolicyIntent.xdg.configFile == [
    "aginctus/config.json"
    "aginctus/operator-package.json"
  ];
assert
  builtins.attrNames userPolicyIntent == [
    "assertions"
    "home"
    "programs"
    "xdg"
  ];
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
# This is an intentionally isolated *builder-local* private accepted-state
# store; the fixture is pure Nix output. The actual Go parser and read-only
# comparison are exercised rather than trusting only a Nix attribute shape.
pkgs.runCommand "aginctus-home-manager-operator-contract"
  {
    nativeBuildInputs = [
      pkgs.jq
      self.packages.${system}.state-local
    ];
  }
  ''
    set -eu
    candidate="${withCandidate.xdg.configFile."aginctus/workload-candidate.json".source}"
    test -f "$candidate"
    jq --arg sha "${candidateFingerprint}" -e '
      .apiVersion == "aginctus.dev/accepted/v1alpha1"
      and .kind == "WorkloadDeployment"
      and .selection.apiVersion == "aginctus.dev/accepted/v1alpha1"
      and .selection.kind == "SelectionManifest"
      and .selection.deploymentID == "hm-deployment"
      and .selection.incusProject == "hm-operator"
      and .selection.selectedWorkloads == ["dev"]
      and (.workloads | length) == 1
      and .workloads[0].id == "dev"
      and .workloads[0].instanceName == "hm-dev"
      and .workloads[0].instanceType == "container"
      and .workloads[0].imageFingerprint == $sha
      and .workloads[0].profiles == ["base"]
      and .workloads[0].ensureRunning == false
    ' "$candidate" > /dev/null

    # The candidate is in the read-only Nix store. Nothing is accepted,
    # reconciled, or written to Incus by this evaluation/check.
    aginctus-state-local init --state-dir "$TMPDIR/local" > /dev/null
    aginctus-state-local status --state-dir "$TMPDIR/local" \
      --deployment hm-deployment --project hm-operator > "$TMPDIR/status.json"
    jq -e '.hasAccepted == false' "$TMPDIR/status.json" > /dev/null
    aginctus-state-local diff --state-dir "$TMPDIR/local" \
      --deployment hm-deployment --project hm-operator \
      --candidate "$candidate" > "$TMPDIR/diff.json"
    jq -e '
      .hasAccepted == false
      and .changed == true
      and .deploymentID == "hm-deployment"
      and .incusProject == "hm-operator"
      and .changes.newlySelected == ["dev"]
    ' "$TMPDIR/diff.json" > /dev/null
    test ! -e "$TMPDIR/local/active.json"

    if aginctus-state-local diff --state-dir "$TMPDIR/local" \
      --deployment other --project hm-operator \
      --candidate "$candidate" > /dev/null 2>&1; then
      echo "candidate bypassed explicit deployment identity" >&2
      exit 1
    fi
    if aginctus-state-local diff --state-dir "$TMPDIR/local" \
      --deployment hm-deployment --project other \
      --candidate "$candidate" > /dev/null 2>&1; then
      echo "candidate bypassed explicit Incus project identity" >&2
      exit 1
    fi

    # No explicit --confirm / expected revision, so acceptance must fail and
    # leave active revision absent. Home Manager never invokes this command.
    if aginctus-state-local accept --state-dir "$TMPDIR/local" \
      --deployment hm-deployment --project hm-operator \
      --candidate "$candidate" > /dev/null 2>&1; then
      echo "accepted without explicit CAS confirmation" >&2
      exit 1
    fi
    test ! -e "$TMPDIR/local/active.json"
    touch "$out"
  ''
