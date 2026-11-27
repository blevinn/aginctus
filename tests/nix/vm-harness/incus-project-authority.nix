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
        environment.systemPackages = [ pkgs.util-linux ];
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
      pkgs.openssl
    ];
    users.users.operator = {
      uid = 1000;
      isNormalUser = true;
      home = "/home/operator";
      createHome = true;
      # Deliberately no incus or incus-admin membership: confined TLS only.
      extraGroups = [ ];
    };
    # Independent second principals. They own their own ephemeral keys and are
    # never used to authenticate the reader; they only populate the trust store
    # so the PEER-read projection can be measured with real records.
    users.users.peer = {
      uid = 1001;
      isNormalUser = true;
      home = "/home/peer";
      createHome = true;
      extraGroups = [ ];
    };
    users.users.foreignpeer = {
      uid = 1002;
      isNormalUser = true;
      home = "/home/foreignpeer";
      createHome = true;
      extraGroups = [ ];
    };
    users.users.multipeer = {
      uid = 1003;
      isNormalUser = true;
      home = "/home/multipeer";
      createHome = true;
      extraGroups = [ ];
    };
  };

  testScript = ''
    import base64
    import hashlib
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
    machine.succeed("incus project create unrelated --config restricted=true --config restricted.snapshots=allow --config restricted.backups=allow")
    machine.succeed("incus network create authz-mgmt ipv4.address=none ipv6.address=none")
    # Explicitly bound access to one host-admin managed shared bridge/pool.
    machine.succeed("incus project set aginctus-authz restricted.devices.nic=managed restricted.devices.disk=managed restricted.networks.access=authz-mgmt restricted.storage-pools.access=default")
    machine.succeed("incus project get aginctus-authz restricted | grep -qx true")
    machine.succeed("incus project get aginctus-authz features.networks | grep -qx false")
    machine.succeed("incus project get aginctus-authz restricted.networks.access | grep -qx authz-mgmt")
    machine.succeed("incus project get unrelated restricted | grep -qx true")
    machine.succeed("incus project get unrelated restricted.snapshots | grep -qx allow")
    machine.succeed("incus project get unrelated restricted.backups | grep -qx allow")
    # Only the independent administrator provisions the environment inventory.
    enrollment = json.dumps({
      "apiVersion": "aginctus.io/environment-enrollment/v1alpha1",
      "items": [],
    }, separators=(",", ":"))
    machine.succeed("incus project set aginctus-authz user.aginctus.environments=" + shlex.quote(enrollment))


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
    def api_get(path, admin=False, patch=None, post=None, timeout=15):
        if admin:
            curl = "curl --silent --show-error --unix-socket /var/lib/incus/unix.socket"
            url = "http://localhost" + path
        else:
            curl = operator + " curl --silent --show-error --insecure --cert /home/operator/.config/incus/client.crt --key /home/operator/.config/incus/client.key"
            url = "https://127.0.0.1:8443" + path
        if patch is not None:
            curl += " --request PATCH --header 'Content-Type: application/json' --data " + shlex.quote(json.dumps(patch))
        if post is not None:
            curl += " --request POST --header 'Content-Type: application/json' --data " + shlex.quote(json.dumps(post))
        output = machine.succeed(curl + f" --max-time {timeout} --write-out '\\n%{{http_code}}' " + shlex.quote(url), timeout=timeout + 10)
        body, status = output.rsplit("\n", 1)
        response = json.loads(body)
        print("AUTHZ API: " + ("admin " if admin else "operator ") + path + " => HTTP " + status)
        return int(status), response

    # Project-scoped enrollment must be visible through the SAME restricted
    # identity, but it must not be writable by that identity. Check through
    # the raw API, not merely CLI flags or a local administrator observation.
    status, response = api_get("/1.0/projects/aginctus-authz")
    assert status == 200 and response["metadata"]["config"]["user.aginctus.environments"] == enrollment, "restricted principal cannot read its enrollment manifest"
    status, response = api_get("/1.0/projects/default")
    assert status in [403, 404] and response["type"] == "error", "restricted principal can read the default project"

    # Phase 1 state-service authority feasibility gate: can a genuinely
    # restricted project principal independently verify the current Incus
    # trust record for a presented TLS certificate WITHOUT administrator
    # credentials? A 403/404 is an architectural blocker, not permission to
    # provision the service with unrestricted Incus credentials.
    operator_fp = machine.succeed(
      "openssl x509 -in /home/operator/.config/incus/client.crt" +
      " -noout -fingerprint -sha256"
    ).strip().split("=", 1)[1].replace(":", "").lower()
    assert len(operator_fp) == 64, "operator TLS fingerprint unavailable"
    status, certificate_record = api_get("/1.0/certificates/" + operator_fp)
    assert status == 200 and certificate_record["type"] == "sync", (
      "BLOCKED: restricted project principal cannot read live Incus TLS trust; " +
      "state-service certificate authorizer needs a different non-admin proof"
    )
    trusted = certificate_record["metadata"]
    assert trusted["fingerprint"].lower() == operator_fp and trusted["type"] == "client", "wrong TLS trust record"
    assert trusted["restricted"] is True and trusted["projects"] == ["aginctus-authz"], (
      "restricted project trust permissions are not exact"
    )
    print("PROJECT STATE SERVICE AUTH: live restricted certificate read confirmed")

    # The state-service peer decision is made against the PRESENTED PEER's
    # trust record, not the reader's own record. The self-read above does not
    # exercise that path, so enroll independent second principals and measure
    # the peer-read projection with the SAME restricted reader identity, the
    # SAME HTTP route and the SAME guest-local administrator for enrollment.
    def certificate_fingerprint(path):
        return machine.succeed(
          "openssl x509 -in " + path + " -noout -fingerprint -sha256"
        ).strip().split("=", 1)[1].replace(":", "").lower()

    # Mirrors internal/orchestration/environment/live_incus_peer_authorizer.go:
    # a trust record is accepted only with an exact fingerprint match, client
    # certificate type, restricted flag and EXACTLY one project equal to the
    # reader's own project. Anything else must fail closed.
    def peer_read_accepted(status, response, fingerprint):
        if status != 200 or not isinstance(response, dict) or response.get("type") != "sync":
            return False
        trusted = response.get("metadata")
        if not isinstance(trusted, dict):
            return False
        return (
            trusted.get("fingerprint", "").lower() == fingerprint
            and trusted.get("type") == "client"
            and trusted.get("restricted") is True
            and trusted.get("projects") == ["aginctus-authz"]
        )

    peer = "runuser -u peer -- env HOME=/home/peer USER=peer LOGNAME=peer"
    foreignpeer = "runuser -u foreignpeer -- env HOME=/home/foreignpeer USER=foreignpeer LOGNAME=foreignpeer"
    multipeer = "runuser -u multipeer -- env HOME=/home/multipeer USER=multipeer LOGNAME=multipeer"
    machine.succeed(peer + " incus remote generate-certificate")
    machine.succeed("test -f /home/peer/.config/incus/client.crt")
    machine.succeed(foreignpeer + " incus remote generate-certificate")
    machine.succeed("test -f /home/foreignpeer/.config/incus/client.crt")
    machine.succeed(multipeer + " incus remote generate-certificate")
    machine.succeed("test -f /home/multipeer/.config/incus/client.crt")
    # The administrator imports only the PUBLIC certificates. Every principal
    # stays restricted to exactly one project, except the deliberately
    # multi-project control, which must be rejected by the exactness rule.
    machine.succeed("incus config trust add-certificate /home/peer/.config/incus/client.crt --restricted --projects aginctus-authz")
    machine.succeed("incus config trust add-certificate /home/foreignpeer/.config/incus/client.crt --restricted --projects unrelated")
    machine.succeed("incus config trust add-certificate /home/multipeer/.config/incus/client.crt --restricted --projects aginctus-authz,unrelated")
    peer_fp = certificate_fingerprint("/home/peer/.config/incus/client.crt")
    foreign_fp = certificate_fingerprint("/home/foreignpeer/.config/incus/client.crt")
    multi_fp = certificate_fingerprint("/home/multipeer/.config/incus/client.crt")
    assert len(peer_fp) == 64 and peer_fp != operator_fp, "peer TLS fingerprint unavailable"
    assert len(foreign_fp) == 64 and foreign_fp not in [operator_fp, peer_fp], "foreign peer TLS fingerprint unavailable"
    assert len(multi_fp) == 64 and multi_fp not in [operator_fp, peer_fp, foreign_fp], "multi-project peer TLS fingerprint unavailable"

    # The restricted reader may resolve the PEER's trust record, and the
    # returned record must satisfy the authorizer's exact acceptance rule.
    status, peer_response = api_get("/1.0/certificates/" + peer_fp)
    assert status == 200 and peer_response["type"] == "sync", (
      "BLOCKED: restricted project principal cannot read a peer TLS trust record; " +
      "state-service peer authorizer has no non-admin proof"
    )
    peer_trusted = peer_response["metadata"]
    assert peer_trusted["fingerprint"].lower() == peer_fp, "peer TLS trust record fingerprint mismatch"
    assert peer_trusted["type"] == "client", "peer TLS trust record is not a client certificate"
    assert peer_trusted["restricted"] is True and peer_trusted["projects"] == ["aginctus-authz"], (
      "peer TLS trust record project permissions are not exact"
    )
    # The authorizer also requires the returned PEM to be the presented peer's
    # own certificate bytes, not a different or substituted record.
    peer_pem = peer_trusted["certificate"]
    assert peer_pem.startswith("-----BEGIN CERTIFICATE-----"), "peer TLS trust record is not PEM encoded"
    peer_der = base64.b64decode("".join(peer_pem.strip().splitlines()[1:-1]))
    assert hashlib.sha256(peer_der).hexdigest() == peer_fp, "peer TLS trust record is not the peer's own certificate"
    assert peer_read_accepted(status, peer_response, peer_fp), "peer-read projection rejected an exact single-project peer record"
    print("PROJECT STATE SERVICE PEER AUTH: restricted operator read peer trust record => HTTP 200, exact fingerprint, client type, restricted, projects exactly aginctus-authz")

    # A trust record that is not EXACTLY one project equal to the reader's own
    # project must never be accepted, whether or not Incus discloses it.
    status, foreign_response = api_get("/1.0/certificates/" + foreign_fp)
    foreign_trusted = foreign_response.get("metadata") if isinstance(foreign_response, dict) else None
    foreign_projects = foreign_trusted.get("projects") if isinstance(foreign_trusted, dict) else None
    print("AUTHZ PROJECT SCOPE: foreign-project peer record => HTTP " + str(status) + ", projects " + repr(foreign_projects))
    assert not peer_read_accepted(status, foreign_response, foreign_fp), (
      "a foreign-project trust record was accepted by the peer-read projection"
    )
    if status == 200:
        assert foreign_projects == ["unrelated"], "foreign-project trust record does not carry the foreign project scope"
    else:
        assert isinstance(foreign_response, dict) and foreign_response.get("type") == "error", "foreign-project trust record read did not fail closed"

    status, multi_response = api_get("/1.0/certificates/" + multi_fp)
    multi_trusted = multi_response.get("metadata") if isinstance(multi_response, dict) else None
    multi_projects = multi_trusted.get("projects") if isinstance(multi_trusted, dict) else None
    print("AUTHZ PROJECT SCOPE: multi-project peer record => HTTP " + str(status) + ", projects " + repr(multi_projects))
    assert not peer_read_accepted(status, multi_response, multi_fp), (
      "a second-project trust record was accepted by the peer-read projection"
    )
    if status == 200:
        assert sorted(multi_projects or []) == ["aginctus-authz", "unrelated"], "multi-project trust record does not carry both projects"
    else:
        assert isinstance(multi_response, dict) and multi_response.get("type") == "error", "multi-project trust record read did not fail closed"
    print("AUTHZ PROJECT SCOPE: foreign-project and second-project trust records are not accepted by the exact peer-read projection")

    # An unknown or mismatched fingerprint must not resolve to any record.
    status, response = api_get("/1.0/certificates/" + ("0" * 64))
    assert status in [403, 404] and response["type"] == "error", "unknown TLS fingerprint resolved to a trust record"
    mismatched_fp = peer_fp[:-1] + ("0" if peer_fp[-1] != "0" else "1")
    status, response = api_get("/1.0/certificates/" + mismatched_fp)
    assert status in [403, 404] and response["type"] == "error", "mismatched TLS fingerprint resolved to a trust record"
    print("AUTHZ PROJECT SCOPE: unknown and mismatched fingerprints resolve to no trust record")

    # Revocation of the PEER's trust entry must fail the peer-read closed.
    machine.succeed("incus config trust remove " + shlex.quote(peer_fp))
    status, response = api_get("/1.0/certificates/" + peer_fp)
    assert status in [403, 404] and response["type"] == "error", "removed peer TLS trust record is still readable"
    assert not peer_read_accepted(status, response, peer_fp), "removed peer TLS trust record is still accepted by the peer-read projection"
    print("PROJECT STATE SERVICE PEER REVOCATION: removed peer trust record no longer resolves for the restricted reader")

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
    # A project-restricted client must not rewrite the authoritative
    # enrollment manifest. If Incus allows this in the pinned policy, this
    # test MUST fail and we must choose a different enrollment store.
    status, response = api_get("/1.0/projects/aginctus-authz", patch={
      "config": {"user.aginctus.environments": "forged"}
    })
    assert status in [403, 404] and response["type"] == "error", "restricted client can rewrite enrollment manifest"
    assert machine.succeed("incus project get aginctus-authz user.aginctus.environments").strip() == enrollment, "denied manifest rewrite changed project inventory"

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

    # A second, actually populated project-local image/volume namespace
    # catches accidental fallback to the default project or CLI-only filtering.
    # The VM administrator creates the foreign image; no user has permission
    # to import or list across the project boundary.
    machine.succeed(
      "incus image import ${workloadImage.config.system.build.metadata}/tarball/*" +
      " ${workloadImage.config.system.build.tarball}/tarball/*" +
      " --alias foreign-image --project unrelated",
      timeout=180,
    )
    image_fingerprint = machine.succeed(
      "incus image list --project unrelated --format json" +
      " | jq -er '.[] | select(any(.aliases[]?; .name == \"foreign-image\")) | .fingerprint'"
    ).strip()
    assert len(image_fingerprint) == 64, "foreign image positive control absent"
    assert "foreign-image" in machine.succeed("incus image info foreign-image --project unrelated"), "foreign image not present"
    status, response = api_get("/1.0/images?project=unrelated&recursion=1", admin=True)
    assert status == 200 and image_fingerprint in json.dumps(response["metadata"]), "foreign image admin inventory missing"
    status, response = api_get("/1.0/images?project=unrelated&recursion=1")
    assert (status == 200 and response["type"] == "sync" and response["metadata"] == []) or (
      status == 403 and response["type"] == "error" and response["error_code"] == status
    ), "foreign image disclosed via restricted API"
    status, response = api_get("/1.0/images/" + image_fingerprint + "?project=unrelated")
    assert status in [403, 404] and response["type"] == "error" and response["error_code"] == status, "named foreign image readable"

    # An operator must be able to attach an explicitly allowed project-local
    # managed volume; this is NOT permission for arbitrary host filesystems.
    # Keep a post-attachment snapshot so a denied foreign alias cannot mutate
    # the allowed instance or make a missing volume appear to be accessible.
    machine.succeed(
      operator + " incus config device add scoped:allowed-instance data disk" +
      " pool=default source=safe-test path=/mnt/safe --project aginctus-authz"
    )
    allowed_volume = machine.succeed(
      "incus config show allowed-instance --project aginctus-authz --expanded"
    )
    assert "safe-test" in allowed_volume and "/mnt/safe" in allowed_volume, "project-local managed volume not attached"
    machine.succeed(operator + " incus start scoped:allowed-instance --project aginctus-authz", timeout=180)
    machine.wait_until_succeeds(operator + " incus exec scoped:allowed-instance --project aginctus-authz -- /run/current-system/sw/bin/mountpoint -q /mnt/safe", timeout=60)
    machine.succeed(operator + " incus stop scoped:allowed-instance --project aginctus-authz --force", timeout=180)
    # Runtime volatile keys can change during the allowed start/stop.
    allowed_volume = machine.succeed("incus config show allowed-instance --project aginctus-authz --expanded")
    status, response = api_get(
      "/1.0/storage-pools/default/volumes/custom/safe-test?project=aginctus-authz"
    )
    assert status == 200 and response["type"] == "sync", "allowed custom volume not visible"
    status, response = api_get(
      "/1.0/storage-pools/default/volumes/custom/foreign-sentinel?project=unrelated"
    )
    assert status in [403, 404] and response["type"] == "error", "foreign custom volume readable"
    # A foreign-only alias cannot be interpreted as a same-project volume.
    deny("incus config device add scoped:allowed-instance foreign-data disk pool=default source=foreign-sentinel path=/mnt/foreign --project aginctus-authz", allow_hidden=True)
    assert not failures, "foreign volume attachment unexpectedly succeeded: " + repr(failures)
    assert machine.succeed(
      "incus config show allowed-instance --project aginctus-authz --expanded"
    ) == allowed_volume, "failed foreign-volume attach mutated allowed instance"
    assert machine.succeed(
      "incus storage volume get default foreign-sentinel user.authz_sentinel --project unrelated"
    ).strip() == "foreign-only", "foreign volume changed during attachment attempt"
    print("INCUS PROJECT STORAGE: permitted scoped custom volume attachment; foreign image and volume invisible/denied to restricted TLS client")

    # Feasibility probe for protected placement. Workload TLS must get an
    # authorization denial for real protected guest and backup endpoints, not
    # a CLI preflight failure or a lookup against an empty project.
    protected_guest = "incus --project unrelated"
    machine.succeed(f"{protected_guest} init foreign-image protected-service --storage default", timeout=180)
    machine.succeed(f"{protected_guest} config device add protected-service state disk pool=default source=foreign-sentinel path=/mnt/protected")
    machine.succeed(f"{protected_guest} start protected-service", timeout=180)
    machine.wait_until_succeeds(f"{protected_guest} exec protected-service -- test -d /mnt/protected", timeout=60)
    machine.succeed(f"{protected_guest} exec protected-service -- touch /mnt/protected/synthetic-marker")
    machine.succeed(f"{protected_guest} file pull protected-service/mnt/protected/synthetic-marker /tmp/protected-marker")
    gi = "/1.0/instances/protected-service"
    gv = "/1.0/storage-pools/default/volumes/custom/foreign-sentinel"
    protected_project = "?project=unrelated"

    def protected_denial(path, post=None, patch=None):
        status, response = api_get(path, post=post, patch=patch)
        assert status == 403 and response["type"] == "error" and response["error_code"] == 403, f"expected daemon authorization denial for {path}: {status}"

    def wait_for_admin_task(status, reply, project_query):
        assert status == 202 and reply["operation"].startswith("/1.0/operations/")
        wait_path = reply["operation"].split("?", 1)[0] + "/wait" + project_query
        status, reply = api_get(wait_path, admin=True, timeout=120)
        assert status == 200 and reply["metadata"]["status_code"] == 200

    protected_denial(gi + "/exec" + protected_project, post={"command": ["true"], "wait-for-websocket": False})
    protected_denial(gi + "/files" + protected_project + "&path=/mnt/protected/synthetic-marker")
    protected_denial(gi + protected_project, patch={"config": {"user.authz_sentinel": "forbidden"}})
    protected_denial(gi + protected_project, patch={"devices": {"state": {"type": "none"}}})
    protected_denial("/1.0/profiles/foreign-sentinel" + protected_project, patch={"config": {"user.authz_sentinel": "forbidden"}})
    machine.succeed(f"{protected_guest} stop protected-service --force", timeout=180)
    for path in (gi, gv):
        protected_denial(path + "/snapshots" + protected_project, post={"name": "blocked"})
        protected_denial(path + "/backups" + protected_project, post={"name": "blocked"})
    machine.succeed(f"{protected_guest} snapshot create protected-service protected-snap")
    machine.succeed(f"{protected_guest} storage volume snapshot create default foreign-sentinel protected-snap")
    for path in (gi, gv):
        snap = path + "/snapshots/protected-snap" + protected_project
        assert api_get(snap, admin=True)[0] == 200
        protected_denial(snap)
    # Keep named backup positive controls long enough to check export.
    for path in (gi, gv):
        status, reply = api_get(path + "/backups" + protected_project, admin=True, post={"name": "protected-backup"})
        wait_for_admin_task(status, reply, protected_project)
        backup = path + "/backups/protected-backup"
        assert api_get(backup + protected_project, admin=True)[0] == 200
        export = backup + "/export" + protected_project
        machine.succeed("curl --fail --silent --show-error --max-time 90 --output /tmp/protected-backup"
                        " --unix-socket /var/lib/incus/unix.socket " + shlex.quote("http://localhost" + export))
        machine.succeed("test -s /tmp/protected-backup")
        protected_denial(export)

    workload_project = "?project=aginctus-authz"
    copy = {
      "name": "denied-protected-copy", "type": "container",
      "source": {"type": "copy", "source": "protected-service", "project": "unrelated", "instance_only": True},
      # Clear the inherited custom device. The target project cannot resolve
      # that volume; an invalid target must not masquerade as denied source access.
      "devices": {"state": {"type": "none"}},
    }
    protected_denial("/1.0/instances" + workload_project, post=copy)
    machine.succeed(f"{protected_guest} storage volume show default foreign-sentinel | grep -F protected-service")
    machine.fail("incus info denied-protected-copy --project aginctus-authz")
    status, reply = api_get("/1.0/instances" + workload_project, admin=True, post=copy)
    wait_for_admin_task(status, reply, workload_project)
    machine.succeed("incus delete denied-protected-copy --project aginctus-authz")
    print("INCUS PROTECTED PLACEMENT: protected data-plane probes use real admin-observed resources")

    # Revocation and re-enrollment must be enforced by Incus, not by a
    # cached CLI project selector. Establish a positive control on the same
    # client certificate and HTTP route before revoking its trust entry.
    client_fingerprint = machine.succeed(
      "openssl x509 -in /home/operator/.config/incus/client.crt -outform DER | sha256sum"
    ).split()[0]
    trusted = json.loads(machine.succeed("incus config trust list --format json"))
    matching = [cert for cert in trusted if cert.get("fingerprint", "").lower() == client_fingerprint]
    assert len(matching) == 1, "test client certificate missing from trust store"
    assert matching[0].get("restricted") is True and matching[0].get("projects") == ["aginctus-authz"], "unexpected test principal authority"
    assert "scope-control" in json.dumps(instances("/1.0/instances?project=aginctus-authz&recursion=1")), "pre-revocation positive control missing"
    control_path = "/1.0/instances/scope-control?project=aginctus-authz"
    status, response = api_get(control_path)
    assert status == 200 and response["metadata"]["config"]["user.authz_sentinel"] == "allowed-only", "named read positive control missing"
    machine.succeed(operator + " incus config set scoped:scope-control user.revocation_control=cli-before --project aginctus-authz")
    status, response = api_get(control_path, patch={"config": {"user.revocation_control": "before"}})
    assert status == 200 and response["type"] == "sync", "pre-revocation write positive control failed"
    control_before = machine.succeed("incus config show scope-control --project aginctus-authz --expanded")
    assert machine.succeed("incus config get scope-control user.revocation_control --project aginctus-authz").strip() == "before"

    # Newly authenticated resource requests must lose authority after revocation.
    # This does not cancel previously accepted asynchronous operations or test
    # a long-lived authenticated connection.
    # The test server itself remains healthy and admin-owned fixtures persist.
    machine.succeed("incus config trust remove " + shlex.quote(client_fingerprint))
    machine.wait_until_succeeds(
      operator + " curl --silent --show-error --max-time 15 --insecure" +
      " --cert /home/operator/.config/incus/client.crt" +
      " --key /home/operator/.config/incus/client.key" +
      " --output /dev/null --write-out '%{http_code}'" +
      " 'https://127.0.0.1:8443/1.0/instances?project=aginctus-authz&recursion=1' | grep -qx 403",
      timeout=60,
    )
    # After revocation Incus's public /1.0 response omits API extensions, so
    # the CLI may fail preflight (e.g. missing api_filtering), not authorization.
    # CLI failure is supplemental only; raw named GET/PATCH below prove denial.
    machine.fail(operator + " incus list scoped: --project aginctus-authz --format json")
    for patch in [None, {"config": {"user.revocation_control": "forbidden"}}]:
        status, response = api_get(control_path, patch=patch)
        assert status == 403 and response["type"] == "error" and response["error_code"] == status, "revoked certificate retained named read/write authority"
    machine.fail(operator + " incus config set scoped:scope-control user.revocation_control=forbidden --project aginctus-authz")
    assert machine.succeed("incus config show scope-control --project aginctus-authz --expanded") == control_before, "revoked write changed the allowed resource"
    assert "scope-control" in json.dumps(instances("/1.0/instances?project=aginctus-authz&recursion=1", admin=True)), "daemon or instance unavailable after revocation"
    assert "foreign-sentinel" in json.dumps(instances("/1.0/instances?project=unrelated&recursion=1", admin=True)), "foreign resources changed during revocation"

    # Recovery remains an independently authorized administrator action.
    # Re-add ONLY the project-scoped public certificate and verify previous
    # project confinement persists rather than granting full-server access.
    machine.succeed(
      "incus config trust add-certificate /home/operator/.config/incus/client.crt" +
      " --restricted --projects aginctus-authz"
    )
    machine.wait_until_succeeds(
      operator + " incus list scoped: --project aginctus-authz --format json" +
      " | jq -e 'any(.[]; .name == \"scope-control\")'",
      timeout=60,
    )
    status, response = api_get(control_path, patch={"config": {"user.revocation_control": "re-enrolled"}})
    assert status == 200 and response["type"] == "sync", "re-enrolled scoped write did not recover"
    assert machine.succeed("incus config get scope-control user.revocation_control --project aginctus-authz").strip() == "re-enrolled"
    assert instances("/1.0/instances?project=unrelated&recursion=1") == [], "revived client gained foreign project visibility"
    status, response = api_get("/1.0/instances/foreign-sentinel?project=unrelated")
    assert status == 403 and response["type"] == "error", "re-enrolled credential gained foreign read"
    deny("incus config set scoped:foreign-sentinel user.authz_sentinel=changed --project unrelated", allow_hidden=True)
    assert machine.succeed("incus config show foreign-sentinel --project unrelated --expanded") == foreign_before, "re-enrollment changed foreign resources"
    assert not failures, "revoked or re-enrolled principal completed forbidden actions: " + repr(failures)
    print("INCUS AUTHORITY REVOCATION: restricted credential denied after removal; scoped permissions restored only after administrator re-enrollment")

    # Human-readable evidence never includes private key or cert contents.
    print("INCUS AUTHORITY TEST: confined positive operations, forbidden operations and host invariants checked; version: " + machine.succeed("incus version").strip())
  '';
}
