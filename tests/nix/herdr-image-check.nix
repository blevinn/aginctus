{
  self,
  nixpkgs,
  system,
}:
let
  inherit (nixpkgs) lib;
  pkgs = import nixpkgs { inherit system; };
  stub = pkgs.writeShellScriptBin "aginctus" "exit 0";
  customImage = pkgs.runCommand "custom-herdr-image-fixture" { } "mkdir -p $out";
  evaluate =
    extra:
    (lib.nixosSystem {
      inherit system;
      modules = [
        self.nixosModules.default
        {
          system.stateVersion = "26.05";
          services.aginctus = {
            enable = true;
            package = stub;
            firewall.enable = false;
            images.opencodeWorkload.enable = false;
            reconcile.enable = true;
          };
        }
        extra
      ];
    }).config;
  default = evaluate { };
  custom = evaluate {
    services.aginctus = {
      images.herdrClient.source = customImage;
      settings.infrastructure.herdr.image.alias = "custom-herdr";
    };
  };
  existing = evaluate {
    services.aginctus = {
      images.herdrClient.enable = false;
      # Disabled imports must not force an image build/source dependency.
      images.herdrClient.source = throw "disabled Herdr image source was forced";
      settings.infrastructure.herdr.image.alias = "preloaded-herdr";
    };
  };
  aliasOnly = evaluate {
    services.aginctus.images.herdrClient.alias = "import-only";
  };
  unit = cfg: cfg.systemd.services.aginctus-image-herdr-client;
  reconcile = cfg: cfg.systemd.services.aginctus-reconcile;
  guest =
    (import ../../plugins/herdr/images/client.nix {
      inherit nixpkgs system pkgs;
      herdrPackage = stub;
      herdrClientCommand = stub;
      sshClientIdentityHelper = stub;
      sshHostTrustHelper = stub;
      sshMachineConfigHelper = stub;
      guestStateBindings = import ../../nix/guest-state-bindings.nix;
    }).guest.config;
  mounts = map (mount: mount.where) guest.systemd.mounts;
in
assert default.services.aginctus.images.herdrClient.source == self.packages.${system}.herdr-client;
assert default.services.aginctus.images.herdrClient.alias == "aginctus-herdr-client";
assert lib.hasInfix "images update aginctus-herdr-client" (unit default).serviceConfig.ExecStart;
assert lib.elem "aginctus-image-herdr-client.service" (reconcile default).requires;
assert custom.services.aginctus.images.herdrClient.alias == "custom-herdr";
assert lib.hasInfix
  (builtins.unsafeDiscardStringContext "images update custom-herdr ${customImage}")
  (unit custom).serviceConfig.ExecStart;
assert lib.elem customImage (reconcile custom).restartTriggers;
assert !(existing.systemd.services ? aginctus-image-herdr-client);
assert !(lib.elem "aginctus-image-herdr-client.service" (reconcile existing).requires);
assert builtins.length (reconcile existing).restartTriggers == 2;
assert existing.services.aginctus.settings.infrastructure.herdr.image.alias == "preloaded-herdr";
# Current behavior: an import-only override does not rewrite the runtime pointer.
assert aliasOnly.services.aginctus.images.herdrClient.alias == "import-only";
assert
  aliasOnly.services.aginctus.settings.infrastructure.herdr.image.alias == "aginctus-herdr-client";
assert lib.elem "/var/lib/herdr" mounts;
assert lib.elem "/var/lib/aginctus/ssh" mounts;
assert
  guest.systemd.services.herdr.requires == [
    "var-lib-herdr.mount"
    "var-lib-aginctus-ssh.mount"
  ];
assert guest.systemd.services.herdr.serviceConfig.ExecStart == "${stub}/bin/herdr server";
assert lib.elem "XDG_STATE_HOME=/var/lib/herdr"
  guest.systemd.services.herdr.serviceConfig.Environment;
assert guest.environment.variables.XDG_CONFIG_HOME == "/var/lib/herdr/config";
assert lib.hasInfix "Include /var/lib/aginctus/ssh/machines/*.conf" guest.programs.ssh.extraConfig;
pkgs.runCommand "aginctus-herdr-image-selection-check" { } ''
  touch "$out"
''
