# Opt-in installed bootstrap, mTLS read and exact-grant CAS exercise. All
# Incus resources and ephemeral credentials live inside the disposable VM.
{
  self,
  nixpkgs,
  system,
  gatewayStartup ? false,
}:
let
  pkgs = import nixpkgs { inherit system; };
  cli = self.packages.${system}.aginctus;
  project = "accepted-environments";
  protectedProject = "accepted-protected";
  mount = "/var/lib/aginctus-project-state";
  serviceUID = 910;
  incusIP = "10.89.37.1";
  serviceIP = "10.89.37.2";
  bridgeSocket = "/run/aginctus-workload-bridge.sock";
  hostGrant = "/etc/aginctus-workload-host.json";
  mkGuest =
    address: isState:
    nixpkgs.lib.nixosSystem {
      inherit system;
      modules = [
        "${nixpkgs}/nixos/maintainers/scripts/incus/incus-container-image.nix"
        self.nixosModules.aginctusProjectState
        {
          system.stateVersion = "26.05";
          networking.useDHCP = false;
          networking.interfaces.eth0.ipv4.addresses = [
            {
              inherit address;
              prefixLength = 24;
            }
          ];
          networking.firewall.interfaces.eth0.allowedTCPPorts = [ 8444 ];
          services.aginctus-project-state = {
            enable = isState;
            uid = serviceUID;
          };
          environment.systemPackages = [
            cli
            pkgs.util-linux
          ];
        }
      ];
    };
  guestImage = mkGuest serviceIP true;
  workerImage = mkGuest "10.89.37.3" false;
