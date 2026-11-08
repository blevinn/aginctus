{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };

  # Evaluate a real standalone Home Manager configuration, without linking
  # Home Manager into NixOS modules or granting the user Incus access.
  # Prebuild a generation for the CLI and repeat-activation checks in the
  # offline guest.
  homeManager = import "${pkgs.home-manager.src}/lib" { lib = pkgs.lib; };
  makeGeneration =
    gatewayID:
    homeManager.homeManagerConfiguration {
      inherit pkgs;
      modules = [
        self.homeManagerModules.default
        {
          home.username = "operator";
          home.homeDirectory = "/home/operator";
          home.stateVersion = "26.05";
          programs.aginctus = {
            enable = true;
            settings.gateway.id = gatewayID;
            # User intent cannot alter the separately configured host policy.
            settings.firewall.zone = "trusted";
          };
        }
      ];
    };
  operatorGeneration = makeGeneration "disposable-operator";
  upgradedGeneration = makeGeneration "upgraded-operator";
  makeFlake =
    generation:
    pkgs.writeText "home-manager-operator-flake.nix" ''
      {
        inputs = {
          generation = {
            url = "path:${generation.activationPackage}";
            flake = false;
          };
          news = {
            url = "path:${generation.config.news.json.output}";
            flake = false;
          };
        };
        outputs = { generation, news, ... }: {
          homeConfigurations.operator = {
            activationPackage = generation.outPath;
            config.news.json.output = news.outPath;
          };
        };
      }
    '';
  operatorFlake = makeFlake operatorGeneration;
  upgradedFlake = makeFlake upgradedGeneration;
  unmanagedConfig = pkgs.writeText "unmanaged-operator-config.json" (
    builtins.toJSON { gateway.id = "unmanaged-operator"; }
  );
  mismatchedManifest = pkgs.writeText "mismatched-operator-package.json" (
    builtins.toJSON {
      schema = 1;
      authority = "user";
      package = "/nix/store/00000000000000000000000000000000-wrong-cli";
    }
  );
  checkReport = pkgs.writeText "check-operator-report.py" ''
    from pathlib import Path
    import sys

    report = Path("/run/operator-report").read_text()
    for root in ["/var/lib/aginctus/state", "/home/operator/.local/state"]:
        secret = Path(root) / "aginctus/gateway/upgraded-operator/secrets.json"
        if secret.is_file() and not secret.is_symlink():
            if secret.read_text().strip() in report:
                sys.exit("synthetic credential material in operator report")
    for user in ["operator", "administrator"]:
        for name in ["client.key", "client.crt"]:
            path = Path(f"/home/{user}/.config/incus/{name}")
            if not path.exists():
                continue
            # Check body fragments too, not only PEM delimiters. Emit no
            # report or credential contents when a redaction check fails.
            for line in path.read_text().splitlines():
                if line and line in report:
                    sys.exit("credential material in operator report")
    print(report, end="")
  '';
