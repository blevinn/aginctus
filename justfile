set shell := ["bash", "-euo", "pipefail", "-c"]

fmt:
    gofmt -w cmd internal
    nix fmt

test:
    go test ./...

vet:
    go vet ./...

check: test vet

build:
    go build ./cmd/aginctus
