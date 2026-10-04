set shell := ["bash", "-euo", "pipefail", "-c"]

fmt:
    gofmt -w cmd internal
    nix fmt .

test:
    go test ./...

vet:
    go vet ./...

check: test vet

build:
    go build ./cmd/aginctus

herdr-flake-update:
    nix flake update herdr

herdr-client-image-build:
    nix build .#herdr-client --out-link result-herdr-client

herdr-client-image-update: herdr-client-image-build
    rootfs="$(find -L result-herdr-client/rootfs -type f -name '*.squashfs' -print -quit)";     metadata="$(find -L result-herdr-client/metadata -type f -name '*.tar.xz' -print -quit)";     test -n "$rootfs";     test -n "$metadata";     incus image delete aginctus-herdr-client >/dev/null 2>&1 || true;     incus image import "$metadata" "$rootfs" --alias aginctus-herdr-client
