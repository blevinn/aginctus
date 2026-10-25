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
        self.nixosModules.aginctusTestenvRecovery
        ({ ... }: {
          system.stateVersion = "26.05";
          boot.loader.grub.enable = false;
          fileSystems."/" = {
            device = "/dev/vda";
            fsType = "ext4";
          };
          # This is an evaluation-only disposable worker fixture. The module
          # itself never enables Incus or creates/reconfigures its project.
          virtualisation.incus.enable = true;
          networking.nftables.enable = true;
          services.aginctusTestenvRecovery.enable = true;
        })
        extra
      ];
    }).config;
  cfg = evaluate { };
  rejected = extra: lib.any (a: !a.assertion) (evaluate extra).assertions;
  disabled = evaluate { services.aginctusTestenvRecovery.enable = lib.mkForce false; };
  service = cfg.systemd.services.aginctus-testenv-recovery;
  timer = cfg.systemd.timers.aginctus-testenv-recovery;
in
assert lib.all (a: a.assertion) cfg.assertions;
assert rejected { virtualisation.incus.enable = lib.mkForce false; };
assert rejected {
  imports = [ self.nixosModules.aginctus ];
  services.aginctus.enable = true;
};
assert rejected { services.aginctusTestenvRecovery.storagePool = "pilot"; };
assert rejected { services.aginctusTestenvRecovery.socketPath = "/tmp/other.socket"; };
assert rejected { services.aginctusTestenvRecovery.policyKeyFile = "/nix/store/key"; };
assert !(disabled.systemd.services ? aginctus-testenv-recovery);
assert !(disabled.systemd.timers ? aginctus-testenv-recovery);
assert !(disabled.users.users ? aginctus-testenv);
assert cfg.services.aginctusTestenvRecovery.enable;
assert service.serviceConfig.User == "aginctus-testenv";
assert service.serviceConfig.NoNewPrivileges;
assert service.serviceConfig.PrivateDevices;
assert service.serviceConfig.ProtectSystem == "strict";
assert service.serviceConfig.IPAddressDeny == "any";
assert service.serviceConfig.RestrictAddressFamilies == [ "AF_UNIX" ];
assert
  service.serviceConfig.LoadCredential
  == [ "policy-key:/var/lib/aginctus-testenv/worker-policy.key" ];
assert timer.timerConfig.OnBootSec == "2min";
assert timer.timerConfig.OnUnitInactiveSec == "1min";
assert timer.timerConfig.Unit == "aginctus-testenv-recovery.service";
pkgs.runCommand "aginctus-testenv-recovery-worker-policy" { } ''
  mkdir -p "$out"
  echo "opt-in worker unit evaluation PASS; real daemon/KVM not verified" > "$out/result"
''
