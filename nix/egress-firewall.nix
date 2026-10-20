{ config, lib, ... }:
let
  cfg = config.services.aginctus;
  owner = "aginctus:egress:v1; NixOS structure, Aginctus runtime grants";
  policies = {
    aginctus-egress-out = {
      ingress = "aginctus-egress";
      egress = "ANY";
      target = "DROP";
      priority = -500;
    };
    aginctus-egress-in = {
      ingress = "ANY";
      egress = "aginctus-egress";
      target = "DROP";
      priority = -500;
    };
    aginctus-egress-input = {
      ingress = "aginctus-egress";
      egress = "HOST";
      target = "DROP";
      priority = -500;
    };
    aginctus-egress-https = {
      ingress = "aginctus-egress";
      egress = "";
      target = "CONTINUE";
      priority = -1000;
    };
    aginctus-egress-host = {
      ingress = "aginctus-egress";
      egress = "HOST";
      target = "CONTINUE";
      priority = -1000;
    };
  };
in
{
  config = lib.mkIf (cfg.enable && cfg.firewall.egress.enable) {
    # Retain the last rules while the daemon is stopped, and prevent the
    # pre-policy DNAT shortcut from admitting externally forwarded traffic.
    services.firewalld.settings = {
      CleanupOnExit = lib.mkDefault false;
      StrictForwardPorts = lib.mkDefault true;
    };
    assertions = [
      {
        assertion =
          !config.services.firewalld.settings.CleanupOnExit
          && config.services.firewalld.settings.StrictForwardPorts
          && config.services.firewalld.settings.NftablesFlowtable == "off"
          && config.services.firewalld.settings.NftablesTableOwner
          && config.services.firewalld.settings.FirewallBackend == "nftables"
          && builtins.elem config.services.firewalld.settings.ReloadPolicy [
            "DROP"
            "INPUT:DROP,FORWARD:DROP,OUTPUT:DROP"
          ]
          && cfg.settings.firewall.zone != "aginctus-egress";
        message = "services.aginctus.firewall.egress requires retained nftables-owned rules, DROP during reload, strict forwarded ports, no flowtable bypass and a separate management zone.";
      }
      {
        assertion =
          !config.networking.nftables.flushRuleset
          && config.networking.nftables.ruleset == ""
          && config.networking.nftables.rulesetFile == null;
        message = "services.aginctus.firewall.egress requires firewalld-owned filtering without a host-wide nftables flush or raw ruleset.";
      }
    ];
    # See docs/gateway-firewalld-egress.md for the permanent/runtime contract.
    environment.etc = {
      "firewalld/zones/aginctus-egress.xml".text = ''
        <?xml version="1.0" encoding="utf-8"?>
        <zone target="DROP" ingress-priority="-32768" egress-priority="-32768">
          <description>${owner}</description>
          <interface name="${lib.escapeXML cfg.settings.gateway.egress.network.name}"/>
        </zone>
      '';
    }
    // lib.mapAttrs' (
      name: policy:
      lib.nameValuePair "firewalld/policies/${name}.xml" {
        text = ''
          <?xml version="1.0" encoding="utf-8"?>
          <policy target="${policy.target}" priority="${toString policy.priority}">
            <description>${owner}</description>
            <ingress-zone name="${policy.ingress}"/>
              ${lib.optionalString (policy.egress != "") ''<egress-zone name="${policy.egress}"/>''}
          </policy>
        '';
      }
    ) policies;
    systemd.services.firewalld.reloadTriggers = [
      config.environment.etc."firewalld/zones/aginctus-egress.xml".source
    ]
    ++ map (name: config.environment.etc."firewalld/policies/${name}.xml".source) (
      builtins.attrNames policies
    );
  };
}
