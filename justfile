set shell := ["bash", "-euo", "pipefail", "-c"]

fmt:
    gofmt -w cmd internal
    nix fmt

test:
    go test ./...

vet:
    go vet ./...

check: test vet

build target="aginctus":
    case "{{target}}" in       aginctus) go build ./cmd/aginctus ;;       herdr-client) nix build .#herdr-client --out-link result-herdr-client ;;       *) echo "unknown build target: {{target}}" >&2; exit 2 ;;     esac

update ecosystem target:
    case "{{ecosystem}}:{{target}}" in       nix:herdr) nix flake update herdr ;;       *) echo "unknown update target: {{ecosystem}} {{target}}" >&2; exit 2 ;;     esac

herdr-client-image-update: (build "herdr-client")
    rootfs="$(find -L result-herdr-client/rootfs -type f -name '*.squashfs' -print -quit)";     metadata="$(find -L result-herdr-client/metadata -type f -name '*.tar.xz' -print -quit)";     test -n "$rootfs";     test -n "$metadata";     incus image delete aginctus-herdr-client >/dev/null 2>&1 || true;     incus image import "$metadata" "$rootfs" --alias aginctus-herdr-client