in
pkgs.testers.runNixOSTest {
  name = "aginctus-environment-project-state-incus";
  nodes.machine = { ... }: {
    system.stateVersion = "26.05";
    networking.nftables.enable = true;
    networking.firewall.interfaces.state-mgmt.allowedTCPPorts = [ 8443 ];
    networking.firewall.interfaces.gateway-mgmt = {
      allowedTCPPorts = [ 53 ];
      allowedUDPPorts = [
        53
        67
      ];
    };
    virtualisation = {
      memorySize = if gatewayStartup then 8192 else 4096;
      diskSize = if gatewayStartup then 32768 else 16384;
      cores = 2;
      incus.enable = true;
      incus.package = pkgs.incus;
    };
    users.users.stateoperator = {
      isNormalUser = true;
      extraGroups = [ "workload-observer" ];
    };
    users.groups.workload-observer = { };
    systemd.sockets.workload-bridge.socketConfig = {
      ListenStream = bridgeSocket;
      Accept = true;
      SocketGroup = "workload-observer";
      SocketMode = "0660";
    };
    systemd.services."workload-bridge@".serviceConfig = {
      ExecStart = "${cli}/bin/aginctus internal workload-bridge-observe ${hostGrant}";
      StandardInput = "socket";
      StandardOutput = "socket";
      StandardError = "journal";
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
    import sys
    from typing import Any

    startup = ${if gatewayStartup then "True" else "False"}
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
    machine.succeed("incus network create state-mgmt ipv4.address=${incusIP}/24 ipv4.dhcp=false ipv4.nat=false ipv6.address=none"
                    " ipv4.routing=false ipv4.dhcp.gateway=none user.aginctus.managed=true user.aginctus.resource=management-network",
                    "incus config set core.https_address=${incusIP}:8443")
    machine.succeed("incus project create ${project} -c restricted=true -c features.images=true"
                    " -c features.profiles=true -c features.storage.volumes=true -c features.networks=false"
                    " -c user.aginctus.managed=true -c user.aginctus.resource=deployment-project"
                    " -c user.aginctus.deployment=${project} -c restricted.devices.nic=managed"
                    " -c restricted.devices.disk=managed -c restricted.networks.access=state-mgmt"
                    " -c restricted.storage-pools.access=default")
    machine.succeed("incus project create ${protectedProject} -c restricted=true -c features.images=true"
                    " -c features.profiles=true -c features.storage.volumes=true -c features.networks=false"
                    " -c user.aginctus.managed=true -c user.aginctus.resource=deployment-project"
                    " -c user.aginctus.deployment=${protectedProject} -c user.aginctus.logical-project=${project}"
                    " -c restricted.devices.nic=managed -c restricted.devices.disk=managed"
                    " -c restricted.networks.access=state-mgmt -c restricted.storage-pools.access=default")
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
                        f" | jq -c {args} {shlex.quote(transform)} >{path}")

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
              "operatorConfigFile": "/run/aginctus/incus.json", "promotionPolicyFile": "/etc/aginctus/promotion.json",
              "executionPolicyFile": "/etc/aginctus/execution.json"},
            {"serviceCertificatePem": "state.crt", "servicePrivateKeyPem": "state.key", "trustedClientCertificatesPem": "peers.pem"})

    operator_dir = "/home/stateoperator/state"
    machine.succeed(f"install -d -m 0700 -o stateoperator {operator_dir}")
    machine.succeed("incus image import ${guestImage.config.system.build.metadata}/tarball/* ${guestImage.config.system.build.tarball}/tarball/* --alias state-guest --project ${protectedProject}", timeout=180)
    machine.succeed("incus image import ${workerImage.config.system.build.metadata}/tarball/* ${workerImage.config.system.build.tarball}/tarball/* --alias worker-guest --project ${project}", timeout=180)
    image = machine.succeed("incus image info worker-guest --project ${project} | awk '/^Fingerprint:/ { print $2 }'").strip()
    state_image = machine.succeed("incus image info state-guest --project ${protectedProject} | awk '/^Fingerprint:/ { print $2 }'").strip()
    gateway_project = "protected-gateway"
    gateway_env = descriptors[1]["id"]
    gateway_base = "ag-" + gateway_env.replace("-", "")
    pg, llm, volume = gateway_base + "-pg", gateway_base + "-llm", gateway_base + "-pgdata"
    gateway_scope = f"--project {gateway_project}"
    gateway_network = "gateway-mgmt" if startup else "state-mgmt"
    if startup:
        machine.succeed("incus network create gateway-mgmt ipv4.address=10.89.38.1/24 ipv4.dhcp=true ipv4.nat=false"
                        " ipv4.routing=false ipv4.dhcp.gateway=none ipv6.address=none dns.mode=managed"
                        " user.aginctus.managed=true user.aginctus.resource=management-network")
    machine.succeed(f"incus project create {gateway_project} -c restricted=true -c features.images=true"
                    " -c features.profiles=true -c features.storage.volumes=true -c features.networks=false"
                    f" -c user.aginctus.managed=true -c user.aginctus.resource=deployment-project -c user.aginctus.deployment={gateway_project}"
                    f" -c user.aginctus.logical-project=${project} -c user.aginctus.protected-purpose=gateway -c user.aginctus.environment-id={gateway_env}"
                    " -c restricted.devices.nic=managed -c restricted.devices.disk=managed"
                    f" -c restricted.networks.access={gateway_network} -c restricted.storage-pools.access=default")
    # Local test images exercise exact Incus fingerprint and attachment observation;
    # the read-only preview does not establish OCI provenance.
    if not startup:
        for alias, source in (("state-guest", "${protectedProject}"), ("worker-guest", "${project}")):
            machine.succeed(f"incus image copy {alias} local: --project {source} --target-project {gateway_project} --alias {alias}", timeout=180)
        machine.succeed(f"incus storage volume create default {volume} {gateway_scope} security.shifted=true"
                        f" user.aginctus.managed=true user.aginctus.resource=gateway-postgres-volume"
                        f" user.aginctus.deployment={gateway_project} user.aginctus.environment-id={gateway_env}")
        for instance, alias, role in ((pg, "worker-guest", "postgres"), (llm, "state-guest", "litellm")):
            machine.succeed(f"incus init {alias} {instance} {gateway_scope} --no-profiles --storage default"
                            f" -c user.aginctus.managed=true -c user.aginctus.resource=gateway-instance -c user.aginctus.role={role}"
                            f" -c user.aginctus.deployment={gateway_project} -c user.aginctus.environment-id={gateway_env}", timeout=180)
            machine.succeed(f"incus config device add {instance} management nic network=state-mgmt name=eth0 ipv4.gateway=none {gateway_scope}")
        machine.succeed(f"incus config device add {pg} database disk pool=default source={volume} path=/var/lib/postgresql/data {gateway_scope}")

    def gateway_image(alias, fingerprint, repository, digest):
        return {"registry": "docker.io", "repository": repository, "architecture": "amd64", "variant": "",
                "manifestDigest": "sha256:" + digest * 64, "configDigest": "sha256:" + "c" * 64,
                "incusFingerprint": fingerprint, "alias": alias}

    def pinned_image(registry, repository, alias, manifest, config, layers):
        return {"registry": registry, "repository": repository, "architecture": "amd64", "variant": "",
                "manifestDigest": "sha256:" + manifest, "configDigest": "sha256:" + config,
                "incusFingerprint": layers, "alias": alias}

    grants = []
    for i, desc in enumerate(descriptors):
        candidate: dict[str, Any] = {"apiVersion": "aginctus.dev/environment/v1alpha1", "kind": "EnvironmentCandidate",
                     "environment": desc, "selectedWorkloads": [], "workloads": []}
        if i == 0:
            candidate["selectedWorkloads"] = ["worker"]
            candidate["workloads"] = [{"id": "worker", "instanceName": "selected-worker", "instanceType": "container",
                                       "imageFingerprint": image, "profiles": [], "ensureRunning": True,
                                       "native": {"runtime": "fixture", "storagePool": "default", "managementNetwork": "state-mgmt"}}]
            candidate["herdr"] = {"instanceName": "fixture-herdr", "imageFingerprint": image,
                                  "storagePool": "default", "managementNetwork": "state-mgmt", "allowStateBootstrap": True}
        else:
            candidate["gateway"] = {"managementNetwork": gateway_network, "egressNetwork": "", "egressIPv4": "",
                                    "rootPool": "default", "databasePool": "default",
                                    "postgres": gateway_image("worker-guest", image, "library/postgres", "a"),
                                    "litellm": gateway_image("state-guest", state_image, "berriai/litellm", "b"),
                                    "credentialVersion": "fixture-v1", "ensureRunning": False}
            if startup:
                candidate["gateway"].update(ensureRunning=True,
                    postgres=pinned_image("docker.io", "library/postgres", "fixture-postgres",
                        "e80acae3f1109e8e4533618b594918936f2858c63afc7204c4773148a673add8",
                        "fa7e2700e906190f900a3c9d47e87f9032cfa38b60d9ac25129a357f85c2da8c",
                        "3b606cf15940ee693400e97b22654e8507a945a7306b96a2e1fd75fbf88295f3"),
                    litellm=pinned_image("ghcr.io", "berriai/litellm", "fixture-litellm",
                        "5e1c40a4f91cb13ae5161b30d09a9874a64d2556283ae766e86ec1edddb9af3e",
                        "afbb38125c0470eb081ff29762d6e2120b1cffeb16df4fb7db25569c44420c6b",
                        "34d7c1f2c6e12d9f7f7fb75963ee914873168ea5b0bab7b973c9d3937aca61ad"))
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

    save(private + "/execution.json", {"apiVersion": "aginctus.dev/project-execution-policy/v1alpha1", "grants": []})
    save(private + "/host.json", {"apiVersion": "aginctus.dev/workload-host-grant/v1alpha1", **binding,
                                 "environmentId": descriptors[0]["id"], "workloadId": "worker", "instance": "selected-worker",
                                 "pool": "default", "bridge": "state-mgmt", "bridgeSocket": "${bridgeSocket}", "ipv4Address": "${incusIP}/24",
                                 "ipv4Nat": False, "ipv4Routing": False, "ipv4DhcpGateway": "none", "ipv6Address": "none"})
    machine.succeed(f"install -m 0644 {private}/host.json ${hostGrant}")
    machine.fail("runuser -u stateoperator -- ${cli}/bin/aginctus internal workload-bridge-observe ${hostGrant}")
    machine.succeed("incus storage volume create default environment-accepted-data --project ${protectedProject}"
                    " user.aginctus.managed=true user.aginctus.resource=project-state-volume user.aginctus.logical-project=${project}")
    machine.fail("incus storage volume show default environment-accepted-data --project foreign-environment")
    guest = "timeout 30 incus exec environment-state-service --project ${protectedProject} --disable-stdin --"
    owner = guest + " runuser -u aginctusproject --"

    def provision():
        machine.succeed("incus init state-guest environment-state-service --project ${protectedProject} --storage default --network state-mgmt"
                        " -c user.aginctus.managed=true -c user.aginctus.resource=project-state-service -c user.aginctus.logical-project=${project}", timeout=180)
        machine.succeed("incus config device add environment-state-service accepted-data disk pool=default"
                        " source=environment-accepted-data path=${mount} --project ${protectedProject}")
        # Admin-side attachment observation is separate from in-guest mount checks.
        attachment = ("${cli}/bin/aginctus internal project-state-attachment"
                      " --project ${protectedProject} --logical-project ${project} --instance environment-state-service"
                      " --pool default --volume environment-accepted-data --device accepted-data"
                      " --mount-path ${mount} --network state-mgmt")
        wrong_pin = "sha256:" + ("0" * 64 if binding["serverId"] != "sha256:" + "0" * 64 else "1" * 64)
        machine.fail(f"{attachment} --server-id {wrong_pin}")
        machine.succeed(f"{attachment} --server-id {binding['serverId']}")
        machine.succeed("incus start environment-state-service --project ${protectedProject}", timeout=180)
        machine.wait_until_succeeds(guest + " id aginctusproject", timeout=120)
        machine.succeed(guest + " install -d -m 0755 /etc/aginctus /run/aginctus")
        for name, dest, uid, mode in (
            ("bootstrap", "/etc/aginctus/project-state-bootstrap.json", 0, "0644"),
            ("promotion", "/etc/aginctus/promotion.json", 0, "0644"),
            ("execution", "/etc/aginctus/execution.json", 0, "0644"),
            ("incus", "/run/aginctus/incus.json", ${toString serviceUID}, "0600"),
            ("state", "/run/aginctus/state.json", ${toString serviceUID}, "0600"),
        ):
            machine.succeed(f"incus file push {private}/{name}.json environment-state-service{dest}"
                            f" --project ${protectedProject} --uid {uid} --gid {uid} --mode {mode}")
        machine.succeed(guest + " chown aginctusproject:aginctusproject ${mount}", guest + " chmod 0700 ${mount}")
        machine.succeed(guest + " test ! -e /var/lib/incus/unix.socket")

    def state_command(i, role, args):
        return (f"timeout 75 runuser -u stateoperator -- env AGINCTUS_RESTRICTED_READ_CONFIG={operator_dir}/{role}-{i}.json"
                f" ${cli}/bin/aginctus --incus-project ${project} --environment {descriptors[i]['id']} state {args} -o json")

    def status(i, role="promoter"):
        return json.loads(machine.succeed(state_command(i, role, "show")))

    def start_service():
        machine.succeed(guest + " systemctl start aginctus-project-state.service")
        machine.wait_until_succeeds(state_command(0, "member", "show"), timeout=60)

    provision()
    # Exercise the actual logical peer against existing protected data-plane objects.
    # No protected-project certificate is installed for any fixture peer.
    for peer in ("service", "promoter", "member"):
        for resource in ("instances/environment-state-service", "storage-pools/default/volumes/custom/environment-accepted-data"):
            path = shlex.quote(f"https://${incusIP}:8443/1.0/{resource}?project=${protectedProject}")
            query = (f"curl -sS --max-time 10 --cacert {private}/incus.crt --cert {private}/{peer}.crt"
                     f" --key {private}/{peer}.key --output /dev/null --write-out '%{{http_code}}' {path}")
            assert machine.succeed(query).strip() == "403", "protected resource API must deny logical peer"
    machine.fail(guest + " systemctl is-active --quiet aginctus-project-state.service")
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
    with subtest("installed protected gateway preview or startup"):
        gateway_grant = {"apiVersion": "aginctus.dev/protected-gateway-grant/v1alpha1", **binding,
                         "environmentId": gateway_env, "revisionId": revisions[1]["id"], "protectedProject": gateway_project}
        grant_file = private + "/gateway-grant.json"
        root_reads = private + "/gateway-reader.json"
        machine.succeed(f"install -m 0600 {operator_dir}/member-1.json {root_reads}")
        preview = f"${cli}/bin/aginctus internal gateway-protected-preview {grant_file} {root_reads}"
        save(grant_file, gateway_grant)
        if startup:
            gateway_dir = f"/var/lib/aginctus/protected-gateway/{gateway_project}/{gateway_env}"
            machine.succeed(f"install -d -m 0700 {gateway_dir}")
            gateway_ops = ("provision", "deliver", "start", "inspect")
            save(private + "/gateway-execution.json", {"apiVersion": "aginctus.dev/project-execution-policy/v1alpha1", "grants": [
                {"peerSha256": fingerprint("member"), **binding, "environmentId": gateway_env,
                 "revisionId": revisions[1]["id"], "workloadId": "gateway-credentials", "operation": f"gateway-{op}"}
                for op in gateway_ops]})
            machine.succeed(f"incus file push {private}/gateway-execution.json environment-state-service/etc/aginctus/execution.json"
                            " --project ${protectedProject} --uid 0 --gid 0 --mode 0644")
            command = "${cli}/bin/aginctus internal gateway-protected-"
            args = f" {grant_file} {root_reads}"
            provisioned = json.loads(machine.succeed(command + "provision" + args, timeout=700))
            assert len(provisioned["result"]["native"]["effects"]) == 7
            repeated = json.loads(machine.succeed(command + "provision" + args, timeout=120))
            assert not repeated["result"]["native"]["effects"]
            machine.succeed(f"incus list {gateway_scope} --format json | jq -e 'length == 2 and all(.[]; .status == \"Stopped\")'")
            machine.succeed(f"incus storage volume get default {volume} user.aginctus.environment-id {gateway_scope} | grep -Fx {gateway_env}")
            machine.fail(f"incus storage volume show default {volume} --project ${project}")
            # The canonical credential source is constructed privately inside the VM.
            machine.succeed(f"umask 077; for key in postgresPassword masterKey saltKey; do"
                            f" (printf sk-; openssl rand -hex 32 | tr -d '\\n') >{private}/$key; done")
            shape = {"apiVersion": "aginctus.dev/protected-gateway-credentials/v1alpha1", "grant": gateway_grant,
                     "version": "fixture-v1", "postgresPassword": "", "masterKey": "", "saltKey": ""}
            secret_files = {key: key for key in ("postgresPassword", "masterKey", "saltKey")}
            compose(gateway_dir + "/credentials.json", {**shape, "version": "wrong-version"}, secret_files)
            machine.fail(command + "publish" + args + f" >{private}/publisher-denial 2>&1")
            compose(gateway_dir + "/credentials.json", shape, secret_files)
            save(grant_file, {**gateway_grant, "revisionId": revisions[0]["id"]})
            machine.fail(command + "publish" + args)
            save(grant_file, gateway_grant)
            publish_status, _ = machine.execute(command + "publish" + args + f" >{private}/publisher-result 2>{private}/publisher-error", timeout=180)
            for guest_name in (pg, llm):
                machine.succeed(f"incus config show {guest_name} {gateway_scope} >{private}/{guest_name}-config")
            for key in secret_files:
                machine.succeed(f"if grep -Flf {private}/{key} {private}/publisher-* {private}/*-config >/dev/null; then exit 1; else test $? -eq 1; fi")
            if publish_status != 0:
                raise AssertionError(machine.succeed(f"cat {private}/publisher-error"))
            published = json.loads(machine.succeed(f"cat {private}/publisher-result"))
            assert published["receipt"]["verified"] and published["receipt"]["version"] == "fixture-v1"
            # An unrelated unrestricted project precludes a private bridge proof.
            machine.fail(command + "start" + args)
            machine.succeed("incus project delete foreign-environment")
            start_status, start_result = machine.execute(command + "start" + args, timeout=200)
            if start_status != 0:
                # Bounded, secret-safe diagnosis inside the disposable VM only.
                # Only fixed labels and exit codes are printed; never the payload.
                print(start_result, file=sys.stderr)
                machine.succeed(f"incus list {gateway_scope} --format json | jq -c '[.[] | {{name, status}}]' >{private}/states.json")
                print(machine.succeed(f"cat {private}/states.json"), file=sys.stderr)
                for probe, label in (
                    (f"incus exec {pg} {gateway_scope} -- getent hosts {pg}", "postgres name resolves in postgres"),
                    (f"incus exec {llm} {gateway_scope} -- getent hosts {pg}", "postgres name resolves in litellm"),
                    (f"incus exec {llm} {gateway_scope} -- test -d /run/aginctus/gateway", "credential directory present"),
                    (f"incus exec {llm} {gateway_scope} -- test -f /proc/1/cmdline", "litellm guest running"),
                    (f"incus exec {pg} {gateway_scope} -- test -f /run/aginctus/gateway/postgres.pgpass", "postgres password file present"),
                ):
                    code, _ = machine.execute(probe)
                    print(f"{label}: exit {code}", file=sys.stderr)
                # Reproduce the LiteLLM launch in the disposable guest with the
                # output captured to a private file, never to the driver log.
                machine.succeed(f"incus file pull {llm}/run/aginctus/gateway/litellm-bootstrap.py {private}/bootstrap-debug.py {gateway_scope}")
                machine.succeed(f"sed -i 's@os.open(\"/dev/null\", os.O_RDWR | os.O_NOFOLLOW)@os.open(\"/tmp/litellm-debug\", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)@' {private}/bootstrap-debug.py")
                machine.succeed(f"incus file push {private}/bootstrap-debug.py {llm}/tmp/bootstrap-debug.py {gateway_scope} --uid 0 --gid 0 --mode 0500")
                machine.succeed(f"incus config set {llm} oci.entrypoint '/app/.venv/bin/python -I -S /tmp/bootstrap-debug.py {pg}' {gateway_scope}")
                machine.execute(f"incus start {llm} {gateway_scope}")
                machine.execute("sleep 30")
                code, _ = machine.execute(f"incus file pull {llm}/tmp/litellm-debug {private}/litellm-debug {gateway_scope}")
                if code == 0:
                    size = machine.succeed(f"wc -c <{private}/litellm-debug").strip()
                    print(f"litellm capture bytes: {size}", file=sys.stderr)
                    for label, pattern in (("database network", "P1001|could not translate host|database server at|Name or service not known"),
                                           ("database auth", "P1000|authentication failed"), ("prisma binaries", "binaries.prisma|prisma.*binary|schema.engine"),
                                           ("runtime option", "No such option|Invalid value"), ("import", "ModuleNotFoundError|ImportError"),
                                           ("local artifact", "Permission denied|FileNotFoundError|No such file")):
                        test, _ = machine.execute(f"grep -iEq {shlex.quote(pattern)} {private}/litellm-debug")
                        if test == 0:
                            print(f"litellm capture class: {label}", file=sys.stderr)
                raise AssertionError("installed startup failed")
            first_start = json.loads(start_result)
            assert first_start["result"]["started"] == [pg, llm]
            assert first_start["result"]["ready"] == [pg, llm]
            repeat_start = json.loads(machine.succeed(command + "start" + args, timeout=320))
            assert not repeat_start["result"].get("started") and repeat_start["result"]["ready"] == [pg, llm]
        observed = json.loads(machine.succeed(preview))
        assert observed["revision"] == revisions[1] and observed["physical"] == {**binding, "project": gateway_project}
        assert observed["databasePresent"] and not observed["writerPin"] and observed["runtime"] == "gated"
        assert observed["imageProvenance"] == "not-verified"
        assert [(i["name"], i["status"]) for i in observed["instances"]] == [(pg, "compatible"), (llm, "compatible")]
        for wrong in ({"revisionId": revisions[0]["id"]}, {"protectedProject": "foreign-environment"}, {"serverId": "sha256:" + "0" * 64}):
            save(grant_file, {**gateway_grant, **wrong})
            machine.fail(preview)
        save(grant_file, gateway_grant)
        machine.succeed(f"chmod 0666 {grant_file}")
        machine.fail(preview)
        machine.succeed(f"chmod 0600 {grant_file}", f"incus config device add {llm} unexpected disk pool=default source={volume} path=/extra {gateway_scope}")
        machine.fail(preview)
        machine.succeed(f"incus config device remove {llm} unexpected {gateway_scope}")
        assert json.loads(machine.succeed(preview)) == observed
    machine.fail(state_command(0, "promoter", "promote --expect genesis"))
    save(private + "/execution.json", {"apiVersion": "aginctus.dev/project-execution-policy/v1alpha1", "grants": [
        {"peerSha256": fingerprint("promoter"), **binding, "environmentId": descriptors[0]["id"],
         "revisionId": revisions[0]["id"], "workloadId": "worker", "operation": action}
         for action in ("reconcile", "stop", "remove")] + [{
         "peerSha256": fingerprint("member"), **binding, "environmentId": descriptors[0]["id"],
         "revisionId": revisions[0]["id"], "workloadId": "worker", "operation": "recover"}]})
    machine.succeed(f"incus file push {private}/execution.json environment-state-service/etc/aginctus/execution.json"
                    " --project ${protectedProject} --uid 0 --gid 0 --mode 0644")
    reconcile = f"reconcile --workload worker --expect {revisions[0]['id']} --host-grant ${hostGrant}"
    machine.fail(state_command(0, "member", reconcile))
    # Missing observer and changed live host policy must both fail before create.
    machine.fail(state_command(0, "promoter", reconcile))
    machine.succeed("systemctl start workload-bridge.socket")
    machine.succeed("incus network set state-mgmt user.aginctus.resource=foreign")
    machine.fail(state_command(0, "promoter", reconcile))
    machine.fail("incus info selected-worker --project ${project}")
    machine.succeed("incus network set state-mgmt user.aginctus.resource=management-network")
    observed = f"plan-native --workload worker --expect {revisions[0]['id']} --host-grant ${hostGrant}"
    plan = json.loads(machine.succeed(state_command(0, "member", observed), timeout=90))
    assert not plan["writerPin"] and plan["revision"] == revisions[0]["id"]
    assert [stage["resourceId"] for stage in plan["result"]["native"]["previews"]] == ["management-network", "worker"]
    assert not plan["result"]["native"]["effects"]
    machine.fail("incus info selected-worker --project ${project}")
    first = json.loads(machine.succeed(state_command(0, "promoter", reconcile), timeout=90))
    first_effects = first["result"]["native"]["effects"]
    assert [effect["effect"]["action"] for effect in first_effects] == ["create", "start"]
    assert all(effect["outcome"] == "applied" for effect in first_effects)
    machine.succeed("incus list selected-worker --project ${project} --format json | jq -e 'length == 1 and .[0].status == \"Running\"'")
    second = json.loads(machine.succeed(state_command(0, "promoter", reconcile), timeout=90))
    assert not second["result"]["native"]["effects"], "repeat reconciliation submitted writes"
    journal = f"${mount}/environments/{descriptors[0]['id']}/native-operations/{first['operationId']}.json"
    machine.succeed(owner + f" cat {journal} | jq -e '.status == \"completed\"'")
    inspect = f"operations --workload worker --revision {revisions[0]['id']}"
    machine.fail(state_command(0, "promoter", inspect))
    inspection_grant = {"peerSha256": fingerprint("member"), **binding,
                        "environmentId": descriptors[0]["id"], "revisionId": revisions[0]["id"],
                        "workloadId": "worker", "operation": "inspect"}
    machine.succeed(f"jq --argjson grant {shlex.quote(compact(inspection_grant))} '.grants += [$grant]'"
                    f" {private}/execution.json >{private}/inspection.json")
    machine.succeed(f"incus file push {private}/inspection.json environment-state-service/etc/aginctus/execution.json"
                    " --project ${protectedProject} --uid 0 --gid 0 --mode 0644")
    listing = json.loads(machine.succeed(state_command(0, "member", inspect)))["operations"]
    assert any(item["id"] == first["operationId"] and not item["unresolved"] for item in listing)
    details = json.loads(machine.succeed(state_command(0, "member", inspect + f" {first['operationId']}")))["operations"]
    assert [effect["action"] for effect in details[0]["effects"]] == ["create", "start"]
    daemon_ids = [effect["daemonOperationId"] for effect in details[0]["effects"]]
    assert len(set(daemon_ids)) == 2 and all(len(identity) == 36 for identity in daemon_ids)
    machine.fail(state_command(0, "member", reconcile))
    incarnation = machine.succeed("incus list selected-worker --project ${project} --format json | jq -er '.[0].created_at'").strip()
    machine.fail(state_command(0, "promoter", reconcile + " --operation stop --instance-created-at 2000-01-01T00:00:00Z"))
    stopped = json.loads(machine.succeed(state_command(0, "promoter", reconcile + f" --operation stop --instance-created-at {incarnation}"), timeout=90))
    assert [effect["effect"]["action"] for effect in stopped["result"]["native"]["effects"]] == ["stop"]
    with subtest("recover an interrupted acknowledged daemon task"):
        coordination = (f"curl -fsS --cacert {private}/state.crt --cert {private}/promoter.crt"
                        f" --key {private}/promoter.key https://${serviceIP}:8444/v1/native-execution"
                        " -H 'Content-Type: application/json'")

        def step(name, operation="reconcile", **fields):
            body = {"apiVersion": "aginctus.dev/project-native-execution/v1alpha1", "step": name,
                    "revision": revisions[0], "workloadId": "worker", "operation": operation, **fields}
            return f"{coordination} --data-binary {shlex.quote(compact(body))}"

        attempt = json.loads(machine.succeed(step("acquire")))["id"]
        recover = f"operations {attempt} --workload worker --revision {revisions[0]['id']} --recover"
        machine.fail(state_command(0, "member", recover))
        machine.fail(state_command(0, "promoter", reconcile))
        machine.succeed(step("begin", id=attempt, action="start", sequence=1))
        daemon = "curl -fsS --unix-socket /var/lib/incus/unix.socket http://localhost"
        update = shlex.quote("/1.0/instances/selected-worker/state?project=${project}")
        submitted = json.loads(machine.succeed(f"{daemon}{update} -X PUT -d '{{\"action\":\"start\"}}'"))
        task = submitted["operation"].split("?")[0]
        assert task.startswith("/1.0/operations/")
        machine.succeed(step("submitted", id=attempt, sequence=1, daemonOperationId=task.rsplit("/", 1)[1]))
        machine.succeed(f"{daemon}{task}/wait?project=${project} | jq -e '.metadata.status == \"Success\"'",
                        step("applied", id=attempt, sequence=1),
                        guest + " systemctl restart aginctus-project-state.service")
        result = json.loads(machine.succeed(state_command(0, "member", recover)))
        assert result["recovered"] and result["operationId"] == attempt
        again = json.loads(machine.succeed(state_command(0, "promoter", reconcile), timeout=90))
        assert not again["result"]["native"]["effects"], "recovery replayed a successful task"
        stop_attempt = json.loads(machine.succeed(step("acquire", operation="stop")))["id"]
        recover_stop = f"operations {stop_attempt} --workload worker --revision {revisions[0]['id']} --recover"
        machine.fail(state_command(0, "member", recover_stop))
        machine.succeed(step("begin", operation="stop", id=stop_attempt, action="stop", sequence=1))
        submitted_stop = json.loads(machine.succeed(f"{daemon}{update} -X PUT -d '{{\"action\":\"stop\",\"force\":true}}'"))
        stop_task = submitted_stop["operation"].split("?")[0]
        assert stop_task.startswith("/1.0/operations/")
        machine.succeed(step("submitted", operation="stop", id=stop_attempt, sequence=1, daemonOperationId=stop_task.rsplit("/", 1)[1]),
                        f"{daemon}{stop_task}/wait?project=${project} | jq -e '.metadata.status == \"Success\"'",
                        step("applied", operation="stop", id=stop_attempt, sequence=1),
                        guest + " systemctl restart aginctus-project-state.service")
        result = json.loads(machine.succeed(state_command(0, "member", recover_stop)))
        assert result["recovered"] and result["operationId"] == stop_attempt
    machine.succeed("incus list selected-worker --project ${project} --format json | jq -e 'length == 1 and .[0].status == \"Stopped\"'")
    removed = json.loads(machine.succeed(state_command(0, "promoter", reconcile + f" --operation remove --instance-created-at {incarnation}"), timeout=90))
    assert [effect["effect"]["action"] for effect in removed["result"]["native"]["effects"]] == ["delete"]
    machine.fail("incus info selected-worker --project ${project}")

    with subtest("installed Herdr planning and authorized identity bootstrap"):
        ops = [{"peerSha256": fingerprint("promoter"), **binding, "environmentId": descriptors[0]["id"],
                "revisionId": revisions[0]["id"], "workloadId": "herdr-client", "operation": op}
               for op in ("herdr-reconcile", "herdr-bootstrap")]
        herdr_policy = private + "/herdr-policy.json"
        target = "environment-state-service/etc/aginctus/execution.json --project ${protectedProject} --uid 0 --gid 0 --mode 0644"
        save(herdr_policy, {"apiVersion": "aginctus.dev/project-execution-policy/v1alpha1", "grants": ops[:1]})
        machine.succeed(f"incus file push {herdr_policy} {target}")
        machine.succeed(f"jq '.workloadId = \"herdr-client\" | .instance = \"fixture-herdr\"' {private}/host.json >{private}/herdr-host.json",
                        f"install -m 0644 {private}/herdr-host.json ${hostGrant}")
        select = f"--herdr --expect {revisions[0]['id']} --host-grant ${hostGrant}"
        preview_herdr = json.loads(machine.succeed(state_command(0, "member", "plan-native " + select + " --bootstrap-herdr-state")))
        assert not preview_herdr["writerPin"] and not preview_herdr["result"]["native"]["effects"]
        machine.fail(state_command(0, "promoter", "reconcile " + select))
        machine.fail(state_command(0, "promoter", "reconcile " + select + " --bootstrap-herdr-state"))
        state_volume = "fixture-herdr-state-system"
        machine.fail(f"incus storage volume show default {state_volume} --project ${project}")
        save(herdr_policy, {"apiVersion": "aginctus.dev/project-execution-policy/v1alpha1", "grants": ops})
        machine.succeed(f"incus file push {herdr_policy} {target}")
        machine.succeed("incus network set state-mgmt user.aginctus.resource=foreign")
        machine.fail(state_command(0, "promoter", "reconcile " + select + " --bootstrap-herdr-state"))
        machine.fail(f"incus storage volume show default {state_volume} --project ${project}")
        machine.succeed("incus network set state-mgmt user.aginctus.resource=management-network")
        boot = json.loads(machine.succeed(state_command(0, "promoter", "reconcile " + select + " --bootstrap-herdr-state")))
        assert [effect["effect"]["action"] for effect in boot["result"]["native"]["effects"]] == ["create", "create", "start"]
        volume_before = machine.succeed(f"incus storage volume show default {state_volume} --project ${project}")
        again = json.loads(machine.succeed(state_command(0, "promoter", "reconcile " + select)))
        assert not again["result"]["native"]["effects"]
        assert machine.succeed(f"incus storage volume show default {state_volume} --project ${project}") == volume_before

    def digests():
        paths = ["${mount}/authority.json"]
        for i, r in enumerate(revisions):
            root = f"${mount}/environments/{descriptors[i]['id']}"
            paths.extend([root + "/active.json", root + "/revisions/" + r["id"] + ".json"])
        return machine.succeed(owner + " sha256sum " + " ".join(paths))

    before = digests()
    for i, r in enumerate(revisions):
        assert status(i)["revision"]["id"] == r["id"]

    machine.succeed("incus stop environment-state-service --project ${protectedProject} --force", timeout=180)
    machine.succeed("incus delete environment-state-service --project ${protectedProject}")
    machine.succeed("incus storage volume show default environment-accepted-data --project ${protectedProject}")
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
    with subtest("exact component grant and live execution boundary"):
        policy = private + "/domain-execution.json"
        target = "environment-state-service/etc/aginctus/execution.json --project ${protectedProject} --uid 0 --gid 0 --mode 0644"
        deliver = {"peerSha256": fingerprint("member"), **binding, "environmentId": gateway_env,
                   "revisionId": revisions[1]["id"], "workloadId": "gateway-credentials", "operation": "gateway-deliver"}

        def install_domain_policy(entries):
            save(policy, {"apiVersion": "aginctus.dev/project-execution-policy/v1alpha1", "grants": entries})
            machine.succeed(f"incus file push {policy} {target}")

        def domain_step(step, **overrides):
            body = {"apiVersion": "aginctus.dev/project-native-execution/v1alpha1", "step": step,
                    "revision": revisions[1], "workloadId": "gateway-credentials", "operation": "gateway-deliver", **overrides}
            return (f"curl -fsS --cacert {private}/state.crt --cert {private}/member.crt --key {private}/member.key"
                    f" https://${serviceIP}:8444/v1/native-execution -H 'Content-Type: application/json' --data-binary {shlex.quote(compact(body))}")

        install_domain_policy([deliver])
        machine.fail(domain_step("acquire", revision=revisions[0]))
        machine.fail(domain_step("acquire", operation="reconcile"))
        attempt = json.loads(machine.succeed(domain_step("acquire")))["id"]
        machine.succeed(domain_step("check", id=attempt))
        machine.fail(domain_step("begin", id=attempt, action="start", sequence=1))
        machine.succeed(domain_step("begin", id=attempt, action="deliver", sequence=1))
        machine.fail(domain_step("submitted", id=attempt, sequence=1, daemonOperationId="ffffffff-ffff-4fff-8fff-ffffffffffff"))
        machine.succeed(domain_step("applied", id=attempt, sequence=1))
        machine.succeed(domain_step("finish", id=attempt, sequence=1, succeeded=True))
        attempt = json.loads(machine.succeed(domain_step("acquire")))["id"]
        machine.succeed(domain_step("begin", id=attempt, action="deliver", sequence=1))
        install_domain_policy([])
        machine.fail(domain_step("check", id=attempt, sequence=1))
        install_domain_policy([deliver])
        machine.succeed(guest + " systemctl restart aginctus-project-state.service")
        machine.wait_until_succeeds(member_read + " | grep -x 200", timeout=30)
        conflict = domain_step("acquire").replace("curl -fsS", "curl -sS -o /dev/null -w '%{http_code}'", 1)
        assert machine.succeed(conflict).strip() == "409"
    machine.succeed("incus config trust remove " + fingerprint("member"))
    assert machine.succeed(member_read).strip() == "403"
    assert status(0)["revision"]["id"] == revisions[0]["id"], "revocation blocked the independent promoter read"
    machine.succeed("incus list --project default --format json | jq -e 'length == 0'")
    print("INSTALLED PROJECT STATE: mounted bootstrap, exact-grant mTLS promotion, retained reopen and two histories")
  '';
}
