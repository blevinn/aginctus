set shell := ["bash", "-euo", "pipefail", "-c"]

import '.just/build.just'

fmt:
    gofmt -w cmd internal
    nix fmt

test:
    go test ./...

vet:
    go vet ./...

check: test vet
