# Explicit, heavyweight disposable NixOS test. No pilot/host Incus daemon.
# Provision and use an actually project-restricted TLS principal, not an
# administrator selecting a project with --project.
{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  # NixOS container image built outside the guest; no Internet image pull,
  # unpinned alias or copy from a real host is necessary at runtime.
  workloadImage = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
      {
        system.stateVersion = "26.05";
        networking.useDHCP = false;
      }
    ];
  };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-incus-project-authority";
  nodes.machine = { ... }: {
    system.stateVersion = "26.05";
    networking.nftables.enable = true;
    virtualisation = {
      memorySize = 4096;
      diskSize = 16384;
      cores = 2;
      incus.enable = true;
      incus.package = pkgs.incus;
    };
    environment.systemPackages = [
      pkgs.incus
      pkgs.jq
      pkgs.coreutils
      pkgs.util-linux
      pkgs.curl
    ];
    users.users.operator = {
      uid = 1000;
      isNormalUser = true;
      home = "/home/operator";
      createHome = true;
      # Deliberately no incus or incus-admin membership: confined TLS only.
      extraGroups = [ ];
    };
  };

  testScript = ''
    import json
    import shlex
    start_all()
    machine.wait_for_unit("incus.service")
    # The test driver supplies an independent disposable QEMU disk and Incus
    # daemon. Storage is initialized inside the guest, never on the host.
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus info")
    machine.succeed("incus version")
    machine.succeed("incus project create aginctus-authz --config restricted=true --config features.images=true --config features.profiles=true --config features.storage.volumes=true --config features.networks=false")
    machine.succeed("incus project create unrelated --config restricted=true")
    machine.succeed("incus network create authz-mgmt ipv4.address=none ipv6.address=none")
    # Explicitly bound access to one host-admin managed shared bridge/pool.
    machine.succeed("incus project set aginctus-authz restricted.devices.nic=managed restricted.networks.access=authz-mgmt restricted.storage-pools.access=default")
    machine.succeed("incus project get aginctus-authz restricted | grep -qx true")
    machine.succeed("incus project get aginctus-authz features.networks | grep -qx false")
    machine.succeed("incus project get aginctus-authz restricted.networks.access | grep -qx authz-mgmt")

    # A VM-only loopback HTTPS endpoint. No host networking or public listener.
    machine.succeed("incus config set core.https_address 127.0.0.1:8443")
    machine.wait_until_succeeds("curl --silent --insecure https://127.0.0.1:8443/1.0 >/dev/null", timeout=60)

    # The user creates its OWN ephemeral key. Root only imports the PUBLIC
    # certificate as restricted to the explicitly provisioned project.
    operator = "runuser -u operator -- env HOME=/home/operator USER=operator LOGNAME=operator"
    machine.succeed(operator + " incus remote generate-certificate")
    machine.succeed("test -f /home/operator/.config/incus/client.crt")
    machine.succeed("test -f /home/operator/.config/incus/client.key")
    machine.succeed("incus config trust add-certificate /home/operator/.config/incus/client.crt --restricted --projects aginctus-authz")
    machine.succeed(operator + " incus remote add scoped https://127.0.0.1:8443 --accept-certificate --project aginctus-authz", timeout=60)

    # Positive read must work through the TLS remote; direct local Unix
    # administration must remain denied to this principal.
    machine.succeed(operator + " incus list scoped: --project aginctus-authz --format json | jq -e 'length == 0'")
    machine.fail(operator + " incus --force-local project list")
    machine.succeed("incus list --project unrelated --format json | jq -e 'length == 0'")
    machine.succeed("incus project get aginctus-authz restricted | grep -qx true")
    machine.succeed("test $(stat -c '%a' /home/operator/.config/incus/client.key) = 600")

    # Restricted-client positive mutations: only confined project-scoped
    # objects; do not provision privileged instances or host-device mappings.
    machine.succeed(operator + " incus profile create scoped:operator-test --project aginctus-authz")
    machine.succeed(operator + " incus storage volume create scoped:default safe-test --project aginctus-authz")

    # Actual approved shared-bridge/root-disk attachment. Import the
    # immutable Nix-built fixture only via the VM's administrator; a
    # project-restricted principal must be able to use it without possessing
    # admin credentials or reading its key material.
    machine.succeed("incus image import ${workloadImage.config.system.build.metadata}/tarball/* ${workloadImage.config.system.build.tarball}/tarball/* --alias authz-fixture --project aginctus-authz", timeout=180)
    machine.succeed(operator + " incus init scoped:authz-fixture scoped:allowed-instance --project aginctus-authz --storage default --network authz-mgmt", timeout=180)
    attached = machine.succeed("incus config show allowed-instance --project aginctus-authz --expanded")
    assert "authz-mgmt" in attached and "default" in attached, "approved network or root volume was not attached"
    machine.succeed(operator + " incus start scoped:allowed-instance --project aginctus-authz", timeout=180)
    machine.succeed(operator + " incus stop scoped:allowed-instance --project aginctus-authz --force", timeout=180)
    stopped = machine.succeed("incus info allowed-instance --project aginctus-authz")
    assert "STOPPED" in stopped.upper(), "restricted instance lifecycle failed"
    # Starting/stopping legitimately adds volatile runtime state. Compare
    # denial probes against the post-lifecycle configuration, not init state.
    instance_before = machine.succeed("incus config show allowed-instance --project aginctus-authz --expanded")

    # Nonempty positive controls: no image or running container is needed to
    # test API visibility. Only guest root provisions the foreign resources.
    machine.succeed("incus init foreign-sentinel --empty --project unrelated --storage default -c user.authz_sentinel=foreign-only")
    machine.succeed("incus profile create foreign-sentinel --project unrelated")
    machine.succeed("incus profile set foreign-sentinel user.authz_sentinel=foreign-only --project unrelated")
    machine.succeed("incus storage volume create default foreign-sentinel --project unrelated user.authz_sentinel=foreign-only")
    machine.succeed(operator + " incus init scoped:scope-control --empty --project aginctus-authz --storage default -c user.authz_sentinel=allowed-only")
    foreign_before = machine.succeed("incus config show foreign-sentinel --project unrelated --expanded")
    assert "foreign-only" in foreign_before
    assert machine.succeed("incus profile get foreign-sentinel user.authz_sentinel --project unrelated").strip() == "foreign-only"
    assert machine.succeed("incus storage volume get default foreign-sentinel user.authz_sentinel --project unrelated").strip() == "foreign-only"

    # Compare the CLI with the raw server response using the SAME confined
    # TLS identity. No CLI-side filtering, broken transport, or empty foreign
    # project may masquerade as isolation. Root's guest-local API is only an
    # existence control, never evidence that the operator is confined.
    def api_get(path, admin=False):
        if admin:
            curl = "curl --silent --show-error --unix-socket /var/lib/incus/unix.socket"
            url = "http://localhost" + path
        else:
            curl = operator + " curl --silent --show-error --insecure --cert /home/operator/.config/incus/client.crt --key /home/operator/.config/incus/client.key"
            url = "https://127.0.0.1:8443" + path
        output = machine.succeed(curl + " --write-out '\\n%{http_code}' " + shlex.quote(url))
        body, status = output.rsplit("\n", 1)
        response = json.loads(body)
        print("AUTHZ API: " + ("admin " if admin else "operator ") + path + " => HTTP " + status)
        return int(status), response

    def instances(path, admin=False):
        status, response = api_get(path, admin)
        assert status == 200 and response["type"] == "sync", "instance collection request failed"
        return response["metadata"]

    def cli_instances(project):
        return json.loads(machine.succeed(operator + " incus list scoped: --project " + project + " --format json"))

    # Both recursive objects and nonrecursive URL lists must be filtered.
    # The allowed scope must remain populated through both transports.
    for recursion in [0, 1, 2]:
        suffix = "&recursion=" + str(recursion)
        foreign = instances("/1.0/instances?project=unrelated" + suffix, admin=True)
        assert len(foreign) == 1 and "foreign-sentinel" in json.dumps(foreign), "foreign fixture missing from admin API"
        assert instances("/1.0/instances?project=unrelated" + suffix) == [], "foreign instance disclosed by raw API"
        allowed = instances("/1.0/instances?project=aginctus-authz" + suffix)
        assert "scope-control" in json.dumps(allowed) and "foreign-sentinel" not in json.dumps(allowed), "allowed API positive control failed"
        assert allowed == instances("/1.0/instances?project=aginctus-authz" + suffix, admin=True), "allowed API omitted or added instances"
        all_admin = instances("/1.0/instances?all-projects=true" + suffix, admin=True)
        assert "foreign-sentinel" in json.dumps(all_admin) and "scope-control" in json.dumps(all_admin), "all-projects admin positive control failed"
        assert instances("/1.0/instances?all-projects=true" + suffix) == allowed, "all-projects API escaped allowed scope"
    assert cli_instances("unrelated") == [], "foreign instance disclosed by CLI"
    assert "scope-control" in [item["name"] for item in cli_instances("aginctus-authz")], "allowed CLI positive control failed"
    all_cli = json.loads(machine.succeed(operator + " incus list scoped: --all-projects --format json"))
    assert sorted((item["project"], item["name"]) for item in all_cli) == sorted((item["project"], item["name"]) for item in cli_instances("aginctus-authz")), "all-projects CLI escaped allowed scope"
    for admin in [False, True]:
        status, response = api_get("/1.0/instances/foreign-sentinel?project=unrelated", admin)
        if admin:
            assert status == 200 and response["metadata"]["config"]["user.authz_sentinel"] == "foreign-only", "foreign named-resource positive control failed"
        else:
            assert status in [403, 404] and response["type"] == "error" and response["error_code"] == status, "foreign instance readable by name"
    status, response = api_get("/1.0/instances/scope-control?project=aginctus-authz")
    assert status == 200 and response["metadata"]["config"]["user.authz_sentinel"] == "allowed-only", "allowed named-resource positive control failed"
    for path in ["/1.0/profiles/foreign-sentinel?project=unrelated", "/1.0/storage-pools/default/volumes/custom/foreign-sentinel?project=unrelated"]:
        status, response = api_get(path, admin=True)
        assert status == 200 and response["metadata"]["config"]["user.authz_sentinel"] == "foreign-only", "foreign profile/volume positive control failed"
        status, response = api_get(path)
        assert status in [403, 404] and response["type"] == "error" and response["error_code"] == status, "foreign profile/volume readable by raw API"
    print("AUTHZ FILTERING: populated foreign instance visible to admin, absent from operator CLI/raw collections, named read denied; allowed controls visible")

    # Snapshot privileged objects AFTER expected authorized changes; every
    # forbidden attempt below must be refused without modifying host state.
    protected = {
      "project": machine.succeed("incus project show aginctus-authz"),
      "foreign": machine.succeed("incus project show unrelated"),
      "network": machine.succeed("incus network show authz-mgmt"),
      "pools": machine.succeed("incus storage list --format json | jq -S ."),
      "foreign_profiles": machine.succeed("incus profile list --project unrelated --format json | jq -S ."),
      "foreign_volumes": machine.succeed("incus storage volume list default --project unrelated --format json | jq -S ."),
      "daemon": machine.succeed("incus config show"),
    }

    # A test that fails only because its CLI syntax is invalid is NOT valid
    # authorization evidence. Assert a bounded class of remote denial errors.
    failures = []
    def deny(command, allow_hidden=False):
        status, output = machine.execute(operator + " " + command + " 2>&1")
        lower = output.lower()
        # These fixed probes contain no credentials. Preserve all outcomes
        # before failing so one unexpected success cannot hide later probes.
        print("AUTHZ PROBE: " + command + " => exit " + str(status) + ": " + output.strip())
        if status == 0:
            failures.append(command)
            return
        assert "unknown flag" not in lower and "unknown command" not in lower and "usage:" not in lower, "invalid CLI probe, not an authorization test"
        denied = ["not authorized", "not allowed", "permission denied", "does not have permission", "forbidden", "access denied", "restricted", "not permitted"]
        if allow_hidden:
            denied.append("not found")
        assert any(piece in lower for piece in denied), "unexpected failure class in restricted authorization probe"

    deny("incus config show scoped:foreign-sentinel --project unrelated", allow_hidden=True)
    deny("incus profile show scoped:foreign-sentinel --project unrelated", allow_hidden=True)
    deny("incus storage volume show scoped:default foreign-sentinel --project unrelated", allow_hidden=True)
    deny("incus config set scoped:foreign-sentinel user.authz_sentinel=changed --project unrelated", allow_hidden=True)
    deny("incus profile set scoped:foreign-sentinel user.authz_sentinel=changed --project unrelated", allow_hidden=True)
    deny("incus storage volume set scoped:default foreign-sentinel user.authz_sentinel=changed --project unrelated", allow_hidden=True)
    deny("incus project set scoped:aginctus-authz restricted=false")
    deny("incus project create scoped:unauthorized")
    deny("incus network create scoped:escape-bridge ipv4.address=none ipv6.address=none")
    deny("incus storage create scoped:escape-pool dir")
    deny("incus storage volume create scoped:default escape-volume --project unrelated", allow_hidden=True)
    deny("incus profile create scoped:foreign-profile --project unrelated", allow_hidden=True)
    deny("incus config set scoped: core.https_address=0.0.0.0:8443")
    deny("incus config device add scoped:allowed-instance forbidden-host disk source=/etc path=/host --project aginctus-authz")
    deny("incus config set scoped:allowed-instance raw.lxc=lxc.apparmor.profile=unconfined --project aginctus-authz")

    assert machine.succeed("incus project show aginctus-authz") == protected["project"], "project restrictions changed"
    assert machine.succeed("incus project show unrelated") == protected["foreign"], "foreign project changed"
    assert machine.succeed("incus network show authz-mgmt") == protected["network"], "host-owned bridge changed"
    assert machine.succeed("incus storage list --format json | jq -S .") == protected["pools"], "storage pool inventory changed"
    assert machine.succeed("incus profile list --project unrelated --format json | jq -S .") == protected["foreign_profiles"], "foreign profile inventory changed"
    assert machine.succeed("incus config show allowed-instance --project aginctus-authz --expanded") == instance_before, "forbidden device/config mutation changed project instance"
    machine.fail("incus config device show allowed-instance --project aginctus-authz | grep forbidden-host")
    assert machine.succeed("incus storage volume list default --project unrelated --format json | jq -S .") == protected["foreign_volumes"], "foreign volume inventory changed"
    assert machine.succeed("incus config show") == protected["daemon"], "daemon configuration changed"
    assert machine.succeed("incus config show foreign-sentinel --project unrelated --expanded") == foreign_before, "foreign instance changed"
    assert cli_instances("unrelated") == [] and instances("/1.0/instances?project=unrelated&recursion=1") == [], "foreign visibility changed after denial probes"
    machine.fail("incus network show escape-bridge")
    machine.fail("incus storage show escape-pool")
    machine.fail("incus project show unauthorized")

    # Assert effective authorization routing, not only a certificate's
    # advertised project restrictions. Empty means server defaults to the
    # protocol's built-in TLS evaluator; any other custom routing is a gate.
    routing = machine.succeed("incus config get authorization.client.tls-restricted").strip()
    assert routing in ["", "tls"], "restricted-TLS authorization evaluator was overridden"
    print("INCUS AUTHORITY VERSION: " + machine.succeed("incus version").strip() + "; TLS routing: " + repr(routing))
    assert not failures, "restricted principal completed forbidden actions: " + repr(failures)

    # Human-readable evidence never includes private key or cert contents.
    print("INCUS AUTHORITY TEST: confined positive operations, forbidden operations and host invariants checked; version: " + machine.succeed("incus version").strip())
  '';
}
