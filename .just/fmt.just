[default]
[no-cd]
default flag="": (all flag)

[private]
[no-cd]
validate flag="":
    @case '{{flag}}' in ''|--commit|--push) ;; *) echo 'Expected --commit or --push' >&2; exit 2 ;; esac

[no-cd]
all flag="": (validate flag) (go (if flag == "--push" { "--commit" } else { flag })) (nix (if flag == "--push" { "--commit" } else { flag }))
    @if [[ '{{flag}}' == '--push' ]] && git rev-parse --verify '@{upstream}' >/dev/null 2>&1 && [[ -n "$(git log '@{upstream}..HEAD' --format=%s --grep='^just fmt \(go\|nix\)$')" ]]; then git push; fi

[no-cd]
go flag="": (validate flag)
    gofmt -w cmd internal
    @if [[ '{{flag}}' == '--commit' || '{{flag}}' == '--push' ]]; then git add .; if ! git diff --cached --quiet; then git commit -m 'just fmt go'; if [[ '{{flag}}' == '--push' ]]; then git push; fi; fi; fi

[no-cd]
nix flag="": (validate flag)
    nix fmt
    @if [[ '{{flag}}' == '--commit' || '{{flag}}' == '--push' ]]; then git add .; if ! git diff --cached --quiet; then git commit -m 'just fmt nix'; if [[ '{{flag}}' == '--push' ]]; then git push; fi; fi; fi
