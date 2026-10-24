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
  kernelRoutingOnly = evaluate {
    services.aginctus.settings.firewall.egress.allowTailscaleRouting = false;
  };
  withoutImages = evaluate {
    services.aginctus.images.herdrClient.enable = false;
    services.aginctus.images.opencodeWorkload.enable = false;
    services.aginctus.reconcile.enable = true;
  };
  hostOnly = evaluate {
    services.aginctus.images.herdrClient.enable = false;
    services.aginctus.images.opencodeWorkload.enable = false;
    services.aginctus.reconcile.enable = false;
    services.aginctus.firewall.enable = true;
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
  withCustomZone = evaluate {
    networking.nftables.enable = true;
    services.firewalld.enable = true;
    services.aginctus.settings.firewall.zone = "aginctus-vm";
  };
  invalidFirewalldZones =
    map
      (
        zone:
        evaluate {
          services.aginctus.settings.firewall.zone = zone;
        }
      )
      [
        "trusted"
        "aginctus-TooLong"
        "aginctus-abcdefghijkl"
        "../danger"
      ];
  nativeNftHost = evaluate {
    networking.nftables.enable = true;
    networking.firewall.backend = "nftables";
  };
  firewalldHost = evaluate {
    services.firewalld.enable = true;
    networking.nftables.enable = true;
  };
  egressHost = evaluate {
    services.firewalld.enable = true;
    networking.nftables.enable = true;
    services.aginctus.firewall.egress.enable = true;
    services.aginctus.settings.gateway.egress.network.name = "custom-egress";
  };
  withReconcile = evaluate {
    services.aginctus.reconcile = {
      enable = true;
      workloads = [ "dev" ];
    };
  };
  withReconcileGateway = evaluate {
    services.aginctus.reconcile = {
      enable = true;
      gateway = true;
      workloads = [ "dev" ];
    };
  };
  withReconcileGatewayNoEnable = evaluate {
    services.aginctus.reconcile = {
      enable = false;
      gateway = true;
    };
  };

  settings = base.services.aginctus.settings;
  imageUnit = base.systemd.services.aginctus-image-herdr-client;
  reconcile = withReconcile.systemd.services.aginctus-reconcile;
  reconcileGateway = withReconcileGateway.systemd.services.aginctus-reconcile;
  noImagesReconcile = withoutImages.systemd.services.aginctus-reconcile;

  # NixOS may report other failed assertions for this deliberately minimal
  # evaluation host. Select only the assertion owned by this module.
  secretAssertions = builtins.filter (
    item:
    !(item.assertion)
    && lib.hasPrefix "services.aginctus.settings cannot contain runtime gateway secrets:" item.message
  ) withForbiddenCredentials.assertions;
  failedAginctusAssertions =
    cfg:
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

  recorder = pkgs.writeShellScript "reconcile-recorder" ''
    set -eu
    if [ "$1" = --configuration-file ]; then
      test -r "$2"
      shift 2
    fi
    printf '%s\n' "$*" >> "$COMMAND_LOG"
    if [ "$*" = "''${FAIL_COMMAND:-}" ]; then
      exit 23
    fi
  '';
  # Substitute only external executables; execute the module's actual shell flow.
  recordedScript =
    unit:
    pkgs.writeShellScript "recorded-reconcile" (
      builtins.replaceStrings
        [ "${stub}/bin/aginctus" "${pkgs.systemd}/bin/systemctl" ]
        [ "${recorder}" "${recorder}" ]
        unit.script
    );
in
# Operator-owned lifecycle may coexist with this existing minimal NixOS host
# policy mode, but the system must not import image aliases or reconcile guests.
assert hostOnly.services.aginctus.enable;
assert hostOnly.services.aginctus.firewall.enable;
assert builtins.hasAttr "aginctus-firewall-reconcile" hostOnly.systemd.services;
assert builtins.hasAttr "aginctus-firewall-reconcile" hostOnly.systemd.timers;
assert !(builtins.hasAttr "aginctus-image-herdr-client" hostOnly.systemd.services);
assert !(builtins.hasAttr "aginctus-image-opencode-workload" hostOnly.systemd.services);
assert !(builtins.hasAttr "aginctus-reconcile" hostOnly.systemd.services);
assert builtins.hasAttr "aginctus/config.json" hostOnly.environment.etc;
# The identity manifest is declarative, public and does not require IFD.
assert builtins.hasAttr "aginctus/operator-package.json" base.environment.etc;
assert builtins.hasAttr "aginctus/operator-package.json" hostOnly.environment.etc;
assert lib.hasInfix "aginctus-system-package.json"
  (toString base.environment.etc."aginctus/operator-package.json".source);
assert !base.services.aginctus.reconcile.enable;
assert !base.services.aginctus.reconcile.gateway;
assert !(builtins.hasAttr "aginctus-reconcile" base.systemd.services);
assert settings.incus.management.network.name == "integration-mgmt";
assert settings.firewall.zone == "aginctus-dns";
assert settings.firewall.egress.allowTailscaleRouting;
assert !kernelRoutingOnly.services.aginctus.settings.firewall.egress.allowTailscaleRouting;
assert builtins.hasAttr "firewalld/zones/aginctus-vm.xml" withCustomZone.environment.etc;
assert builtins.all (
  cfg:
  lib.any (item: lib.hasInfix "settings.firewall.zone" item.message) (failedAginctusAssertions cfg)
) invalidFirewalldZones;
assert defaultPackage == self.packages.${system}.aginctus;
assert base.services.aginctus.firewall.enable;
assert base.systemd.timers.aginctus-firewall-reconcile.timerConfig.OnUnitActiveSec == "30s";
assert lib.hasInfix "AGINCTUS_INPUT" base.networking.firewall.extraCommands;
assert !(lib.hasInfix "AGINCTUS_INPUT" firewalldHost.networking.firewall.extraCommands);
# A native-only chain must not instantiate an incomplete nixos-fw table on a
# firewalld host: NixOS supplies the table's family only for its own backend.
assert !(builtins.hasAttr "nixos-fw" firewalldHost.networking.nftables.tables);
assert !(builtins.hasAttr "firewalld/zones/aginctus-egress.xml" firewalldHost.environment.etc);
assert lib.hasInfix "custom-egress"
  egressHost.environment.etc."firewalld/zones/aginctus-egress.xml".text;
assert !egressHost.services.firewalld.settings.CleanupOnExit;
assert egressHost.services.firewalld.settings.StrictForwardPorts;
assert egressHost.systemd.timers.aginctus-egress-firewall.timerConfig.AccuracySec == "1s";
assert
  egressHost.systemd.services.aginctus-egress-firewall.serviceConfig.RuntimeDirectory
  == "aginctus-egress-firewall";
assert
  egressHost.systemd.services.aginctus-egress-firewall.serviceConfig.RuntimeDirectoryMode == "0700";
assert builtins.elem "XDG_CONFIG_HOME=/run/aginctus-egress-firewall"
  egressHost.systemd.services.aginctus-egress-firewall.serviceConfig.Environment;
assert lib.hasInfix "/var/lib/aginctus-firewall/overrides.json"
  egressHost.systemd.services.aginctus-egress-firewall.serviceConfig.ExecStart;
assert !(builtins.hasAttr "nixos-fw" base.networking.nftables.tables);
assert nativeNftHost.networking.nftables.tables."nixos-fw".family == "inet";
assert builtins.elem "firewalld.service"
  firewalldHost.systemd.services.aginctus-firewall-reconcile.after;
assert lib.hasInfix "chain aginctus_dns_input"
  nativeNftHost.networking.nftables.tables."nixos-fw".content;
assert lib.hasInfix "jump aginctus_dns_input" nativeNftHost.networking.firewall.extraInputRules;
assert builtins.elem "nftables.service"
  nativeNftHost.systemd.services.aginctus-firewall-reconcile.after;

assert lib.hasInfix "aginctus-firewall-reconcile.service"
  withReconcile.systemd.services.aginctus-reconcile.script;
assert !(lib.hasInfix "gateway apply" reconcile.script);
# reconcile.gateway must pin XDG_STATE_HOME so the gateway CLI's
# os.UserHomeDir fallback (which fails inside systemd units that do not
# export $HOME) does not abort the reconcile oneshot.
assert builtins.elem "XDG_STATE_HOME=/var/lib/aginctus/state"
  reconcileGateway.serviceConfig.Environment;
# reconcile.gateway without reconcile.enable is rejected.
assert builtins.length (failedAginctusAssertions withReconcileGatewayNoEnable) == 1;
assert lib.hasInfix "reconcile.gateway requires"
  (builtins.head (failedAginctusAssertions withReconcileGatewayNoEnable)).message;
# Without reconcile.gateway, the reconcile unit does not set
# XDG_STATE_HOME; the gateway-only CLI subcommand keeps using its
# built-in state-dir resolution (which still needs a usable HOME or
# XDG_STATE_HOME at the call site, but is the caller's responsibility).
assert
  !(builtins.elem "XDG_STATE_HOME=/var/lib/aginctus/state"
    reconcile.serviceConfig.Environment or [ ]
  );
assert builtins.length secretAssertions == 1;
assert lib.hasInfix "gateway.postgres.password" (builtins.head secretAssertions).message;
assert lib.hasInfix "gateway.master.key" (builtins.head secretAssertions).message;
assert lib.hasInfix "gateway.salt.key" (builtins.head secretAssertions).message;
assert settings.workloads.dev.runtime.type == "opencode";
assert builtins.length (failedAginctusAssertions invalidAliases) == 2;
assert lib.any (item: lib.hasInfix "alias format" item.message) (
  failedAginctusAssertions invalidAliases
);
assert lib.any (item: lib.hasInfix "distinct aliases" item.message) (
  failedAginctusAssertions invalidAliases
);
assert builtins.length (failedAginctusAssertions duplicateAliases) == 1;
assert lib.hasInfix "distinct aliases"
  (builtins.head (failedAginctusAssertions duplicateAliases)).message;
assert builtins.length (failedAginctusAssertions unknownReconcileWorkload) == 1;
assert lib.hasInfix "missing"
  (builtins.head (failedAginctusAssertions unknownReconcileWorkload)).message;
assert imageUnit.serviceConfig.Type == "oneshot";
assert
  imageUnit.restartTriggers == [
    base.services.aginctus.images.herdrClient.source
    stub
  ];
assert
  base.systemd.services.aginctus-image-opencode-workload.restartTriggers == [
    base.services.aginctus.images.opencodeWorkload.source
    stub
  ];
assert builtins.elem pkgs.incus imageUnit.path;
assert
  reconcile.restartTriggers == [
    withReconcile.environment.etc."aginctus/config.json".source
    stub
    withReconcile.services.aginctus.images.herdrClient.source
    withReconcile.services.aginctus.images.opencodeWorkload.source
  ];
assert
  noImagesReconcile.restartTriggers == [
    withoutImages.environment.etc."aginctus/config.json".source
    stub
  ];
assert reconcile.requires == expected;
assert reconcile.after == expected;
assert noImagesReconcile.requires == [ "incus.service" ];
assert noImagesReconcile.after == [ "incus.service" ];
pkgs.runCommand "aginctus-nixos-module-evaluation" { } ''
  ${pkgs.python3}/bin/python3 - <<'PY'
  import os
  from pathlib import Path
  import subprocess

  log = Path.cwd() / "commands"
  scripts = {
      False: "${recordedScript reconcile}",
      True: "${recordedScript reconcileGateway}",
  }
  for gateway, script in scripts.items():
      expected = ["network ensure", "start aginctus-firewall-reconcile.service"]
      if gateway:
          expected.append("gateway apply")
      expected += ["herdr client ensure", "workload dev ensure"]

      def run(failure=""):
          result = subprocess.run(
              [script],
              env={**os.environ, "COMMAND_LOG": str(log), "FAIL_COMMAND": failure},
              capture_output=True, text=True,
          )
          return result

      # Exact traces exclude removal commands, including when gateway is off.
      log.write_text("")
      for _ in range(2):
          result = run()
          assert result.returncode == 0, result.stderr
      assert log.read_text().splitlines() == expected * 2

      for index, command in enumerate(expected):
          log.write_text("")
          result = run(command)
          assert result.returncode == 23, (command, result.returncode, result.stderr)
          assert log.read_text().splitlines() == expected[:index + 1], command
  PY
  touch "$out"
''
