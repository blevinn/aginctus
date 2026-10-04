[default]
[no-cd]
all: cli herdr-client

[no-cd]
cli:
    go build ./cmd/aginctus

[no-cd]
herdr-client:
    nix build .#herdr-client --out-link result-herdr-client
