# Opt-in recovery scheduler. Enable ONLY on a dedicated, isolated Incus
# worker with no pilot credentials/daemon access. This module does NOT
# initialize Incus, create a storage pool or change any Incus projects.
{ self }:
{ config, lib, pkgs, ... }:
let
  cfg = config.services.aginctusTestenvRecovery;
  types = lib.types;
in
{
  options.services.aginctusTestenvRecovery = {
    enable = lib.mkEnableOption "dedicated testenv Incus TTL recovery worker";

    package = lib.mkOption {
      type = types.package;
      default = self.packages.${pkgs.system}.testenv-sweep;
      description = "Store-pinned one-shot testenv Incus reconciliation binary.";
    };

    storagePool = lib.mkOption {
      type = types.str;
      default = "testenv";
      description = "Dedicated worker-owned Incus storage pool.";
    };

    socketPath = lib.mkOption {
      type = types.str;
      default = "/var/lib/incus/unix.socket";
      description = "Trusted local Incus daemon Unix socket on the dedicated worker.";
    };

    policyKeyFile = lib.mkOption {
      type = types.str;
      default = "/var/lib/aginctus-testenv/worker-policy.key";
      description = ''
        Runtime, root-owned private credential provisioned out of band.
        Must contain exactly 32 bytes and remain stable across reboots.
        Never define this file using a Nix store-backed environment.etc source.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = config.virtualisation.incus.enable;
        message = "Testenv recovery requires a separately provisioned Incus daemon on the dedicated worker.";
      }
      {
        assertion = !(config.services.aginctus.enable or false);
        message = "Do not enable testenv recovery on a host running the Aginctus pilot service.";
      }
      {
        assertion = cfg.storagePool == "testenv";
        message = "Testenv recovery storage pool must be the dedicated testenv pool.";
      }
      {
        assertion = cfg.socketPath == "/var/lib/incus/unix.socket";
        message = "Testenv recovery cannot select an arbitrary Incus socket.";
      }
      {
        assertion = cfg.policyKeyFile == "/var/lib/aginctus-testenv/worker-policy.key";
        message = "Testenv recovery key must be provisioned at the fixed private worker path.";
      }
    ];

    users.groups.aginctus-testenv = { };
    users.users.aginctus-testenv = {
      isSystemUser = true;
      group = "aginctus-testenv";
      extraGroups = [ "incus-admin" ];
      home = "/var/empty";
      createHome = false;
      shell = "${pkgs.shadow}/bin/nologin";
    };

    systemd.services.aginctus-testenv-recovery = {
      description = "Reconcile expired signed disposable Incus test VMs";
      after = [ "incus.socket" ];
      wants = [ "incus.socket" ];
      serviceConfig = {
        Type = "oneshot";
        User = "aginctus-testenv";
        Group = "aginctus-testenv";
        SupplementaryGroups = [ "incus-admin" ];
        ExecStart = "${cfg.package}/bin/aginctus-testenv-sweep";
        LoadCredential = [ "policy-key:${cfg.policyKeyFile}" ];
        Environment = [
          "AGINCTUS_TESTENV_WORKER_SOCKET=${cfg.socketPath}"
          "AGINCTUS_TESTENV_WORKER_POOL=${cfg.storagePool}"
          "AGINCTUS_TESTENV_POLICY_KEY_FILE=/run/credentials/aginctus-testenv-recovery.service/policy-key"
        ];
        TimeoutStartSec = "150s";
        NoNewPrivileges = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ProtectControlGroups = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        RestrictAddressFamilies = [ "AF_UNIX" ];
        IPAddressDeny = "any";
        CapabilityBoundingSet = [ ];
        AmbientCapabilities = [ ];
        LockPersonality = true;
        SystemCallArchitectures = "native";
        UMask = "0077";
      };
    };

    systemd.timers.aginctus-testenv-recovery = {
      description = "Independent testenv Incus TTL recovery schedule";
      wantedBy = [ "timers.target" ];
      timerConfig = {
        OnBootSec = "2min";
        OnUnitInactiveSec = "1min";
        Persistent = true;
        Unit = "aginctus-testenv-recovery.service";
      };
    };
  };
}
