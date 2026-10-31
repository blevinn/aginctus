{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  lib = nixpkgs.lib;
  evaluate =
    extra:
    (lib.nixosSystem {
      inherit system;
      modules = [
        self.nixosModules.aginctusTestenvWorker
        {
          system.stateVersion = "26.05";
          boot.loader.grub.enable = false;
          fileSystems."/" = {
            device = "/dev/vda";
            fsType = "ext4";
          };
          virtualisation.incus.enable = true;
          networking.nftables.enable = true;
          services.aginctusTestenvWorker = {
            enable = true;
            dedicatedDisposableHost = true;
            imageFingerprint = lib.concatStrings (lib.replicate 64 "a");
            poolConfigFingerprint = lib.concatStrings (lib.replicate 64 "b");
          };
        }
        extra
      ];
    }).config;
  cfg = evaluate { };
  rejected = extra: lib.any (a: !a.assertion) (evaluate extra).assertions;
  disabled = evaluate { services.aginctusTestenvWorker.enable = lib.mkForce false; };
  unit = cfg.systemd.services.aginctus-testenv-preflight;
in
assert system != "x86_64-linux" || lib.all (a: a.assertion) cfg.assertions;
assert rejected { services.aginctusTestenvWorker.dedicatedDisposableHost = lib.mkForce false; };
assert rejected { services.aginctusTestenvWorker.imageFingerprint = lib.mkForce "mutable-alias"; };
assert rejected { services.aginctusTestenvWorker.poolConfigFingerprint = lib.mkForce ""; };
assert rejected { virtualisation.incus.enable = lib.mkForce false; };
assert rejected {
  imports = [ self.nixosModules.aginctus ];
  services.aginctus.enable = true;
};
assert !(disabled.systemd.services ? aginctus-testenv-preflight);
assert !(disabled.systemd.services ? aginctus-testenv-recovery);
assert cfg.services.aginctusTestenvRecovery.enable;
assert unit.wantedBy == [ ];
assert unit.serviceConfig.User == "aginctus-testenv";
assert unit.serviceConfig.RestrictAddressFamilies == [ "AF_UNIX" ];
assert unit.serviceConfig.IPAddressDeny == "any";
assert unit.serviceConfig.ProtectSystem == "strict";
assert
  unit.serviceConfig.LoadCredential == [ "policy-key:/var/lib/aginctus-testenv/worker-policy.key" ];
assert
  !(builtins.elem "aginctus-testenv-preflight.service" cfg.systemd.services.aginctus-testenv-recovery.requires);
pkgs.runCommand "aginctus-testenv-installed-worker-policy" { } ''
  mkdir -p "$out"
  touch "$out/evaluation-only-no-runtime-proof"
''
