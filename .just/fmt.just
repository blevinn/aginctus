[default]
[no-cd]
default flag="": (all flag)

[no-cd]
all flag="":
    @case '{{flag}}' in ''|--commit|--push) ;; *) echo 'Expected --commit or --push' >&2; exit 2 ;; esac
    @before=$$(git rev-parse HEAD); \
      child_flag='{{flag}}'; \
      if [[ "$$child_flag" == '--push' ]]; then child_flag='--commit'; fi; \
      just fmt go "$$child_flag"; \
      just fmt nix "$$child_flag"; \
      if [[ '{{flag}}' == '--push' && "$$before" != "$$(git rev-parse HEAD)" ]]; then git push; fi

[no-cd]
go flag="":
    @case '{{flag}}' in ''|--commit|--push) ;; *) echo 'Expected --commit or --push' >&2; exit 2 ;; esac
    gofmt -w cmd internal
    @if [[ '{{flag}}' == '--commit' || '{{flag}}' == '--push' ]]; then git add .; if ! git diff --cached --quiet; then git commit -m 'just fmt go'; if [[ '{{flag}}' == '--push' ]]; then git push; fi; fi; fi

[no-cd]
nix flag="":
    @case '{{flag}}' in ''|--commit|--push) ;; *) echo 'Expected --commit or --push' >&2; exit 2 ;; esac
    nix fmt
    @if [[ '{{flag}}' == '--commit' || '{{flag}}' == '--push' ]]; then git add .; if ! git diff --cached --quiet; then git commit -m 'just fmt nix'; if [[ '{{flag}}' == '--push' ]]; then git push; fi; fi; fi
