{ self }:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  inherit (lib)
    mkEnableOption
    mkIf
    mkOption
    types
    ;

  cfg = config.programs.aginctus;
  jsonFormat = pkgs.formats.json { };
  knownSettingsOptions = import ./settings-options.nix { inherit lib jsonFormat; };
  # Render the MCP subtree in the shape the Aginctus Go schema accepts.
  mcpSettings = import ./mcp-settings.nix { inherit lib; };
  # Export the declared user CLI package, never credentials or environment.
  operatorPackageManifest = jsonFormat.generate "aginctus-user-package.json" {
    schema = 1;
    authority = "user";
    package = toString cfg.package;
  };

  # Both dotted keys and nested objects are possible in the freeform JSON
  # layer. Reject runtime credential paths before writing the Nix store.
  configuredSecretPaths = import ./runtime-secret-paths.nix { inherit lib; } cfg.settings;
  # A narrowly typed, pinned, secret-free workload candidate.  The module
  # generates candidate JSON only; it never initializes or promotes accepted
  # state, connects to Incus, or installs a user reconciler.
  candidateName = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]{0,99}";
  candidateFingerprint = types.strMatching "[0-9a-f]{64}";
  candidateWorkload = types.submodule {
    options = {
      id = mkOption {
        type = candidateName;
        description = "Explicit selected workload identifier.";
      };
      instanceName = mkOption {
        type = candidateName;
        description = "Pinned Incus instance name, not an ownership claim.";
      };
      instanceType = mkOption {
        type = types.enum [ "container" "virtual-machine" ];
        description = "Pinned workload type.";
      };
      imageFingerprint = mkOption {
        type = candidateFingerprint;
        description = "Immutable lowercase SHA256 image fingerprint (not an alias).";
      };
      profiles = mkOption {
        type = types.listOf candidateName;
        description = "Explicit ordered Incus profile names, possibly empty.";
      };
      ensureRunning = mkOption {
        type = types.bool;
        description = "Candidate running-state intent; never activated by HM.";
      };
    };
  };
  candidateWorkloads =
    if cfg.workloadCandidate == null then [ ] else cfg.workloadCandidate.workloads;
  candidateIDs = map (w: w.id) candidateWorkloads;
  candidateInstanceNames = map (w: w.instanceName) candidateWorkloads;
  unique = xs: builtins.length xs == builtins.length (lib.unique xs);
  candidateValid =
    builtins.length candidateWorkloads <= 256
    && unique candidateIDs
    && unique candidateInstanceNames
    && lib.all (w: builtins.length w.profiles <= 32 && unique w.profiles) candidateWorkloads;
  # Project identity is sourced from the SAME rendered Home Manager settings.
  # It is not a caller-selected ambient Incus default or cross-principal merge.
  workloadCandidateSpec = {
    apiVersion = "aginctus.dev/accepted/v1alpha1";
    kind = "WorkloadDeployment";
    selection = {
      apiVersion = "aginctus.dev/accepted/v1alpha1";
      kind = "SelectionManifest";
      deploymentID = cfg.workloadCandidate.deploymentID;
      incusProject = cfg.settings.incus.project;
      selectedWorkloads = candidateIDs;
    };
    workloads = map (w: {
      inherit (w) id instanceName instanceType imageFingerprint profiles ensureRunning;
    }) candidateWorkloads;
  };
in
{
  options.programs.aginctus = {
    enable = mkEnableOption "Aginctus operator CLI and nonsecret user configuration";

    workloadCandidate = mkOption {
      type = types.nullOr (types.submodule {
        options = {
          deploymentID = mkOption {
            type = candidateName;
            description = "Explicit deployment identity for offline accepted-state comparison.";
          };
          workloads = mkOption {
            type = types.listOf candidateWorkload;
            description = "Pinned, nonsecret workload-only candidate. Omission means no candidate.";
          };
        };
      });
      default = null;
      description = ''
        Optional nonsecret, strictly typed workload-only candidate rendered
        to XDG config. Does not promote accepted state or reconcile Incus.
        Use aginctus-state-local with an explicitly initialized same-UID
        private store for experimental manual comparison/acceptance.
      '';
    };

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.aginctus;
      description = "Aginctus CLI installed in the operator's Home Manager profile.";
    };

    settings = mkOption {
      type = types.submodule {
        freeformType = jsonFormat.type;
        options = knownSettingsOptions;
      };
      default = { };
      description = ''
        Nonsecret Aginctus configuration written to the user's XDG config
        directory. Runtime gateway credentials are forbidden here; they
        must remain in protected state outside the Nix store.
      '';
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = configuredSecretPaths == [ ];
        message =
          "programs.aginctus.settings cannot contain runtime gateway secrets: "
          + lib.concatStringsSep ", " configuredSecretPaths;
      }
    ]
    ++ mcpSettings.assertions "programs.aginctus" cfg.settings.mcp
    ++ lib.optional (cfg.workloadCandidate != null) {
      assertion = candidateValid;
      message = "programs.aginctus.workloadCandidate must select at most 256 unique workload IDs and instance names, with at most 32 unique profiles per workload";
    };

    home.packages = [ cfg.package ];
    xdg.configFile = {
      "aginctus/operator-package.json".source = operatorPackageManifest;
      "aginctus/config.json".source = jsonFormat.generate "aginctus-user-config.json" (
        mcpSettings.renderSettings cfg.settings
      );
    } // lib.optionalAttrs (cfg.workloadCandidate != null) {
      "aginctus/workload-candidate.json".source = jsonFormat.generate
        "aginctus-hm-workload-candidate.json" workloadCandidateSpec;
    };
  };
}
