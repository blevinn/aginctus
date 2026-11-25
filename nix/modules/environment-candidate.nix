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
  attachmentName = types.strMatching "[A-Za-z0-9][A-Za-z0-9_-]{0,62}";
  required = type: mkOption { inherit type; };
  optional =
    type:
    mkOption {
      type = types.nullOr type;
      default = null;
    };
  nativeWorkloadType = types.submodule {
    options = {
      runtime = required resourceName;
      storagePool = required resourceName;
      managementNetwork = required resourceName;
    };
  };
  herdrType = types.submodule {
    options = {
      instanceName = required environmentName;
      imageFingerprint = required fingerprint;
      storagePool = required attachmentName;
      managementNetwork = required attachmentName;
      allowStateBootstrap = required types.bool;
    };
  };
  # Nix accepts ordinary DNS registry hosts (optionally with a numeric port)
  # and OCI repository paths; no URL, tag, userinfo, or credential source.
  registryLabel = "[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?";
  repositoryPart = "[a-z0-9]+(([._]|__|-+)[a-z0-9]+)*";
  platformPart = "[a-z0-9][a-z0-9-]{0,31}";
  digest = types.strMatching "sha256:[0-9a-f]{64}";
  gatewayImageType = types.submodule {
    options = {
      registry = required (types.strMatching "${registryLabel}(\\.${registryLabel})*(:[1-9][0-9]{0,4})?");
      repository = required (types.strMatching "${repositoryPart}(/${repositoryPart})*");
      architecture = required (types.strMatching platformPart);
      variant = required (types.strMatching "(${platformPart})?");
      manifestDigest = required digest;
      configDigest = required digest;
      incusFingerprint = required fingerprint;
      alias = required attachmentName;
    };
  };
  gatewayType = types.submodule {
    options = {
      managementNetwork = required attachmentName;
      egressNetwork = required (types.strMatching "([A-Za-z0-9][A-Za-z0-9_-]{0,62})?");
      egressIPv4 = required types.str;
      rootPool = required attachmentName;
      databasePool = required attachmentName;
      postgres = required gatewayImageType;
      litellm = required gatewayImageType;
      credentialVersion = required (types.strMatching "[A-Za-z0-9][A-Za-z0-9._-]{0,127}");
      ensureRunning = required types.bool;
    };
  };
  workloadType = types.submodule {
    options = {
      id = mkOption { type = resourceName; };
      instanceName = mkOption { type = resourceName; };
      instanceType = mkOption {
        type = types.enum [
          "container"
          "virtual-machine"
        ];
      };
      imageFingerprint = mkOption { type = fingerprint; };
      profiles = mkOption { type = types.listOf resourceName; };
      ensureRunning = mkOption { type = types.bool; };
      native = optional nativeWorkloadType;
    };
  };
  candidateType = types.nullOr (
    types.submodule {
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
        herdr = optional herdrType;
        gateway = optional gatewayType;
      };
    }
  );
  unique = xs: builtins.length xs == builtins.length (lib.unique xs);
  # Incus gateway names use a compact canonical UUID. Reserve this namespace
  # even when the candidate doesn't declare a gateway, as in Go validation.
  reservedInstance =
    name: builtins.match "ag-[0-9a-f]{12}[1-8][0-9a-f]{3}[89ab][0-9a-f]{15}-(pg|llm)" name != null;
  validGatewayImage =
    image:
    let
      port = builtins.match ".*:([0-9]+)" image.registry;
    in
    builtins.stringLength image.registry <= 253
    && builtins.stringLength image.repository <= 255
    && (port == null || builtins.fromJSON (builtins.head port) <= 65535);
  validEgressIPv4 =
    value:
    let
      octet = "(0|[1-9][0-9]{0,2})";
      parsed = builtins.match "${octet}\\.${octet}\\.${octet}\\.${octet}/(1[6-9]|2[0-9]|30)" value;
      numbers = map builtins.fromJSON parsed;
      at = builtins.elemAt numbers;
      first = at 0;
      second = at 1;
      lowBits = (at 2) * 256 + (at 3);
      blockSize = builtins.elemAt [
        65536
        32768
        16384
        8192
        4096
        2048
        1024
        512
        256
        128
        64
        32
        16
        8
        4
      ] ((at 4) - 16);
      host = lib.mod lowBits blockSize;
    in
    parsed != null
    && lib.all (n: n <= 255) numbers
    && first > 0
    && first != 127
    && !(first == 169 && second == 254)
    && !(first >= 224 && first <= 239)
    && first < 240
    && host != 0
    && host != blockSize - 1;
  validGateway =
    g:
    g.postgres.alias != g.litellm.alias
    && validGatewayImage g.postgres
    && validGatewayImage g.litellm
    && (
      if g.egressNetwork == "" then
        g.egressIPv4 == ""
      else
        g.egressNetwork != g.managementNetwork && validEgressIPv4 g.egressIPv4
    );
  renderImage = image: {
    inherit (image)
      registry
      repository
      architecture
      variant
      manifestDigest
      configDigest
      incusFingerprint
      alias
      ;
  };
in
{
  type = candidateType;
  valid =
    candidate:
    candidate.project != "default"
    && builtins.length candidate.workloads <= 256
    && unique (map (w: w.id) candidate.workloads)
    && unique (map (w: w.instanceName) candidate.workloads)
    && lib.all (
      w: !reservedInstance w.instanceName && builtins.length w.profiles <= 32 && unique w.profiles
    ) candidate.workloads
    && (
      candidate.herdr == null
      || (
        !reservedInstance candidate.herdr.instanceName
        && !(builtins.elem candidate.herdr.instanceName (map (w: w.instanceName) candidate.workloads))
      )
    )
    && (candidate.gateway == null || validGateway candidate.gateway);
  render =
    candidate:
    {
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
      workloads = map (
        w:
        {
          inherit (w)
            id
            instanceName
            instanceType
            imageFingerprint
            profiles
            ensureRunning
            ;
        }
        // lib.optionalAttrs (w.native != null) {
          native = { inherit (w.native) runtime storagePool managementNetwork; };
        }
      ) candidate.workloads;
    }
    // lib.optionalAttrs (candidate.herdr != null) {
      herdr = {
        inherit (candidate.herdr)
          instanceName
          imageFingerprint
          storagePool
          managementNetwork
          allowStateBootstrap
          ;
      };
    }
    // lib.optionalAttrs (candidate.gateway != null) {
      gateway = {
        inherit (candidate.gateway)
          managementNetwork
          egressNetwork
          egressIPv4
          rootPool
          databasePool
          credentialVersion
          ensureRunning
          ;
        postgres = renderImage candidate.gateway.postgres;
        litellm = renderImage candidate.gateway.litellm;
      };
    };
}
