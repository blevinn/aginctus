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
          # The explicit '--session default' remote API bridge ignores
          # HERDR_SOCKET_PATH and derives its socket from XDG_CONFIG_HOME.
          # Both settings must match the daemon's canonical config directory.
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

  nodes.client = {
    imports = [
      (import ./guest-state-bindings.nix {
        bindings = [
          {
            class = "system";
            subdir = "herdr";
            target = "/var/lib/herdr";
          }
          {
            class = "system";
            subdir = "ssh";
            target = "/var/lib/aginctus/ssh";
          }
        ];
      })
    ];
    system.stateVersion = "26.05";
    environment.systemPackages = [
      (import ./herdr-client-command.nix { inherit pkgs herdrPackage; })
      pkgs.openssh
    ];
    programs.ssh.extraConfig = ''
      Include /var/lib/aginctus/ssh/machines/*.conf
    '';
    # Disposable stand-in for an Incus-attached filesystem, never pilot storage.
    systemd.mounts = [
      {
        what = "tmpfs";
        where = "/var/lib/aginctus/state/system";
        type = "tmpfs";
        wantedBy = [ "multi-user.target" ];
      }
    ];
  };

  testScript = ''
    import json
    import shlex

    start_all()
    client.wait_for_unit("var-lib-herdr.mount")
    client.wait_for_unit("var-lib-aginctus-ssh.mount")
    client.succeed('test "$(stat -c %d:%i /var/lib/herdr)" = "$(stat -c %d:%i /var/lib/aginctus/state/system/herdr)"')
    client.succeed('test "$(stat -c %d:%i /var/lib/aginctus/ssh)" = "$(stat -c %d:%i /var/lib/aginctus/state/system/ssh)"')

    # Seed only a nonsecret catalog fixture, never SSH credentials. Bare Incus
    # exec has no XDG environment; the shipped command must still read/write
    # the daemon's persistent catalog, ignoring ambient rootfs XDG paths.
    catalog = {"version": 1, "ssh": [{
        "id": "0123456789abcdef0123456789abcdef", "label": "fixture-only",
        "target": "fixture-only", "session": "default", "enabled": True,
    }]}
    client.succeed("mkdir -p /var/lib/herdr/herdr/client")
    client.succeed("printf '%s' '" + json.dumps(catalog) + "' > /var/lib/herdr/herdr/client/endpoints.json")
    bare_cli = "env -i PATH=/run/current-system/sw/bin HOME=/root herdr "
    rows = json.loads(client.succeed(bare_cli + "machine list --json"))
    assert len(rows) == 1 and rows[0]["enabled"] and rows[0]["label"] == "fixture-only", rows
    client.succeed(bare_cli + "machine disable " + catalog["ssh"][0]["id"])
    saved = json.loads(client.succeed("cat /var/lib/aginctus/state/system/herdr/herdr/client/endpoints.json"))
    assert saved["ssh"][0]["enabled"] is False, saved
    client.succeed("test ! -e /root/.local/state/herdr/client/endpoints.json")
    # Unbind/rebind simulates rediscovery of the same data without creating a
    # private identity. This is not a real Incus guest-recreation acceptance.
    client.succeed("systemctl stop var-lib-herdr.mount")
    client.fail(bare_cli + "machine list --json")
    client.succeed("systemctl start var-lib-herdr.mount")
    rows = json.loads(client.succeed(bare_cli + "machine list --json"))
    assert len(rows) == 1 and rows[0]["enabled"] is False, rows
    client.succeed("test ! -e /var/lib/aginctus/ssh/manifest")
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
    setenv = {
        setting
        for line in sshd_env.splitlines()
        if line.lower().startswith("setenv ")
        for setting in line.split()[1:]
    }
    # NixOS's systemd `Environment` field can quote values containing
    # '=' characters; match either form.
    assert (
        "HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock" in service_env
        or 'HERDR_SOCKET_PATH="/var/lib/herdr/config/herdr/herdr.sock"' in service_env
    ), service_env
    assert "HERDR_SOCKET_PATH=/var/lib/herdr/config/herdr/herdr.sock" in setenv, setenv
    assert "XDG_CONFIG_HOME=/var/lib/herdr/config" in setenv, setenv

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

    # Real two-node managed-SSH API forwarding, not local machine-list/cache.
    # Test-only keys live in the disposable VM. No production SSH identity
    # is generated; host trust is sourced from the other VM, not ssh-keyscan.
    client.succeed("install -d -m 0700 /var/lib/aginctus/ssh/identities/1 /var/lib/aginctus/ssh/trust /var/lib/aginctus/ssh/machines")
    client.succeed("ssh-keygen -q -t ed25519 -N \\"\\" -f /var/lib/aginctus/ssh/identities/1/id_ed25519")
    client.succeed("chmod 600 /var/lib/aginctus/ssh/identities/1/id_ed25519")
    public_key = client.succeed("cat /var/lib/aginctus/ssh/identities/1/id_ed25519.pub").strip()
    machine.succeed("install -d -o agent -m 0700 /home/agent/.ssh")
    machine.succeed("printf '%s\\n' " + shlex.quote(public_key)
                    + " > /home/agent/.ssh/authorized_keys && chown agent /home/agent/.ssh/authorized_keys"
                    + " && chmod 0600 /home/agent/.ssh/authorized_keys")
    host_key = machine.succeed("cat /etc/ssh/ssh_host_ed25519_key.pub").split()
    assert host_key[0] == "ssh-ed25519"
    client.succeed("printf '%s\\n' " + shlex.quote("aginctus-dev " + " ".join(host_key[:2]))
                   + " > /var/lib/aginctus/ssh/trust/known_hosts && chmod 0600 /var/lib/aginctus/ssh/trust/known_hosts")
    ssh_config = """Host aginctus-dev
    HostName machine
    User agent
    IdentityFile /var/lib/aginctus/ssh/identities/1/id_ed25519
    IdentitiesOnly yes
    IdentityAgent none
    BatchMode yes
    StrictHostKeyChecking yes
    UpdateHostKeys no
    UserKnownHostsFile /var/lib/aginctus/ssh/trust/known_hosts
    GlobalKnownHostsFile /dev/null
    HostKeyAlias aginctus-dev
    HostKeyAlgorithms ssh-ed25519
    ConnectTimeout 5
"""
    client.succeed("printf '%s\\n' " + shlex.quote(ssh_config)
                   + " > /var/lib/aginctus/ssh/machines/aginctus-dev.conf")
    assert client.succeed("ssh -o BatchMode=yes aginctus-dev id -un").strip() == "agent"
    assert client.succeed("ssh -o BatchMode=yes aginctus-dev printenv XDG_CONFIG_HOME").strip() == "/var/lib/herdr/config"
    client.succeed(bare_cli + "machine add aginctus-dev --label aginctus-dev --remote-session default")
    def remote_workspaces():
        output = client.succeed(bare_cli + "--machine aginctus-dev workspace list")
        envelope = json.loads(output)
        assert envelope["result"]["type"] == "workspace_list", envelope
        assert isinstance(envelope["result"]["workspaces"], list), envelope
    remote_workspaces()
    machine.succeed("systemctl restart herdr.service")
    machine.wait_for_file("/var/lib/herdr/config/herdr/herdr.sock")
    remote_workspaces()
    client.succeed("test ! -e /root/.local/state/herdr/client/endpoints.json")
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
