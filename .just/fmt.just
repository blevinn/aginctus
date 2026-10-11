[default]
[no-cd]
all flag="":
    gofmt -w cmd internal
    nix fmt
    bash .just/fmt-git.sh "" '{{flag}}'

[no-cd]
go flag="":
    gofmt -w cmd internal
    bash .just/fmt-git.sh go '{{flag}}'

[no-cd]
nix flag="":
    nix fmt
    bash .just/fmt-git.sh nix '{{flag}}'
