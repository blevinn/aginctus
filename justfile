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
    go run ./cmd/aginctus images update aginctus-herdr-client result-herdr-client

opencode-workload-image-update:
    just build opencode-workload
    go run ./cmd/aginctus images update aginctus-opencode-workload result-opencode-workload
