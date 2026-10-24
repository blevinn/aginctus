# Opt-in independent testenv recovery timer. Does not launch VMs, grant
# Incus privileges, configure pilot networks or expose MCP mutation.
{ self }:
{ config, lib, pkgs, ... }:
let
  inherit (lib) mkEnableOption mkIf mkOption types;
  cfg = config.services.aginctusTestenvRecovery;
  stateRoot = "/var/lib/aginctus-testenv/runs";
  worker = "aginctus-testenv";
in
{
  options.services.aginctusTestenvRecovery = {
    enable = mkEnableOption "a dedicated independent Aginctus testenv VM recovery watchdog";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.testenv-recovery;
      description = "Pinned standalone Aginctus testenv recovery executable.";
    };

    delegatedCgroupRoot = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "/sys/fs/cgroup/system.slice/aginctus-testenv-worker.service";
      description = ''
        Absolute existing cgroup v2 subtree delegated to the dedicated
        unprivileged testenv worker. The module does not create the
        delegation, grant sudo, or alter host cgroup ancestors.
        Activation without a specified subtree is forbidden.
      '';
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion =
          cfg.delegatedCgroupRoot != null
          && lib.hasPrefix "/sys/fs/cgroup/" cfg.delegatedCgroupRoot
          && !(lib.hasInfix ".." cfg.delegatedCgroupRoot);
        message = "services.aginctusTestenvRecovery requires an explicit delegated cgroup v2 root under /sys/fs/cgroup.";
      }
      {
        assertion = pkgs.stdenv.hostPlatform.isLinux;
        message = "Aginctus testenv recovery requires Linux cgroup v2.";
      }
    ];

    users.groups.${worker} = { };
    users.users.${worker} = {
      isSystemUser = true;
      group = worker;
      home = "/var/lib/aginctus-testenv";
      createHome = false;
      description = "Unprivileged Aginctus disposable VM watchdog";
      extraGroups = [ ];
    };

    systemd.services.aginctus-testenv-recovery = {
      description = "Recover expired or abandoned Aginctus disposable VM cgroups";
      serviceConfig = {
        Type = "oneshot";
        User = worker;
        Group = worker;
        UMask = "0077";
        StateDirectory = "aginctus-testenv/runs";
        StateDirectoryMode = "0700";
        ExecStart = "${cfg.package}/bin/aginctus-testenv-recover";
        TimeoutStartSec = "55s";
        NoNewPrivileges = true;
        CapabilityBoundingSet = "";
        AmbientCapabilities = "";
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        ProtectKernelModules = true;
        ProtectKernelLogs = true;
        ProtectKernelTunables = true;
        # The service must modify ONLY its previously delegated subtree.
        # ProtectControlGroups would also block cgroup.kill.
        ProtectControlGroups = false;
        ReadWritePaths = [ (toString cfg.delegatedCgroupRoot) ];
      };
      environment = {
        AGINCTUS_TESTENV_RUN_ROOT = stateRoot;
        AGINCTUS_TESTENV_CGROUP_ROOT = toString cfg.delegatedCgroupRoot;
      };
    };

    systemd.timers.aginctus-testenv-recovery = {
      description = "Independent Aginctus testenv VM recovery cadence";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = "2min";
        OnUnitInactiveSec = "1min";
        AccuracySec = "15s";
        Unit = "aginctus-testenv-recovery.service";
      };
    };
  };
}
