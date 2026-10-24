{ self, nixpkgs, system }:
let
  systemConfig = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      self.nixosModules.aginctusTestenvRecovery
      {
        system.stateVersion = "26.05";
        services.aginctusTestenvRecovery = {
          enable = true;
          delegatedCgroupRoot = "/sys/fs/cgroup/system.slice/aginctus-testenv-worker.service";
        };
      }
    ];
  };
  cfg = systemConfig.config;
  service = cfg.systemd.services.aginctus-testenv-recovery;
  timer = cfg.systemd.timers.aginctus-testenv-recovery;
  pkgs = nixpkgs.legacyPackages.${system};
in
assert service.serviceConfig.User == "aginctus-testenv";
assert service.serviceConfig.Type == "oneshot";
assert service.serviceConfig.NoNewPrivileges;
assert service.serviceConfig.PrivateDevices;
assert service.serviceConfig.StateDirectoryMode == "0700";
assert service.environment.AGINCTUS_TESTENV_RUN_ROOT == "/var/lib/aginctus-testenv/runs";
assert service.environment.AGINCTUS_TESTENV_CGROUP_ROOT == "/sys/fs/cgroup/system.slice/aginctus-testenv-worker.service";
assert timer.timerConfig.OnBootSec == "2min";
assert timer.timerConfig.OnUnitInactiveSec == "1min";
assert cfg.users.users.aginctus-testenv.isSystemUser;
pkgs.runCommand "aginctus-testenv-recovery-module-check" { } ''
  touch "$out"
''
