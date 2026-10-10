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
  withReconcile = evaluate {
    services.aginctus.reconcile = {
      enable = true;
      workloads = [ "dev" ];
    };
  };

  json = builtins.fromJSON (builtins.readFile base.environment.etc."aginctus/config.json".source);
  imageUnit = base.systemd.services.aginctus-image-herdr-client;
  reconcile = withReconcile.systemd.services.aginctus-reconcile;
  noImagesReconcile = withoutImages.systemd.services.aginctus-reconcile;

  expected = [
    "incus.service"
    "aginctus-image-herdr-client.service"
    "aginctus-image-opencode-workload.service"
  ];
in
assert json.incus.management.network.name == "integration-mgmt";
assert json.workloads.dev.runtime.type == "opencode";
assert imageUnit.serviceConfig.Type == "oneshot";
assert builtins.elem pkgs.incus imageUnit.path;
assert reconcile.requires == expected;
assert reconcile.after == expected;
assert noImagesReconcile.requires == [ "incus.service" ];
assert noImagesReconcile.after == [ "incus.service" ];
pkgs.runCommand "aginctus-nixos-module-evaluation" { } ''
  touch "$out"
''
