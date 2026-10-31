# Installed observation gate; provisioning and isolation require operator approval.
{ self }:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.aginctusTestenvWorker;
  marker = pkgs.writeText "aginctus-disposable-worker-marker" "aginctus-dedicated-disposable-worker-v1\n";
in
{
  imports = [ self.nixosModules.aginctusTestenvRecovery ];
  options.services.aginctusTestenvWorker = {
    enable = lib.mkEnableOption "installed dedicated testenv worker preflight and recovery";
    dedicatedDisposableHost = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Operator acknowledgement of dedicated disposable host isolation, not runtime proof.";
    };
    imageFingerprint = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Reviewed imported x86_64 VM image fingerprint in the restricted project.";
    };
    poolConfigFingerprint = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "Operator-reviewed SHA256 of the dedicated pool configuration from read-only inspection.";
    };
  };
  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.dedicatedDisposableHost;
        message = "The installed testenv worker requires explicit dedicated disposable host acknowledgement.";
      }
      {
        assertion = pkgs.stdenv.hostPlatform.system == "x86_64-linux";
        message = "The installed testenv worker image admission currently supports x86_64-linux only.";
      }
      {
        assertion = builtins.match "[0-9a-f]{64}" cfg.imageFingerprint != null;
        message = "The installed testenv worker requires an immutable imported VM image fingerprint.";
      }
      {
        assertion = builtins.match "[0-9a-f]{64}" cfg.poolConfigFingerprint != null;
        message = "The installed testenv worker requires a reviewed pool configuration fingerprint.";
      }
    ];
    services.aginctusTestenvRecovery.enable = true;
    # C copies a regular nonsecret marker only when absent; it never replaces
    # pre-existing operator state. The executable checks its ownership/content.
    systemd.tmpfiles.rules = [
      "d /etc/aginctus-testenv 0755 root root -"
      "C /etc/aginctus-testenv/disposable-worker 0444 root root - ${marker}"
    ];
    systemd.services.aginctus-testenv-preflight = {
      description = "Read-only installed testenv worker image and storage admission";
      after = [
        "incus.socket"
        "systemd-tmpfiles-setup.service"
      ];
      wants = [ "incus.socket" ];
      serviceConfig = {
        Type = "oneshot";
        User = "aginctus-testenv";
        Group = "aginctus-testenv";
        SupplementaryGroups = [ "incus-admin" ];
        ExecStart = "${
          self.packages.${pkgs.stdenv.hostPlatform.system}.testenv-worker-preflight
        }/bin/aginctus-testenv-worker-preflight";
        LoadCredential = [ "policy-key:/var/lib/aginctus-testenv/worker-policy.key" ];
        Environment = [
          "AGINCTUS_TESTENV_WORKER_SOCKET=/var/lib/incus/unix.socket"
          "AGINCTUS_TESTENV_POLICY_KEY_FILE=/run/credentials/aginctus-testenv-preflight.service/policy-key"
          "AGINCTUS_TESTENV_IMAGE_FINGERPRINT=${cfg.imageFingerprint}"
          "AGINCTUS_TESTENV_POOL_CONFIG_FINGERPRINT=${cfg.poolConfigFingerprint}"
        ];
        TimeoutStartSec = "45s";
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
        UMask = "0077";
      };
    };
  };
}
