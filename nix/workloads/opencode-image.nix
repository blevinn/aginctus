# Pinned OpenCode runtime and isolated workload system definition.
{
  pkgs,
  nixpkgs,
  system,
  herdrPackage,
  sshAuthorizeHelper,
}:
let
  # Bundle the OpenCode guest Python scripts into a single store
  # directory so the shared validator module sits alongside
  # provision.py and launch.py and is importable at runtime.
  opencodeGuestScripts = pkgs.runCommand "aginctus-opencode-guest-scripts" { } ''
    mkdir -p $out
    cp ${../../plugins/opencode/guest/provision.py} $out/provision.py
    cp ${../../plugins/opencode/guest/launch.py} $out/launch.py
    cp ${../../plugins/opencode/guest/gateway_endpoint.py} $out/gateway_endpoint.py
    cp ${../../plugins/opencode/guest/go_session_plugin.js} $out/go_session_plugin.js
    cp ${../../plugins/opencode/guest/siwc_plugin.js} $out/siwc_plugin.js
  '';

  # Scoped source patch for the flake-pinned CLI event-consumer join.
  aginctusPinnedOpenCode = import ./patched-opencode.nix { inherit pkgs; };

  # Runtime-only gateway configuration: no endpoint, model or key is
  # materialized into the Nix store. Herdr's PATH resolves this wrapper.
  aginctusOpenCode = pkgs.writeShellScriptBin "opencode" ''
    exec ${pkgs.python3}/bin/python3 ${opencodeGuestScripts}/launch.py ${aginctusPinnedOpenCode}/bin/opencode "$@"
  '';

  gatewayProvisionHelper = pkgs.writeShellScriptBin "aginctus-opencode-gateway-provision" ''
    exec ${pkgs.python3}/bin/python3 ${opencodeGuestScripts}/provision.py
  '';
  # Root-owned, pinned source and interpreter; operator-only host
  # invocation remains separate from the guest's network capability.
  opencodeEgressProbe = pkgs.writeShellScriptBin "aginctus-egress-probe" ''
    exec ${pkgs.python3}/bin/python3 -I -B ${../../plugins/opencode/guest/egress_probe.py} "$@"
  '';

  opencodeWorkloadSystem = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
      (
        { ... }:
        {
          system.stateVersion = "26.05";

          users.users.agent = {
            isNormalUser = true;
            home = "/home/agent";
            createHome = true;
          };

          environment.systemPackages = [
            herdrPackage
            pkgs.git
            aginctusOpenCode
            gatewayProvisionHelper
            opencodeEgressProbe
            pkgs.openssh
            sshAuthorizeHelper
          ];

          services.openssh = {
            enable = true;
            settings = {
              PasswordAuthentication = false;
              KbdInteractiveAuthentication = false;
              PermitRootLogin = "no";
              AuthorizedKeysFile = ".ssh/authorized_keys /var/lib/aginctus/ssh/authorized_keys/%u";
              AllowAgentForwarding = false;
              AllowTcpForwarding = false;
              AllowStreamLocalForwarding = false;
              X11Forwarding = false;
              PermitTunnel = false;
              PermitUserRC = false;
            };
            # Herdr's remote API bridge invokes '--session default'.
            # Explicit sessions resolve their socket from config_dir()
            # before consulting HERDR_SOCKET_PATH. Non-login SSH exec
            # does not inherit the systemd daemon's XDG variables, so
            # pin XDG_CONFIG_HOME to the daemon's directory as well.
            # No user-writable symlinks, per-call overrides or relaxed SSH.
            extraConfig = ''
              SetEnv HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock XDG_CONFIG_HOME=/var/lib/herdr/config
            '';
          };

          systemd.services.herdr = {
            description = "Herdr headless server";
            wantedBy = [ "multi-user.target" ];
            after = [ "network-online.target" ];
            wants = [ "network-online.target" ];

            serviceConfig = {
              User = "agent";
              Environment = [
                "HOME=/home/agent"
                "XDG_CONFIG_HOME=/var/lib/herdr/config"
                "XDG_RUNTIME_DIR=/run/herdr"
                "XDG_STATE_HOME=/var/lib/herdr"
                # Belt-and-braces: the server is a managed daemon and
                # must answer the same socket the SSH CLI declares
                # above. Without this, `HERDR_SOCKET_PATH` would be
                # ignored on the server side and the CLI would probe
                # an unrelated path on first contact.
                "HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock"
              ];
              ExecStart = "${herdrPackage}/bin/herdr server";
              Restart = "on-failure";
              RestartSec = "2s";
              RuntimeDirectory = "herdr";
              StateDirectory = "herdr";
            };
          };
        }
      )
    ];
  };

in
{
  system = opencodeWorkloadSystem;
  runtime = aginctusPinnedOpenCode;
}
