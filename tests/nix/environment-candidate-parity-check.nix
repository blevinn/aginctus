{
  self,
  nixpkgs,
  system,
}:
let
  inherit (nixpkgs) lib;
  pkgs = import nixpkgs { inherit system; };
  stub = pkgs.writeShellScriptBin "aginctus" "exit 1";
  nixos =
    candidate:
    let
      config =
        (lib.nixosSystem {
          inherit system;
          modules = [
            self.nixosModules.default
            {
              system.stateVersion = "26.05";
              services.aginctus = {
                enable = true;
                package = stub;
                images.herdrClient.enable = false;
                images.opencodeWorkload.enable = false;
                reconcile.enable = false;
                settings.incus.project = "ordinary-system";
                environmentCandidate = candidate;
              };
            }
          ];
        }).config;
    in
    {
      inherit (config) assertions;
      options = config.services.aginctus.environmentCandidate;
      source = config.environment.etc."aginctus/environment-candidate.json".source;
    };
  home =
    candidate:
    let
      config =
        (lib.evalModules {
          specialArgs = { inherit pkgs; };
          modules = [
            self.homeManagerModules.default
            {
              options = {
                home.packages = lib.mkOption {
                  type = lib.types.listOf lib.types.package;
                  default = [ ];
                };
                xdg.configFile = lib.mkOption {
                  type = lib.types.attrsOf lib.types.anything;
                  default = { };
                };
                assertions = lib.mkOption {
                  type = lib.types.listOf lib.types.anything;
                  default = [ ];
                };
              };
              config.programs.aginctus = {
                enable = true;
                package = stub;
                settings.incus.project = "ordinary-user";
                environmentCandidate = candidate;
              };
            }
          ];
        }).config;
    in
    {
      inherit (config) assertions;
      options = config.programs.aginctus.environmentCandidate;
      source = config.xdg.configFile."aginctus/environment-candidate.json".source;
    };
  evaluate =
    module:
    builtins.tryEval (
      builtins.deepSeq module.options (
        lib.all (
          a: a.assertion || !(lib.hasInfix ".aginctus.environmentCandidate" a.message)
        ) module.assertions
      )
    );
  valid =
    module:
    let
      v = evaluate module;
    in
    v.success && v.value;
  sha = s: lib.concatStrings (lib.replicate 64 s);
  oldWorkload = {
    id = "legacy";
    instanceName = "existing-workload";
    instanceType = "container";
    imageFingerprint = sha "b";
    profiles = [
      "second"
      "first"
    ];
    ensureRunning = false;
  };
  workload = oldWorkload // {
    id = "agent";
    instanceName = "native-agent";
    instanceType = "virtual-machine";
    profiles = [ ];
    ensureRunning = true;
    native = {
      runtime = "opencode";
      storagePool = "workload-pool";
      managementNetwork = "approved-mgmt";
    };
  };
  base = {
    id = "7c82e6d1-1212-4123-8123-0123456789ab";
    name = "development";
    serverId = "sha256:${sha "a"}";
    project = "candidate-project";
    workloads = [ oldWorkload ];
  };
  postgres = {
    registry = "registry.example.org:5443";
    repository = "mirror/postgres";
    architecture = "arm64";
    variant = "v8";
    manifestDigest = "sha256:${sha "c"}";
    configDigest = "sha256:${sha "d"}";
    incusFingerprint = sha "e";
    alias = "pg-pinned";
  };
  full = base // {
    workloads = [
      oldWorkload
      workload
    ];
    herdr = {
      instanceName = "herdr-client";
      storagePool = "herdr-pool";
      managementNetwork = "approved-mgmt";
      imageFingerprint = sha "f";
      allowStateBootstrap = false;
    };
    gateway = {
      managementNetwork = "approved-mgmt";
      egressNetwork = "approved-egress";
      egressIPv4 = "10.42.40.25/24";
      rootPool = "gateway-pool";
      databasePool = "database-pool";
      inherit postgres;
      litellm = postgres // {
        registry = "ghcr.io";
        repository = "mirror/litellm";
        alias = "llm-pinned";
        variant = "";
      };
      credentialVersion = "revision-3";
      ensureRunning = true;
    };
  };
  change = lib.recursiveUpdate full;
  invalid = [
    (change { workloads = [ (workload // { native.storagePool = "../host"; }) ]; })
    (change {
      workloads = [ (workload // { native = { inherit (workload.native) runtime storagePool; }; }) ];
    })
    (change { workloads = [ (lib.recursiveUpdate workload { native.workspace = "/srv/host"; }) ]; })
    (full // { herdr = removeAttrs full.herdr [ "allowStateBootstrap" ]; })
    (change { herdr.instanceName = workload.instanceName; })
    (change { herdr.instanceName = "ag-7c82e6d11212412381230123456789ac-pg"; })
    (change { gateway.password = "forbidden-field"; })
    (change { gateway.postgres.registry = "https://registry.example.org"; })
    (change { gateway.postgres.repository = "postgres:latest"; })
    (change { gateway.postgres.manifestDigest = "latest"; })
    (change { gateway.litellm.alias = postgres.alias; })
    (change { gateway.egressNetwork = full.gateway.managementNetwork; })
    (change { gateway.egressNetwork = ""; })
    (change { gateway.egressIPv4 = "10.42.40.024/24"; })
    (change { gateway.egressIPv4 = "10.42.40.255/24"; })
    (change { gateway.egressIPv4 = "10.42.40.0/24"; })
    (change { gateway.egressIPv4 = "127.0.0.2/24"; })
    (change { gateway.egressIPv4 = "0.1.0.1/16"; })
    (change { gateway.egressIPv4 = "240.1.0.1/16"; })
  ];
  noEgress = change {
    gateway = {
      egressNetwork = "";
      egressIPv4 = "";
    };
  };
  fixture =
    candidate:
    let
      n = nixos candidate;
      h = home candidate;
    in
    assert valid n && valid h;
    {
      nixos = n.source;
      home = h.source;
    };
  manifest = pkgs.writeText "environment-candidate-manifest.json" (
    builtins.toJSON {
      old = fixture base;
      native = fixture full;
      noEgress = fixture noEgress;
    }
  );
in
assert lib.all (c: !(valid (nixos c)) && !(valid (home c))) invalid;
self.packages.${system}.aginctus.overrideAttrs (_: {
  pname = "aginctus-environment-candidate-parity";
  doCheck = true;
  checkPhase = ''
    runHook preCheck
    export AGINCTUS_ENVIRONMENT_CANDIDATE_MANIFEST=${manifest}
    go test ./internal/orchestration/environment -run '^TestNixEnvironmentCandidateParity$' -count=1 -v
    runHook postCheck
  '';
})
