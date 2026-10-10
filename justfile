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

check: test vet

herdr-client-image-update:
    just build herdr-client
    rootfs="$(find -L result-herdr-client/rootfs -type f -name '*.squashfs' -print -quit)";     metadata="$(find -L result-herdr-client/metadata -type f -name '*.tar.xz' -print -quit)";     test -n "$rootfs";     test -n "$metadata";     bash scripts/update-image-alias.sh aginctus-herdr-client "$metadata" "$rootfs"

opencode-workload-image-update:
    just build opencode-workload
    rootfs="$(find -L result-opencode-workload/rootfs -type f -name '*.squashfs' -print -quit)";     metadata="$(find -L result-opencode-workload/metadata -type f -name '*.tar.xz' -print -quit)";     test -n "$rootfs";     test -n "$metadata";     bash scripts/update-image-alias.sh aginctus-opencode-workload "$metadata" "$rootfs"
