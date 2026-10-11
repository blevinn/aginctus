{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  lib = nixpkgs.lib;
  stub = pkgs.writeShellScriptBin "aginctus" "exit 0";

  evaluate =
    extraModule:
    (lib.nixosSystem {
      inherit system;
      modules = [
        self.nixosModules.default
        {
          system.stateVersion = "26.05";
          services.aginctus = {
            enable = true;
            package = stub;
            settings.incus.management.network.name = "integration-mgmt";
          };
        }
        extraModule
      ];
    }).config;

  base = evaluate { };
  withoutImages = evaluate {
    services.aginctus.images.herdrClient.enable = false;
    services.aginctus.images.opencodeWorkload.enable = false;
    services.aginctus.reconcile.enable = true;
  };
  withForbiddenCredentials = evaluate {
    services.aginctus.settings.gateway = {
      postgres.password = "must-not-be-stored";
      master.key = "must-not-be-stored";
      salt.key = "must-not-be-stored";
    };
  };
  invalidAliases = evaluate {
    services.aginctus.images.herdrClient.alias = "invalid alias";
    services.aginctus.images.opencodeWorkload.alias = "invalid alias";
  };
  duplicateAliases = evaluate {
    services.aginctus.images.opencodeWorkload.alias = "aginctus-herdr-client";
  };
  unknownReconcileWorkload = evaluate {
    services.aginctus.reconcile = {
      enable = true;
      workloads = [ "missing" ];
    };
  };
  withReconcile = evaluate {
    services.aginctus.reconcile = {
      enable = true;
      workloads = [ "dev" ];
    };
  };

  settings = base.services.aginctus.settings;
  imageUnit = base.systemd.services.aginctus-image-herdr-client;
  reconcile = withReconcile.systemd.services.aginctus-reconcile;
  noImagesReconcile = withoutImages.systemd.services.aginctus-reconcile;

  # NixOS may report other failed assertions for this deliberately minimal
  # evaluation host. Select only the assertion owned by this module.
  secretAssertions = builtins.filter (
    item:
    !(item.assertion)
    && lib.hasPrefix "services.aginctus.settings cannot contain runtime gateway secrets:" item.message
  ) withForbiddenCredentials.assertions;
  failedAginctusAssertions = cfg:
    builtins.filter (
      item: !(item.assertion) && lib.hasPrefix "services.aginctus." item.message
    ) cfg.assertions;

  defaultPackage =
    (evaluate {
      services.aginctus.package = lib.mkForce self.packages.${system}.aginctus;
    }).services.aginctus.package;

  expected = [
    "incus.service"
    "aginctus-image-herdr-client.service"
    "aginctus-image-opencode-workload.service"
  ];
in
assert settings.incus.management.network.name == "integration-mgmt";
assert defaultPackage == self.packages.${system}.aginctus;
assert builtins.length secretAssertions == 1;
assert lib.hasInfix "gateway.postgres.password" (builtins.head secretAssertions).message;
assert lib.hasInfix "gateway.master.key" (builtins.head secretAssertions).message;
assert lib.hasInfix "gateway.salt.key" (builtins.head secretAssertions).message;
assert settings.workloads.dev.runtime.type == "opencode";
assert builtins.length (failedAginctusAssertions invalidAliases) == 2;
assert builtins.length (failedAginctusAssertions duplicateAliases) == 1;
assert builtins.length (failedAginctusAssertions unknownReconcileWorkload) == 1;
assert lib.hasInfix "missing" (builtins.head (failedAginctusAssertions unknownReconcileWorkload)).message;
assert imageUnit.serviceConfig.Type == "oneshot";
assert builtins.elem pkgs.incus imageUnit.path;
assert reconcile.requires == expected;
assert reconcile.after == expected;
assert noImagesReconcile.requires == [ "incus.service" ];
assert noImagesReconcile.after == [ "incus.service" ];
pkgs.runCommand "aginctus-nixos-module-evaluation" { } ''
  touch "$out"
''
