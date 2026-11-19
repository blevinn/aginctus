{ lib }:
let
  inherit (lib) mkOption types;
  resourceName = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]{0,99}";
  environmentName = types.strMatching "[a-z][a-z0-9-]{0,62}";
  environmentUUID = types.strMatching "[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";
  # Syntactic subset for a *declared* stable server identifier. Runtime
  # identity comes from the authenticated Incus connection, never Nix alone.
  serverID = types.strMatching "[A-Za-z0-9][A-Za-z0-9._:-]{0,255}";
  fingerprint = types.strMatching "[0-9a-f]{64}";
  workloadType = types.submodule {
    options = {
      id = mkOption { type = resourceName; };
      instanceName = mkOption { type = resourceName; };
      instanceType = mkOption { type = types.enum [ "container" "virtual-machine" ]; };
      imageFingerprint = mkOption { type = fingerprint; };
      profiles = mkOption { type = types.listOf resourceName; };
      ensureRunning = mkOption { type = types.bool; };
    };
  };
  candidateType = types.nullOr (types.submodule {
    options = {
      id = mkOption {
        type = environmentUUID;
        description = "Immutable environment UUID, not a permission grant.";
      };
      name = mkOption {
        type = environmentName;
        description = "Renameable environment display name.";
      };
      serverId = mkOption {
        type = serverID;
        description = "Declared server identity; verify against actual Incus connection.";
      };
      project = mkOption {
        type = resourceName;
        description = "Explicit non-default Incus project.";
      };
      workloads = mkOption {
        type = types.listOf workloadType;
        description = "Pinned nonsecret selected workload intent.";
      };
    };
  });
  unique = xs: builtins.length xs == builtins.length (lib.unique xs);
in
{
  type = candidateType;
  valid = candidate:
    candidate.project != "default"
    && builtins.length candidate.workloads <= 256
    && unique (map (w: w.id) candidate.workloads)
    && unique (map (w: w.instanceName) candidate.workloads)
    && lib.all (w: builtins.length w.profiles <= 32 && unique w.profiles) candidate.workloads;
  render = candidate: {
    apiVersion = "aginctus.dev/environment/v1alpha1";
    kind = "EnvironmentCandidate";
    environment = {
      inherit (candidate) id name;
      binding = {
        serverId = candidate.serverId;
        project = candidate.project;
      };
    };
    selectedWorkloads = map (w: w.id) candidate.workloads;
    workloads = map (w: {
      inherit (w) id instanceName instanceType imageFingerprint profiles ensureRunning;
    }) candidate.workloads;
  };
}
