# Opt-in installed bootstrap, mTLS read and exact-grant CAS exercise. All
# Incus resources and ephemeral credentials live inside the disposable VM.
{
  self,
  nixpkgs,
  system,
}:
let
  pkgs = import nixpkgs { inherit system; };
  cli = self.packages.${system}.aginctus;
  project = "accepted-environments";
  mount = "/var/lib/aginctus-project-state";
  serviceUID = 910;
  incusIP = "10.89.37.1";
  serviceIP = "10.89.37.2";
  guestImage = nixpkgs.lib.nixosSystem {
    inherit system;
    modules = [
      "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
      {
        system.stateVersion = "26.05";
        networking.useDHCP = false;
        networking.interfaces.eth0.ipv4.addresses = [
          {
            address = serviceIP;
            prefixLength = 24;
          }
        ];
        networking.firewall.interfaces.eth0.allowedTCPPorts = [ 8444 ];
        users.groups.aginctusproject.gid = serviceUID;
        users.users.aginctusproject = {
          uid = serviceUID;
          isSystemUser = true;
          group = "aginctusproject";
        };
        environment.systemPackages = [
          cli
          pkgs.util-linux
        ];
        # Deliberately not started until mount, restricted TLS and policy exist.
        systemd.services.aginctus-project-state.serviceConfig = {
          User = "aginctusproject";
          Environment = "AGINCTUS_PROJECT_STATE_READ_SERVICE_CONFIG=/run/aginctus/state.json";
          ExecStart = "${cli}/bin/aginctus serve project-state";
        };
      }
    ];
  };
