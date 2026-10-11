[default]
[no-cd]
all flag="": (go (if flag == "--push" { "--commit" } else { flag })) (nix (if flag == "--push" { "--commit" } else { flag }))
    @if [[ '{{flag}}' == '--push' ]]; then git push; fi

[no-cd]
go flag="":
    @case '{{flag}}' in ''|--commit|--push) ;; *) echo 'Expected --commit or --push' >&2; exit 2 ;; esac
    gofmt -w cmd internal
    @if [[ '{{flag}}' == '--commit' || '{{flag}}' == '--push' ]]; then git add . && git commit -m 'just fmt go'; fi
    @if [[ '{{flag}}' == '--push' ]]; then git push; fi

[no-cd]
nix flag="":
    @case '{{flag}}' in ''|--commit|--push) ;; *) echo 'Expected --commit or --push' >&2; exit 2 ;; esac
    nix fmt
    @if [[ '{{flag}}' == '--commit' || '{{flag}}' == '--push' ]]; then git add . && git commit -m 'just fmt nix'; fi
    @if [[ '{{flag}}' == '--push' ]]; then git push; fi
