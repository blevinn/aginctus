{ self, nixpkgs, system }:
let
  pkgs = import nixpkgs { inherit system; };
  worker = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      self.nixosModules.aginctusTestenvRecovery
      ({ ... }: {
        system.stateVersion = "26.05";
        # This is an evaluation-only disposable worker fixture. The module
        # itself never enables Incus or creates/reconfigures its project.
        virtualisation.incus.enable = true;
        services.aginctusTestenvRecovery.enable = true;
      })
    ];
  };
  cfg = worker.config;
  service = cfg.systemd.services.aginctus-testenv-recovery;
  timer = cfg.systemd.timers.aginctus-testenv-recovery;
in
assert cfg.services.aginctusTestenvRecovery.enable;
assert service.serviceConfig.User == "aginctus-testenv";
assert service.serviceConfig.NoNewPrivileges;
assert service.serviceConfig.PrivateDevices;
assert service.serviceConfig.ProtectSystem == "strict";
assert service.serviceConfig.IPAddressDeny == "any";
assert service.serviceConfig.RestrictAddressFamilies == [ "AF_UNIX" ];
assert service.serviceConfig.LoadCredential == [ "policy-key:/var/lib/aginctus-testenv/worker-policy.key" ];
assert timer.timerConfig.OnBootSec == "2min";
assert timer.timerConfig.OnUnitInactiveSec == "1min";
assert timer.timerConfig.Unit == "aginctus-testenv-recovery.service";
pkgs.runCommand "aginctus-testenv-recovery-worker-policy" { } ''
  mkdir -p "$out"
  echo "opt-in worker unit evaluation PASS; real daemon/KVM not verified" > "$out/result"
''