in
pkgs.testers.runNixOSTest {
  name = "aginctus-environment-project-state-incus";
  nodes.machine = { ... }: {
    system.stateVersion = "26.05";
    networking.nftables.enable = true;
    networking.firewall.interfaces.state-mgmt.allowedTCPPorts = [ 8443 ];
    virtualisation = {
      memorySize = 4096;
      diskSize = 16384;
      cores = 2;
      incus.enable = true;
      incus.package = pkgs.incus;
    };
    users.users.stateoperator = {
      isNormalUser = true;
    };
    environment.systemPackages = [
      cli
      pkgs.incus
      pkgs.jq
      pkgs.openssl
      pkgs.curl
    ];
  };

  testScript = ''
    import hashlib
    import json
    import shlex

    start_all()
    machine.wait_for_unit("incus.service", timeout=60)
    machine.succeed("incus admin init --auto --storage-backend dir")

    # Keys stay in the VM and are never copied into Nix or returned by succeed().
    private = "/run/state-test"
    machine.succeed(f"install -d -m 0700 {private}")

    def certificate(name, extension):
        machine.succeed(f"umask 077 && openssl req -x509 -newkey rsa:2048 -nodes -days 1"
                        f" -subj /CN={name} -addext {shlex.quote(extension)}"
                        f" -keyout {private}/{name}.key -out {private}/{name}.crt >/dev/null 2>&1")

    certificate("incus", "subjectAltName=IP:${incusIP}")
    certificate("state", "subjectAltName=IP:${serviceIP}")
    for name in ("service", "promoter", "member"):
        certificate(name, "extendedKeyUsage=clientAuth")

    machine.succeed("systemctl stop incus.socket incus.service",
                    f"install -m 0600 {private}/incus.key /var/lib/incus/server.key",
                    f"install -m 0644 {private}/incus.crt /var/lib/incus/server.crt",
                    "systemctl start incus.socket incus.service")
    machine.wait_for_unit("incus.service", timeout=60)
    machine.succeed("incus network create state-mgmt ipv4.address=${incusIP}/24 ipv4.dhcp=false ipv4.nat=false ipv6.address=none",
                    "incus config set core.https_address=${incusIP}:8443")
    machine.succeed("incus project create ${project} -c restricted=true -c features.images=true"
                    " -c features.profiles=true -c features.storage.volumes=true -c features.networks=false"
                    " -c user.aginctus.managed=true -c user.aginctus.resource=deployment-project"
                    " -c user.aginctus.deployment=${project} -c restricted.devices.nic=managed"
                    " -c restricted.devices.disk=managed -c restricted.networks.access=state-mgmt"
                    " -c restricted.storage-pools.access=default")
    machine.succeed("incus project create foreign-environment -c features.storage.volumes=true")
    for name in ("service", "promoter", "member"):
        machine.succeed(f"incus config trust add-certificate {private}/{name}.crt --name {name} --restricted --projects ${project}")

    def fingerprint(name):
        return machine.succeed(f"set -o pipefail; openssl x509 -in {private}/{name}.crt -outform DER | sha256sum").split()[0]

    def compact(data):
        return json.dumps(data, separators=(",", ":"))

    def save(path, data):
        machine.succeed(f"umask 077 && printf '%s' {shlex.quote(compact(data))} >{path}")

    binding = {"serverId": "sha256:" + fingerprint("incus"), "project": "${project}"}
    descriptors = [
        {"id": "11111111-1111-4111-8111-111111111111", "name": "development", "binding": binding},
        {"id": "22222222-2222-4222-8222-222222222222", "name": "testing", "binding": binding},
    ]
    enrollment = {"apiVersion": "aginctus.io/environment-enrollment/v1alpha1", "items": descriptors}
    machine.succeed("incus project set ${project} " + shlex.quote("user.aginctus.environments=" + compact(enrollment)))

    def compose(path, data, files):
        # jq gets private bytes directly from VM files, not the driver or argv.
        args = " ".join(f"--rawfile {field} {private}/{file}" for field, file in files.items())
        transform = "." + "".join(f" | .{field} = $" + field for field in files)
        machine.succeed(f"umask 077 && printf '%s' {shlex.quote(compact(data))}"
                        f" | jq {args} {shlex.quote(transform)} >{path}")

    def incus_config():
        return {"apiVersion": "aginctus.dev/restricted-operator-read/v1alpha1",
                "endpoint": "https://${incusIP}:8443", "project": "${project}",
                "trustedServerSha256": binding["serverId"].split(":", 1)[1]}

    def transport_files(name):
        return {"serverCertificatePem": "incus.crt", "clientCertificatePem": name + ".crt",
                "clientPrivateKeyPem": name + ".key"}

    compose(private + "/incus.json", incus_config(), transport_files("service"))
    save(private + "/bootstrap.json", {"apiVersion": "aginctus.dev/project-state-bootstrap/v1alpha1", "binding": binding,
                                      "serviceUid": ${toString serviceUID}, "mountPath": "${mount}",
                                      "operatorConfigFile": "/run/aginctus/incus.json"})
    machine.succeed(f"cat {private}/promoter.crt {private}/member.crt >{private}/peers.pem")
    compose(private + "/state.json", {"apiVersion": "aginctus.dev/project-state-read-service/v1alpha1",
             "project": "${project}", "listenAddress": "${serviceIP}:8444", "mountPath": "${mount}",
             "operatorConfigFile": "/run/aginctus/incus.json", "promotionPolicyFile": "/etc/aginctus/promotion.json"},
            {"serviceCertificatePem": "state.crt", "servicePrivateKeyPem": "state.key", "trustedClientCertificatesPem": "peers.pem"})

    operator_dir = "/home/stateoperator/state"
    machine.succeed(f"install -d -m 0700 -o stateoperator {operator_dir}")
    grants = []
    for i, desc in enumerate(descriptors):
        candidate = {"apiVersion": "aginctus.dev/environment/v1alpha1", "kind": "EnvironmentCandidate",
                     "environment": desc, "selectedWorkloads": [], "workloads": []}
        candidate_path = f"{operator_dir}/candidate-{i}.json"
        save(candidate_path, candidate)
        machine.succeed(f"chown stateoperator {candidate_path}")
        grants.append({"peerSha256": fingerprint("promoter"), **binding, "environmentId": desc["id"],
                       "expectedId": "genesis", "candidateDigest": hashlib.sha256(compact(candidate).encode()).hexdigest()})
        for name in ("promoter", "member"):
            config = {**incus_config(), "stateServiceEndpoint": "https://${serviceIP}:8444",
                      "stateServiceSha256": fingerprint("state"), "configuredCandidateFile": candidate_path}
            path = f"{operator_dir}/{name}-{i}.json"
            compose(path, config, {**transport_files(name), "stateServiceCertificatePem": "state.crt"})
            machine.succeed(f"chown stateoperator {path}")
    save(private + "/promotion.json", {"apiVersion": "aginctus.dev/project-promotion-policy/v1alpha1", "grants": grants})

    machine.succeed("incus image import ${guestImage.config.system.build.metadata}/tarball/* ${guestImage.config.system.build.tarball}/tarball/* --alias state-guest --project ${project}", timeout=180)
    machine.succeed("incus storage volume create default environment-accepted-data --project ${project}"
                    " user.aginctus.managed=true user.aginctus.resource=project-state-volume")
    machine.fail("incus storage volume show default environment-accepted-data --project foreign-environment")
    guest = "timeout 30 incus exec environment-state-service --project ${project} --disable-stdin --"
    owner = guest + " runuser -u aginctusproject --"

    def provision():
        machine.succeed("incus init state-guest environment-state-service --project ${project} --storage default --network state-mgmt"
                        " -c user.aginctus.managed=true -c user.aginctus.resource=project-state-service", timeout=180)
        machine.succeed("incus config device add environment-state-service accepted-data disk pool=default"
                        " source=environment-accepted-data path=${mount} --project ${project}")
        # Admin-side attachment observation is separate from in-guest mount checks.
        attachment = ("${cli}/bin/aginctus internal project-state-attachment"
                      " --project ${project} --instance environment-state-service"
                      " --pool default --volume environment-accepted-data --device accepted-data"
                      " --mount-path ${mount} --network state-mgmt")
        wrong_pin = "sha256:" + ("0" * 64 if binding["serverId"] != "sha256:" + "0" * 64 else "1" * 64)
        machine.fail(f"{attachment} --server-id {wrong_pin}")
        machine.succeed(f"{attachment} --server-id {binding['serverId']}")
        machine.succeed("incus start environment-state-service --project ${project}", timeout=180)
        machine.wait_until_succeeds(guest + " id aginctusproject", timeout=120)
        machine.succeed(guest + " install -d -m 0755 /etc/aginctus /run/aginctus")
        for name, dest, uid, mode in (
            ("bootstrap", "/etc/aginctus/project-state-bootstrap.json", 0, "0644"),
            ("promotion", "/etc/aginctus/promotion.json", 0, "0644"),
            ("incus", "/run/aginctus/incus.json", ${toString serviceUID}, "0600"),
            ("state", "/run/aginctus/state.json", ${toString serviceUID}, "0600"),
        ):
            machine.succeed(f"incus file push {private}/{name}.json environment-state-service{dest}"
                            f" --project ${project} --uid {uid} --gid {uid} --mode {mode}")
        machine.succeed(guest + " chown aginctusproject:aginctusproject ${mount}", guest + " chmod 0700 ${mount}")
        machine.succeed(guest + " test ! -e /var/lib/incus/unix.socket")

    def state_command(i, role, args):
        return (f"timeout 25 runuser -u stateoperator -- env AGINCTUS_RESTRICTED_READ_CONFIG={operator_dir}/{role}-{i}.json"
                f" ${cli}/bin/aginctus --incus-project ${project} --environment {descriptors[i]['id']} state {args} -o json")

    def status(i, role="promoter"):
        return json.loads(machine.succeed(state_command(i, role, "show")))

    def start_service():
        machine.succeed(guest + " systemctl start aginctus-project-state.service")
        machine.wait_until_succeeds(state_command(0, "member", "show"), timeout=60)

    provision()
    machine.fail(guest + " ${cli}/bin/aginctus serve project-state bootstrap")
    assert machine.succeed(owner + " ${cli}/bin/aginctus serve project-state bootstrap").strip() == "initialized"
    start_service()
    assert not status(0, "member")["hasAccepted"], "bootstrap published an accepted revision"
    # Same project membership and working mTLS reads are not a promoter grant.
    machine.fail(state_command(0, "member", "promote --expect genesis"))
    assert not status(0)["hasAccepted"]
    revisions = [json.loads(machine.succeed(state_command(i, "promoter", "promote --expect genesis"))) for i in range(2)]
    assert all(r["generation"] == 1 for r in revisions)
    assert revisions[0]["id"] != revisions[1]["id"]
    machine.fail(state_command(0, "promoter", "promote --expect genesis"))

    def digests():
        paths = ["${mount}/authority.json"]
        for i, r in enumerate(revisions):
            root = f"${mount}/environments/{descriptors[i]['id']}"
            paths.extend([root + "/active.json", root + "/revisions/" + r["id"] + ".json"])
        return machine.succeed(owner + " sha256sum " + " ".join(paths))

    before = digests()
    for i, r in enumerate(revisions):
        assert status(i)["revision"]["id"] == r["id"]

    machine.succeed("incus stop environment-state-service --project ${project} --force", timeout=180)
    machine.succeed("incus delete environment-state-service --project ${project}")
    machine.succeed("incus storage volume show default environment-accepted-data --project ${project}")
    provision()
    assert machine.succeed(owner + " ${cli}/bin/aginctus serve project-state bootstrap").strip() == "reopened"
    start_service()
    for i, r in enumerate(revisions):
        assert status(i)["revision"]["id"] == r["id"]
    assert digests() == before, "retained authority or accepted records changed on recreation"

    # Probe the listener directly: a denied operator CLI alone could mean
    # that its preliminary Incus connection failed before reaching mTLS.
    url = shlex.quote("https://${serviceIP}:8444/v1/accepted?environment=" + descriptors[0]["id"])
    member_read = (f"curl -sS --max-time 10 --cacert {private}/state.crt"
                   f" --cert {private}/member.crt --key {private}/member.key"
                   f" --output /dev/null --write-out '%{{http_code}}' {url}")
    assert machine.succeed(member_read).strip() == "200"
    machine.succeed("incus config trust remove " + fingerprint("member"))
    assert machine.succeed(member_read).strip() == "403"
    assert status(0)["revision"]["id"] == revisions[0]["id"], "revocation blocked the independent promoter read"
    machine.succeed("incus list --project default --format json | jq -e 'length == 0'")
    print("INSTALLED PROJECT STATE: mounted bootstrap, exact-grant mTLS promotion, retained reopen and two histories")
  '';
}
