[no-cd]
nix target:
    case "{{target}}" in       herdr) nix flake update herdr ;;       *) echo "unknown Nix update target: {{target}}" >&2; exit 2 ;;     esac
