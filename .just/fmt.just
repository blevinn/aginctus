[default]
[no-cd]
all: go nix

[no-cd]
go:
    gofmt -w cmd internal

[no-cd]
nix:
    nix fmt