in
pkgs.testers.runNixOSTest {
  name = "aginctus-vm-home-manager-operator";

  nodes.machine = {
    imports = [ self.nixosModules.default ];
    system.stateVersion = "26.05";
    networking.nftables.enable = true;
    networking.dhcpcd.allowInterfaces = [ "eth0" ];
    services.firewalld.enable = true;
    services.aginctus = {
      enable = true;
      images.herdrClient.enable = false;
      images.opencodeWorkload.enable = false;
      reconcile.enable = false;
      firewall.egress.enable = true;
      settings = {
        incus.management.network.name = "hm-mgmt";
        gateway.egress.network.name = "hm-egress";
      };
    };
    boot.supportedFilesystems = [ "btrfs" ];
    virtualisation = {
      cores = 2;
      memorySize = 4096;
      diskSize = 12288;
      incus.enable = true;
    };
    users.users.operator = {
      uid = 1000;
      isNormalUser = true;
      home = "/home/operator";
      createHome = true;
    };
    users.users.administrator = {
      uid = 1001;
      isNormalUser = true;
      home = "/home/administrator";
      createHome = true;
    };
    nix.settings.allowed-users = [
      "root"
      "operator"
    ];
    nix.settings.experimental-features = [
      "nix-command"
      "flakes"
    ];
    # Embed the full prebuilt generation and its closure in the guest store.
    # This does not activate Home Manager or touch the user's home as root.
    system.extraDependencies = [
      operatorGeneration.activationPackage
      upgradedGeneration.activationPackage
      operatorFlake
      upgradedFlake
      pkgs.home-manager
      operatorGeneration.config.news.json.output
      upgradedGeneration.config.news.json.output
    ];
    environment.systemPackages = [
      pkgs.incus
      pkgs.jq
      pkgs.home-manager
      pkgs.openssl
      pkgs.firewalld
      pkgs.nftables
      pkgs.netcat-openbsd
      pkgs.dnsutils
    ];
  };

  testScript = ''
    start_all()
    machine.wait_for_unit("incus.service")
    # The VM's Incus daemon is initialized solely to make zero-mutation
    # assertions meaningful. The user has no Incus permission or group grant.
    machine.succeed("incus admin init --auto --storage-backend btrfs --storage-create-loop 2")
    machine.succeed("incus storage list --format json | jq -e 'any(.[]; .name == \"default\" and .driver == \"btrfs\")'")
    machine.succeed("incus list --format json | jq -e 'length == 0'")
    machine.succeed("incus image list --format json | jq -e 'length == 0'")
    machine.wait_for_unit("firewalld.service")
    machine.succeed("incus network create hm-mgmt ipv4.address=10.247.84.1/24 ipv4.nat=false ipv4.routing=false ipv6.address=none raw.dnsmasq=address=/fixture.incus/10.247.84.1 user.aginctus.managed=true user.aginctus.resource=management-network")
    machine.succeed("incus project create aginctus -c restricted=true -c features.networks=true")
    machine.succeed("incus project create foreign")
    for project in ["default", "aginctus", "foreign"]:
        machine.succeed(f"incus profile create scope-sentinel --project {project}")
    machine.wait_until_succeeds("systemctl reset-failed aginctus-firewall-reconcile.service; systemctl start aginctus-firewall-reconcile.service", timeout=60)

    def host_policy():
        return machine.succeed("nft -s list ruleset")

    def host_units():
        units = machine.succeed("systemctl list-unit-files --no-legend 'aginctus*'")
        observed = {line.split()[0] for line in units.splitlines()}
        assert observed == {"aginctus-firewall-reconcile.service", "aginctus-firewall-reconcile.timer", "aginctus-egress-firewall.service", "aginctus-egress-firewall.timer"}, units
        for name in ["aginctus-firewall-reconcile", "aginctus-egress-firewall"]:
            assert machine.succeed(f"systemctl show -p User --value {name}.service").strip() == "root"
            machine.succeed(f"systemctl is-active {name}.timer")
        machine.succeed("test $(stat -c '%u:%g:%a' /var/lib/aginctus-firewall) = 0:0:700")
        machine.succeed("test $(stat -c '%u:%g:%a' /var/lib/aginctus-firewall/overrides.json) = 0:0:600")

    host_units()
    firewall_before = host_policy()
    host_config = machine.succeed("sha256sum /etc/aginctus/config.json /etc/aginctus/operator-package.json")

    # Compare complete nonsecret resource inventory, not just instance counts.
    inventories = ["project list", "storage list"] + [
        f"{command} --project {project}"
        for project in ["default", "aginctus", "foreign"]
        for command in ["list", "image list", "network list", "profile list"]
    ]
    before = {command: machine.succeed(f"incus {command} --format json | jq -S .") for command in inventories}

    # Synthetic protected host state is not a source for operator credentials.
    machine.succeed("install -d -m 0700 /var/lib/aginctus/state")
    machine.succeed("printf synthetic-protected-state > /var/lib/aginctus/state/acceptance-sentinel; chmod 0600 /var/lib/aginctus/state/acceptance-sentinel")
    protected_before = machine.succeed("sha256sum /var/lib/aginctus/state/acceptance-sentinel; stat -c '%u:%g:%a' /var/lib/aginctus/state /var/lib/aginctus/state/acceptance-sentinel")

    operator = "runuser -u operator -- env HOME=/home/operator USER=operator LOGNAME=operator"

    # Exercise Home Manager's real CLI as the unprivileged operator. The
    # fixture and all Nix inputs are already in the guest store; no network or
    # NixOS-side activation is involved.
    home_manager = "${pkgs.home-manager}/bin/home-manager"
    machine.succeed(operator + " install -d -m 0700 /home/operator/home-manager-flake")
    machine.succeed(operator + " cp ${operatorFlake} /home/operator/home-manager-flake/flake.nix")
    machine.succeed(operator + " nix --offline flake lock /home/operator/home-manager-flake", timeout=120)
    operator_home_manager = operator + " " + home_manager + " --flake /home/operator/home-manager-flake#operator"
    machine.succeed("test -x " + home_manager)
    machine.fail("test -e /home/operator/.config/aginctus/config.json")
    machine.succeed(operator_home_manager + " build --no-out-link", timeout=600)
    machine.fail("test -e /home/operator/.config/aginctus/config.json")
    machine.succeed(operator_home_manager + " switch", timeout=600)

    # Retain an explicit repeat activation using the independently evaluated
    # generation closure as a second non-mutating activation path.
    activation = "runuser -u operator -- env HOME=/home/operator USER=operator LOGNAME=operator ${operatorGeneration.activationPackage}/activate"
    machine.succeed("test -x ${operatorGeneration.activationPackage}/activate")
    machine.succeed("runuser -u operator -- test -L /home/operator/.config/aginctus/config.json")
    machine.succeed("runuser -u operator -- jq -e '.gateway.id == \"disposable-operator\"' /home/operator/.config/aginctus/config.json")
    machine.succeed("runuser -u operator -- /home/operator/.nix-profile/bin/aginctus config get gateway.id | grep -q disposable-operator")
    cli = operator + " /home/operator/.nix-profile/bin/aginctus"
    assert not {"wheel", "incus", "incus-admin"}.intersection(machine.succeed(operator + " id -nG").split())
    machine.succeed(cli + " operator package-audit --owner user | grep -q 'observed running CLI path'")
    status, _ = machine.execute(cli + " operator preflight --project aginctus", timeout=30)
    assert status == 1, "operator preflight must fail for denied Incus access, not usage"
    assert host_policy() == firewall_before, "unauthorized activation changed firewall policy"
    machine.fail(operator + " test -r /var/lib/aginctus/state/acceptance-sentinel")

    # Repeated activation remains an operator-only package/config change.
    machine.succeed(activation, timeout=600)
    machine.succeed(cli + " operator package-audit --owner user")

    # A changed declaration must not make incompatible system/user CLIs acceptable.
    manifest = "/home/operator/.config/aginctus/operator-package.json"
    machine.succeed(operator + " rm -f " + manifest)
    machine.succeed(operator + " cp ${mismatchedManifest} " + manifest)
    status, mismatch = machine.execute(cli + " operator package-audit --owner user 2>&1")
    assert status == 1 and "operator package declarations differ" in mismatch, "system/user declaration mismatch must fail safely"
    machine.succeed(operator + " rm -f " + manifest)

    # The CLI selects its profile before activation; a file conflict must leave
    # the active-generation GC root and unmanaged contents intact.
    profile = "/home/operator/.local/state/nix/profiles/home-manager"
    active_generation = "/home/operator/.local/state/home-manager/gcroots/current-home"
    original_profile = machine.succeed(operator + " readlink -f " + profile).strip()
    assert machine.succeed(operator + " readlink -f " + active_generation).strip() == "${operatorGeneration.activationPackage}"
    machine.succeed(operator + " install -d -m 0700 /home/operator/upgraded-flake")
    machine.succeed(operator + " cp ${upgradedFlake} /home/operator/upgraded-flake/flake.nix")
    machine.succeed(operator + " nix --offline flake lock /home/operator/upgraded-flake", timeout=120)
    upgraded_home_manager = operator + " " + home_manager + " --flake /home/operator/upgraded-flake#operator"
    config = "/home/operator/.config/aginctus/config.json"
    machine.succeed(operator + " rm -f " + config)
    machine.succeed(operator + " cp ${unmanagedConfig} " + config)
    conflicting_file = machine.succeed("sha256sum " + config)
    status, failure = machine.execute(upgraded_home_manager + " switch 2>&1", timeout=600)
    assert status != 0 and "would be clobbered" in failure, "unmanaged config must block activation"
    assert machine.succeed("sha256sum " + config) == conflicting_file
    machine.fail("test -L " + config)
    assert machine.succeed(operator + " readlink -f " + profile).strip() != original_profile
    assert machine.succeed(operator + " readlink -f " + active_generation).strip() == "${operatorGeneration.activationPackage}"
    machine.succeed(cli + " config get gateway.id | grep -q unmanaged-operator")
    machine.succeed(operator + " rm -f " + config)

    # Retry after explicit conflict removal, then roll back using the recorded
    # generation's activate script, as standalone Home Manager documents.
    upgrade = operator + " ${upgradedGeneration.activationPackage}/activate"
    machine.succeed(upgraded_home_manager + " switch", timeout=600)
    machine.succeed(cli + " config get gateway.id | grep -q upgraded-operator")
    assert machine.succeed(operator + " readlink -f " + active_generation).strip() == "${upgradedGeneration.activationPackage}"
    machine.succeed(operator + " " + original_profile + "/activate", timeout=600)
    assert machine.succeed(operator + " readlink -f " + profile).strip() == "${operatorGeneration.activationPackage}"
    assert machine.succeed(operator + " readlink -f " + active_generation).strip() == "${operatorGeneration.activationPackage}"
    machine.succeed(cli + " config get gateway.id | grep -q disposable-operator")
    machine.succeed(cli + " operator package-audit --owner user")
    machine.succeed(upgraded_home_manager + " switch", timeout=600)
    machine.succeed(cli + " config get gateway.id | grep -q upgraded-operator")
    machine.succeed(cli + " operator package-audit --owner user")

    # Starting a lingering user manager does not start Aginctus units or timers.
    machine.succeed("loginctl enable-linger operator")
    machine.wait_for_unit("user@1000.service")
    user_units = machine.succeed(operator + " env XDG_RUNTIME_DIR=/run/user/1000 systemctl --user list-unit-files --no-pager --no-legend")
    assert not any(line.startswith("aginctus") for line in user_units.splitlines()), "Aginctus user units or timers were installed"
    machine.succeed("loginctl disable-linger operator")
    machine.succeed("systemctl stop user@1000.service")

    # Only the disposable administrator provisions TLS trust. Private keys are
    # generated inside the VM, never materialized in Nix or returned to the log.
    machine.succeed("incus config set core.https_address 127.0.0.1:8443")
    for user in ["operator", "administrator"]:
        home = f"/home/{user}"
        machine.succeed(f"runuser -u {user} -- install -d -m 0700 {home}/.config/incus")
        machine.succeed(f"runuser -u {user} -- sh -c 'umask 077; openssl req -x509 -newkey rsa:2048 -nodes -keyout {home}/.config/incus/client.key -out {home}/.config/incus/client.crt -days 1 -subj /CN={user} >/dev/null 2>&1'")
        restriction = " --restricted --projects aginctus" if user == "operator" else ""
        machine.succeed(f"incus config trust add-certificate {home}/.config/incus/client.crt --name {user}{restriction}")
        # Incus selects the sole permitted project when adding a restricted
        # remote. Subsequent probes must not rewrite this client configuration.
        machine.succeed(f"runuser -u {user} -- env HOME={home} incus remote add fixture https://127.0.0.1:8443 --accept-certificate --auth-type tls")
        machine.succeed(f"runuser -u {user} -- env HOME={home} incus remote switch fixture")
        # Initialize Incus's last-working-address cache before freezing config.
        machine.succeed(f"runuser -u {user} -- env HOME={home} incus project list --format json >/dev/null")

    admin = "runuser -u administrator -- env HOME=/home/administrator USER=administrator LOGNAME=administrator"
    admin_cli = admin + " ${self.packages.${system}.aginctus}/bin/aginctus"
    client_config = machine.succeed("sha256sum /home/operator/.config/incus/config.yml /home/administrator/.config/incus/config.yml")

    def report(command, expected_status):
        status, _ = machine.execute("umask 077; " + command + " > /run/operator-report 2>&1", timeout=30)
        assert (status != 0 if expected_status == "denied" else status == expected_status), "unexpected command status"
        output = machine.succeed("${pkgs.python3}/bin/python3 ${checkReport}")
        assert "PRIVATE KEY" not in output and "BEGIN CERTIFICATE" not in output, "credential material in report"
        return output

    def denied(command, reason):
        assert reason in report(command, "denied"), "failure did not demonstrate the expected boundary"

    restricted = report(operator + " env INCUS_PROJECT=foreign /home/operator/.nix-profile/bin/aginctus operator preflight --project aginctus", 0)
    assert "restricted TLS credential; selected project declared permitted (writes unproven)" in restricted
    assert "Incus project: aginctus (instance read allowed)" in restricted
    assert "Write permissions: unverified" in restricted
    assert "Host bridge, firewall, pool and admin permissions: unverified" in restricted
    report(cli + " operator preflight --project foreign", 1)
    # The CLI refuses default before any probe; direct GET also proves server denial.
    report(cli + " operator preflight --project default", 2)
    machine.succeed(operator + " incus profile show scope-sentinel --project aginctus")
    for project in ["default", "foreign"]:
        denied(operator + f" incus project show {project}", "does not have permission")
    denied(operator + " incus profile show scope-sentinel --project foreign", "does not have permission")
    # Incus TLS authorization allows reads of default-project resources for
    # inheritance. Restricted credentials do not establish metadata isolation.
    machine.succeed(operator + " incus profile show scope-sentinel --project default")
    denied(operator + " incus profile set scope-sentinel user.phase1=forbidden --project default", "does not have permission")
    machine.succeed(operator + " incus project list --format json | jq -e 'map(.name) == [\"aginctus\"]'")

    # Effective write denials belong ONLY in this disposable fixture, never in
    # production preflight. Exact project flags prevent ambient switching.
    denied(operator + " incus network create hm-denied --type bridge --project aginctus ipv4.address=none ipv6.address=none", "Network type does not support non-default projects")
    denied(operator + " incus network create hm-denied --type bridge --project default ipv4.address=none ipv6.address=none", "does not have permission")
    denied(operator + " incus project create forbidden-project", "Certificate is restricted")
    denied(operator + " incus config set core.https_address=127.0.0.1:9443", "Certificate is restricted")
    machine.fail(operator + " ip link add forbidden-linux type bridge")
    machine.fail(operator + " nft add table inet forbidden_table")
    machine.fail(operator + " systemctl start aginctus-reconcile.service")
    denied(operator + " systemctl start aginctus-firewall-reconcile.service", "Access denied")
    machine.fail(operator + " test -r /var/lib/aginctus-firewall/overrides.json")
    machine.fail(operator + " sh -c 'printf unsafe > /var/lib/aginctus-firewall/overrides.json'")
    machine.fail(operator + " sh -c 'printf unsafe > /var/lib/aginctus/state/acceptance-sentinel'")

    broad = report(admin_cli + " operator preflight --project aginctus", 0)
    assert "broad unrestricted TLS credential; effective administrative capabilities unproven" in broad
    assert "Write permissions: unverified" in broad
    machine.succeed(admin + " incus list --project foreign --format json | jq -e 'length == 0'")
    # Positive control for the exact host-network operation denied above. This
    # demonstrates one effective fixture capability, not arbitrary mutation rights.
    machine.succeed(admin + " incus project show foreign")
    machine.succeed(admin + " incus profile show scope-sentinel --project foreign")
    machine.succeed(admin + " incus network create hm-denied --type bridge --project default ipv4.address=none ipv6.address=none")
    machine.succeed(admin + " incus network delete hm-denied --project default")

    # Re-activation with restricted credentials still changes no Incus objects.
    firewall_authorized = host_policy()
    machine.succeed(upgrade, timeout=600)
    assert host_policy() == firewall_authorized, "authorized activation changed firewall policy"
    assert machine.succeed("sha256sum /home/operator/.config/incus/config.yml /home/administrator/.config/incus/config.yml") == client_config
    assert machine.succeed("sha256sum /etc/aginctus/config.json /etc/aginctus/operator-package.json") == host_config
    host_units()

    # Real DNS positive/host-port negative survive host firewall reload without
    # any user controller or authority to start privileged firewall operations.
    machine.succeed("ip netns add guest; ip link add guest-host type veth peer name guest-eth; ip link set guest-host master hm-mgmt; ip link set guest-host up; ip link set guest-eth netns guest; ip -n guest link set lo up; ip -n guest link set guest-eth up; ip -n guest addr add 10.247.84.25/24 dev guest-eth")
    machine.succeed("systemd-run --unit=host-listener ${pkgs.python3}/bin/python3 -m http.server 18082 --bind 0.0.0.0")
    machine.wait_until_succeeds("ss -lnt | grep ':18082'")
    def packets():
        machine.succeed("ip netns exec guest dig +time=1 +tries=1 @10.247.84.1 fixture.incus A | grep -F 10.247.84.1")
        machine.fail("ip netns exec guest nc -z -w 1 10.247.84.1 18082")
    packets()
    machine.succeed("firewall-cmd --reload")
    machine.succeed("systemctl start aginctus-firewall-reconcile.service")
    packets()

    # Mask both activation paths so preflight cannot restart Incus implicitly.
    # User configuration switching must remain independent of daemon health.
    client_credentials = machine.succeed("sha256sum /home/operator/.config/incus/client.key /home/operator/.config/incus/client.crt")
    machine.succeed("systemctl mask --runtime --now incus.socket incus.service", timeout=60)
    machine.fail("systemctl is-active --quiet incus.service")
    unavailable = report(cli + " operator preflight --project aginctus", 1)
    assert "unavailable-or-denied" in unavailable
    machine.succeed(operator_home_manager + " switch", timeout=600)
    machine.succeed(cli + " config get gateway.id | grep -q disposable-operator")
    machine.succeed(upgraded_home_manager + " switch", timeout=600)
    machine.succeed(cli + " config get gateway.id | grep -q upgraded-operator")
    machine.succeed(cli + " operator package-audit --owner user")
    machine.fail("systemctl is-active --quiet incus.service")
    machine.fail(operator + " test -r /var/lib/aginctus/state/acceptance-sentinel")
    machine.succeed("systemctl unmask --runtime incus.socket incus.service")
    machine.succeed("systemctl start incus.socket incus.service", timeout=60)
    machine.wait_for_unit("incus.service")
    recovered = report(cli + " operator preflight --project aginctus", 0)
    assert "restricted TLS credential; selected project declared permitted (writes unproven)" in recovered
    assert machine.succeed("sha256sum /home/operator/.config/incus/client.key /home/operator/.config/incus/client.crt") == client_credentials
    assert machine.succeed("sha256sum /home/operator/.config/incus/config.yml /home/administrator/.config/incus/config.yml") == client_config
    machine.wait_until_succeeds("systemctl reset-failed aginctus-firewall-reconcile.service; systemctl start aginctus-firewall-reconcile.service", timeout=60)
    # Incus recreates its bridge on clean shutdown/start. The synthetic peer
    # is driver-owned, so reattach it rather than claiming workload recovery.
    machine.succeed("ip link set guest-host master hm-mgmt; ip link set guest-host up")
    packets()

    # Metadata diagnostics must not turn an unreadable host tree into evidence
    # of absence, or repair/adopt a competing user credential tree.
    system_gateway = "/var/lib/aginctus/state/aginctus/gateway/upgraded-operator"
    user_gateway = "/home/operator/.local/state/aginctus/gateway/upgraded-operator"
    machine.succeed("install -d -m 0700 " + system_gateway)
    machine.succeed("umask 077; openssl rand -hex 32 > " + system_gateway + "/secrets.json")
    machine.succeed(operator + " install -d -m 0700 " + user_gateway)
    machine.succeed(operator + " sh -c 'umask 077; openssl rand -hex 32 > " + user_gateway + "/secrets.json'")
    def credential_metadata(path):
        return machine.succeed("sha256sum " + path + "/secrets.json; stat -c '%u:%g:%a' " + path + " " + path + "/secrets.json")
    system_gateway_before = credential_metadata(system_gateway)
    user_gateway_before = credential_metadata(user_gateway)
    state_audit = cli + " operator state-audit --owner user"
    state_report = report(state_audit, 1)
    assert "System gateway credential metadata: unverified" in state_report
    assert "User gateway credential metadata: present" in state_report
    assert "no migration" in state_report
    machine.fail(operator + " test -r " + system_gateway + "/secrets.json")
    root_cli = "env XDG_STATE_HOME=/run/absent-operator-state ${
      self.packages.${system}.aginctus
    }/bin/aginctus --config gateway.id=upgraded-operator"
    root_report = report(root_cli + " operator state-audit --owner system", 0)
    assert "System gateway credential metadata: present" in root_report
    assert "User gateway credential metadata: absent" in root_report
    machine.fail("test -e /run/absent-operator-state")

    machine.succeed(operator + " chmod 0644 " + user_gateway + "/secrets.json")
    assert "User gateway credential metadata: unsafe" in report(state_audit, 1)
    machine.succeed("test $(stat -c '%a' " + user_gateway + "/secrets.json) = 644")
    machine.succeed(operator + " chmod 0600 " + user_gateway + "/secrets.json")
    machine.succeed(operator + " mv " + user_gateway + "/secrets.json " + user_gateway + "/saved-credential")
    machine.succeed(operator + " ln -s " + system_gateway + "/secrets.json " + user_gateway + "/secrets.json")
    assert "User gateway credential metadata: unsafe" in report(state_audit, 1)
    machine.succeed("test -L " + user_gateway + "/secrets.json")
    machine.succeed(operator + " rm -f " + user_gateway + "/secrets.json")
    machine.succeed(operator + " mv " + user_gateway + "/saved-credential " + user_gateway + "/secrets.json")
    machine.succeed("chown root:root " + user_gateway + "/secrets.json")
    assert "User gateway credential metadata: unsafe" in report(state_audit, 1)
    machine.succeed("test $(stat -c '%u' " + user_gateway + "/secrets.json) = 0")
    machine.succeed("chown operator:users " + user_gateway + "/secrets.json")
    machine.succeed(operator_home_manager + " switch", timeout=600)
    machine.succeed(upgraded_home_manager + " switch", timeout=600)
    assert "User gateway credential metadata: present" in report(state_audit, 1)
    assert credential_metadata(system_gateway) == system_gateway_before
    assert credential_metadata(user_gateway) == user_gateway_before

    for command in inventories:
        assert machine.succeed(f"incus {command} --format json | jq -S .") == before[command], f"HM activation or user-manager lifecycle changed Incus {command}"
    assert machine.succeed("sha256sum /var/lib/aginctus/state/acceptance-sentinel; stat -c '%u:%g:%a' /var/lib/aginctus/state /var/lib/aginctus/state/acceptance-sentinel") == protected_before
    machine.succeed("incus list --format json | jq -e 'length == 0'")
    machine.succeed("incus image list --format json | jq -e 'length == 0'")
    host_units()
    machine.succeed("test ! -e /home/operator/.config/systemd/user/aginctus-reconcile.service")
    machine.succeed("test ! -e /home/operator/.config/systemd/user/aginctus-image-update.service")
  '';
}
