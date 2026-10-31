# Explicit test-only import. Never included by the production host module.
{ self, system }:
{ lib, ... }:
let
  pins = builtins.fromJSON (builtins.readFile ../../tests/oci-stage/pins.json);
  arch =
    {
      x86_64-linux = "amd64";
      aarch64-linux = "arm64";
    }
    .${system};
  reference = image: "${image.repository}@${image.platforms.${arch}.manifest}";
  images = builtins.listToAttrs (
    map (image: {
      name = image.name;
      value = reference image;
    }) pins.images
  );
  helper = self.packages.${system}.aginctus.overrideAttrs (_: {
    pname = "aginctus-test-oci-stage";
    subPackages = [ "./tests/oci-stage" ];
    ldflags = [ ];
    postInstall = "";
  });
in
{
  services.aginctus.settings.gateway = {
    compose.project = lib.mkForce "aginctus-gateway";
    images = lib.mapAttrs (_: lib.mkForce) images;
  };
  environment.etc.aginctus-test-oci.text = "disposable-guest-only\n";
  environment.systemPackages = [ helper ];
  # No wantedBy: provisioning is explicit, after guest-only Incus init and
  # loopback listener setup, before gateway up or synthetic inference.
  systemd.services.aginctus-test-oci-stage = {
    description = "Stage immutable test-only gateway OCI images";
    requires = [ "incus.service" ];
    after = [ "incus.service" ];
    serviceConfig = {
      Type = "oneshot";
      ExecStart = "${helper}/bin/oci-stage stage";
      TimeoutStartSec = "11min";
      MemoryMax = "1G";
      CPUQuota = "100%";
      UMask = "0077";
      PrivateTmp = true;
      StateDirectory = "aginctus-test-oci";
      Environment = [
        "HOME=/var/lib/aginctus-test-oci"
        "INCUS_CONF=/var/lib/aginctus-test-oci/incus"
        "INCUS_GLOBAL_CONF=/var/lib/aginctus-test-oci/incus-global"
      ];
    };
  };
}
