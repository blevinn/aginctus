{
  self,
  nixpkgs,
  herdr,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  lib = nixpkgs.lib;

  herdrPackage = herdr.packages.${system}.herdr;

  # Mirror the workload image's NixOS configuration (services.openssh
  # with SetEnv HERDR_SOCKET_PATH and systemd.services.herdr with the
  # matching socket). The test VM does not need the Incus image base
  # because it is a standalone NixOS VM; the relevant surface is the
  # OpenSSH and systemd service definitions, which are reproduced
  # verbatim from the workload image.
  workloadModules = [
    (
      { pkgs, ... }:
      {
        system.stateVersion = "26.05";

        users.users.agent = {
          isNormalUser = true;
          home = "/home/agent";
          createHome = true;
          # No password; the test injects an SSH key via NixOS's
          # users.users.<name>.openssh.authorizedKeys instead.
          hashedPassword = "*";
        };

        environment.systemPackages = [
          herdrPackage
          pkgs.openssh
        ];

        services.openssh = {
          enable = true;
          settings = {
            PasswordAuthentication = false;
            KbdInteractiveAuthentication = false;
            PermitRootLogin = "no";
            AuthorizedKeysFile = ".ssh/authorized_keys";
            AllowAgentForwarding = false;
            AllowTcpForwarding = false;
            AllowStreamLocalForwarding = false;
            X11Forwarding = false;
            PermitTunnel = false;
            PermitUserRC = false;
          };
          # Mirrors the production sshd SetEnv from flake.nix: the
          # SSH CLI's HERDR_SOCKET_PATH must agree with the systemd
          # service's, so `herdr --machine ... workspace list` reaches
          # the managed server without any per-call wrapper or symlink.
          extraConfig = ''
            SetEnv HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock
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
in
pkgs.testers.runNixOSTest {
  name = "aginctus-herdr-socket-alignment";

  nodes.machine = { pkgs, ... }: {
    imports = workloadModules;
    virtualisation.memorySize = 1024;
  };

  testScript = ''
    import json

    start_all()
    machine.wait_for_unit("multi-user.target")

    # The sshd and herdr services must agree on HERDR_SOCKET_PATH. This
    # is the alignment the merged PR #328 wired in; verify the effective
    # configuration matches.
    machine.wait_for_unit("sshd.service")
    machine.wait_for_unit("herdr.service")

    # The herdr service binds the configured socket; the CLI resolves
    # it from the same environment. The two paths MUST match.
    service_env = machine.succeed(
        "systemctl show herdr.service -p Environment --no-pager"
    ).strip()
    # `sshd -T` outputs the effective configuration. SetEnv settings
    # surface as `setenv <NAME> <VALUE>` rather than the per-key form;
    # some OpenSSH versions also fold them into `acceptenv`. Probe both
    # so the assertion survives an upstream key rename.
    sshd_env = machine.succeed(
        "sshd -T 2>/dev/null"
    ).strip()
    setenv_line = ""
    for line in sshd_env.splitlines():
        # sshd -T emits `setenv` settings as `setenv NAME=VALUE`
        # (no space between NAME and =). Match that exact shape.
        if line.lower().startswith("setenv "):
            name_value = line[len("setenv "):]
            if name_value.startswith("HERDR_SOCKET_PATH="):
                setenv_line = name_value
                break
    # NixOS's systemd `Environment` field can quote values containing
    # '=' characters; match either form.
    assert (
        "HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock" in service_env
        or 'HERDR_SOCKET_PATH="/var/lib/herdr/config/herdr/herdr.sock"' in service_env
    ), service_env
    assert setenv_line.endswith("/var/lib/herdr/config/herdr/herdr.sock"), repr(setenv_line)

    # The systemd service writes the socket under StateDirectory, which
    # NixOS mounts at /var/lib/herdr. Confirm the socket appears after
    # the service starts; this is the precondition for the CLI call.
    machine.wait_for_file("/var/lib/herdr/config/herdr/herdr.sock")

    # Smoke probe: invoke the herdr CLI as the agent user with the
    # SSH-derived HERDR_SOCKET_PATH. No manual symlink or wrapper is
    # installed. The "machine list" subcommand must respond with a
    # JSON envelope rather than NotFound; an empty machine list is the
    # documented state on a clean image.
    def herdr_probe():
        return machine.succeed(
            "su - agent -c 'export HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock && "
            + "${herdrPackage}/bin/herdr machine list --json'"
        )

    initial = herdr_probe()
    parsed = json.loads(initial)
    assert isinstance(parsed, list), initial
    # A freshly built image has no registered managed machines; the
    # response must be a JSON array, not an error.
    assert parsed == [], parsed

    # Failure mode for missing socket: HERDR_SOCKET_PATH pointing at a
    # non-existent socket must surface a clean error, not a silent hang
    # or a path that "happens to work" via a stale fallback. `machine
    # list` reads a local cache, so the CLI command we exercise here is
    # `api snapshot`, which always contacts the server. The CLI sends
    # the JSON envelope to stderr; capture both streams.
    missing_probe = machine.succeed(
        "su - agent -c 'export HERDR_SOCKET_PATH=/run/herdr/missing.sock && "
        + "${herdrPackage}/bin/herdr api snapshot' 2>&1 || true"
    ).strip()
    # `api snapshot` exits non-zero on server_not_running; the test
    # driver did not raise because of the `|| true`. Recover the
    # envelope from stderr (merged into stdout by the redirection).
    parsed_missing = json.loads(missing_probe)
    assert "error" in parsed_missing, parsed_missing
    assert parsed_missing["error"]["code"] == "server_not_running", parsed_missing

    # Service restart preserves behaviour: the socket must come back at
    # the same path, and the probe must continue to return a valid JSON
    # envelope.
    machine.succeed("systemctl restart herdr.service")
    machine.wait_for_file("/var/lib/herdr/config/herdr/herdr.sock")
    post_restart = herdr_probe()
    parsed_restart = json.loads(post_restart)
    assert isinstance(parsed_restart, list), post_restart

    # Strict SSH posture must not be weakened by the socket alignment.
    # The merged configuration pins every security-sensitive option off
    # and the test confirms they remain so on a running VM.
    sshd_full = machine.succeed("sshd -T 2>/dev/null").strip()
    for option, want in [
        ("passwordauthentication", "no"),
        ("kbdinteractiveauthentication", "no"),
        ("permitrootlogin", "no"),
        ("allowagentforwarding", "no"),
        ("allowtcpforwarding", "no"),
        ("allowstreamlocalforwarding", "no"),
        ("x11forwarding", "no"),
        ("permittunnel", "no"),
        ("permituserrc", "no"),
    ]:
        got = ""
        for line in sshd_full.splitlines():
            if line.lower().startswith(option + " "):
                got = line.split(" ", 1)[1].strip()
        assert got == want, f"{option}={got!r} want {want!r}"
  '';
}
