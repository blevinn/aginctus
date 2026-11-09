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
    environment.systemPackages = [ pkgs.incus pkgs.jq pkgs.coreutils pkgs.util-linux pkgs.curl ];
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
    start_all()
    machine.wait_for_unit("incus.service")
    # The test driver supplies an independent disposable QEMU disk and Incus
    # daemon. Storage is initialized inside the guest, never on the host.
    machine.succeed("incus admin init --auto --storage-backend dir")
    machine.succeed("incus info")
    machine.succeed("incus version")
    machine.succeed("incus project create aginctus-authz --config restricted=true --config features.images=true --config features.profiles=true --config features.storage.volumes=true --config features.networks=false")
    machine.succeed("incus project create unrelated --config restricted=true")
    machine.succeed("incus network create aginctus-authz-mgmt ipv4.address=none ipv6.address=none")
    # Explicitly bound access to one host-admin managed shared bridge/pool.
    machine.succeed("incus project set aginctus-authz restricted.devices.nic=managed restricted.networks.access=aginctus-authz-mgmt restricted.storage-pools.access=default")
    machine.succeed("incus project get aginctus-authz restricted | grep -qx true")
    machine.succeed("incus project get aginctus-authz features.networks | grep -qx false")
    machine.succeed("incus project get aginctus-authz restricted.networks.access | grep -qx aginctus-authz-mgmt")

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
    machine.succeed(operator + " incus init scoped:authz-fixture scoped:allowed-instance --project aginctus-authz --storage default --network aginctus-authz-mgmt", timeout=180)
    instance_before = machine.succeed("incus config show allowed-instance --project aginctus-authz --expanded")
    assert "aginctus-authz-mgmt" in instance_before and "default" in instance_before, "approved network or root volume was not attached"
    machine.succeed(operator + " incus start scoped:allowed-instance --project aginctus-authz", timeout=180)
    machine.succeed(operator + " incus stop scoped:allowed-instance --project aginctus-authz --force", timeout=180)
    stopped = machine.succeed("incus info allowed-instance --project aginctus-authz")
    assert "STOPPED" in stopped.upper(), "restricted instance lifecycle failed"

    # Snapshot privileged objects AFTER expected authorized changes; every
    # forbidden attempt below must be refused without modifying host state.
    protected = {
      "project": machine.succeed("incus project show aginctus-authz"),
      "foreign": machine.succeed("incus project show unrelated"),
      "network": machine.succeed("incus network show aginctus-authz-mgmt"),
      "pools": machine.succeed("incus storage list --format json | jq -S ."),
      "foreign_profiles": machine.succeed("incus profile list --project unrelated --format json | jq -S ."),
    }

    # A test that fails only because its CLI syntax is invalid is NOT valid
    # authorization evidence. Assert a bounded class of remote denial errors.
    def deny(command):
        status, output = machine.execute(operator + " " + command + " 2>&1")
        lower = output.lower()
        assert status != 0, "restricted principal completed a forbidden action"
        assert "unknown flag" not in lower and "unknown command" not in lower and "usage:" not in lower, "invalid CLI probe, not an authorization test"
        assert any(piece in lower for piece in ["not authorized", "not allowed", "permission denied", "forbidden", "access denied", "not found", "restricted"]), "unexpected failure class in restricted authorization probe"

    deny("incus list scoped: --project unrelated --format json")
    deny("incus project set scoped:aginctus-authz restricted=false")
    deny("incus project create scoped:unauthorized")
    deny("incus network create scoped:escape-bridge ipv4.address=none ipv6.address=none")
    deny("incus storage create scoped:escape-pool dir")
    deny("incus storage volume create scoped:default escape-volume --project unrelated")
    deny("incus profile create scoped:foreign-profile --project unrelated")
    deny("incus config set scoped:core.https_address 0.0.0.0:8443")
    deny("incus config device add scoped:allowed-instance forbidden-host disk source=/etc path=/host --project aginctus-authz")
    deny("incus config set scoped:allowed-instance raw.lxc=lxc.apparmor.profile=unconfined --project aginctus-authz")

    assert machine.succeed("incus project show aginctus-authz") == protected["project"], "project restrictions changed"
    assert machine.succeed("incus project show unrelated") == protected["foreign"], "foreign project changed"
    assert machine.succeed("incus network show aginctus-authz-mgmt") == protected["network"], "host-owned bridge changed"
    assert machine.succeed("incus storage list --format json | jq -S .") == protected["pools"], "storage pool inventory changed"
    assert machine.succeed("incus profile list --project unrelated --format json | jq -S .") == protected["foreign_profiles"], "foreign profile inventory changed"
    assert machine.succeed("incus config show allowed-instance --project aginctus-authz --expanded") == instance_before, "forbidden device/config mutation changed project instance"
    machine.fail("incus config device show allowed-instance --project aginctus-authz | grep forbidden-host")
    machine.fail("incus network show escape-bridge")
    machine.fail("incus storage show escape-pool")
    machine.fail("incus project show unauthorized")

    # Assert effective authorization routing, not only a certificate's
    # advertised project restrictions. Empty means server defaults to the
    # protocol's built-in TLS evaluator; any other custom routing is a gate.
    routing = machine.succeed("incus config get authorization.client.tls-restricted").strip()
    assert routing in ["", "tls"], "restricted-TLS authorization evaluator was overridden"

    # Human-readable evidence never includes private key or cert contents.
    print("INCUS AUTHORITY TEST: confined positive operations, forbidden operations and host invariants checked; version: " + machine.succeed("incus version").strip())
  '';
}
