package cli

import (
	"fmt"
	"io"
)

// Help is resolved before configuration loading so it remains usable offline.
func runHelp(args []string, stdout, stderr io.Writer) (int, bool) {
	var command string
	switch {
	case args[0] == "help":
		if len(args) > 2 {
			fmt.Fprintln(stderr, "Usage: aginctus help [command]")
			return 2, true
		}
		if len(args) == 2 {
			command = args[1]
			if command == "--help" || command == "-h" {
				command = "help"
			}
		}
	case len(args) == 1 && (args[0] == "--help" || args[0] == "-h"):
	case len(args) == 2 && (args[1] == "--help" || args[1] == "-h"):
		command = args[0]
	default:
		return 0, false
	}
	switch command {
	case "":
		printUsage(stdout)
	case "apply":
		printApplyUsage(stdout)
	case "gateway":
		printGatewayUsage(stdout)
	case "network":
		printNetworkUsage(stdout)
	case "herdr":
		printHerdrClientUsage(stdout)
	case "workload":
		printWorkloadUsage(stdout)
	case "images":
		fmt.Fprintln(stdout, "Usage: aginctus [global options] images list|ls\n       aginctus [global options] images list|ls --project NAME (read-only; owned local project)\n       aginctus [global options] images import --project NAME ALIAS PATH --confirm-create (create-only; owned local project)\n       aginctus [global options] images owner-audit --project NAME --owner system|user ALIAS (read-only; NOT write authorization)\n       aginctus [global options] images import-owned --project NAME --owner system|user ALIAS PATH --confirm-create (create-only; NOT authority proof)\n       aginctus [global options] images update NAME PATH (legacy v0.1 project)")
	case "config":
		fmt.Fprintln(stdout, "Usage: aginctus [global options] config show\n       aginctus [global options] config get <path>")
	case "testenv":
		printTestenvUsage(stdout)
	case "doctor":
		fmt.Fprintln(stdout, "Usage: aginctus [global options] doctor [--fix]  (read-only unless --fix; may configure loopback HTTPS)")
	case "operator":
		fmt.Fprintln(stdout, "Usage: aginctus [global options] operator preflight --project NAME (read-only; write and host authority unverified)")
		fmt.Fprintln(stdout, "       aginctus [global options] operator state-audit --owner system|user (read-only; no secret reads or migration)")
		fmt.Fprintln(stdout, "       aginctus operator units-audit --owner system|user (read-only; current user's manager only; no config options)")
		fmt.Fprintln(stdout, "       aginctus operator package-audit --owner system|user (read-only; declared Nix package parity)")
		fmt.Fprintln(stdout, "       aginctus operator project-plan --project NAME (read-only; never ambient/default project)")
		fmt.Fprintln(stdout, "       aginctus operator project-ensure --project NAME --confirm-create (local project create-only; explicit mutation)")
	case "version":
		fmt.Fprintln(stdout, "Usage: aginctus version|--version\nRevision: packaged clean Git commit or source-<content hash>; local Go VCS revision[-dirty], or unknown when unavailable (including go run). No timestamps.")
	case "help":
		fmt.Fprintln(stdout, "Usage: aginctus help [command]")
	case "status", "remove":
		fmt.Fprintf(stdout, "Usage: aginctus [global options] %s\n", command)
	default:
		fmt.Fprintf(stderr, "unknown help command %q\n", command)
		return 2, true
	}
	return 0, true
}

func printApplyUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aginctus [global options] apply [--render | --from FILE | --from=FILE]")
	fmt.Fprintln(w, "  apply deploys configured orchestration (mutation). --render prints a deterministic JSON plan (offline, no mutation).")
	fmt.Fprintln(w, "  --from accepts a YAML/JSON multi-document orchestration stream in a regular file (not stdin or a symlink; at most 16 MiB).")
	fmt.Fprintln(w, "  The complete stream is validated before execution; --from executes and mutates resources, it is not a preview.")
}
