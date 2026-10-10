set shell := ["bash", "-euo", "pipefail", "-c"]

mod build '.just/build.just'
mod update '.just/update.just'

fmt:
    gofmt -w cmd internal
    nix fmt

test:
    go test ./...

vet:
    go vet ./...

check: test vet modules

modules:
    bash scripts/check-go-modules.sh

herdr-client-image-update:
    just build herdr-client
    rootfs="$(find -L result-herdr-client/rootfs -type f -name '*.squashfs' -print -quit)";     metadata="$(find -L result-herdr-client/metadata -type f -name '*.tar.xz' -print -quit)";     test -n "$rootfs";     test -n "$metadata";     incus image delete aginctus-herdr-client >/dev/null 2>&1 || true;     incus image import "$metadata" "$rootfs" --alias aginctus-herdr-client

opencode-workload-image-update:
    just build opencode-workload
    rootfs="$(find -L result-opencode-workload/rootfs -type f -name '*.squashfs' -print -quit)";     metadata="$(find -L result-opencode-workload/metadata -type f -name '*.tar.xz' -print -quit)";     test -n "$rootfs";     test -n "$metadata";     incus image delete aginctus-opencode-workload >/dev/null 2>&1 || true;     incus image import "$metadata" "$rootfs" --alias aginctus-opencode-workload
