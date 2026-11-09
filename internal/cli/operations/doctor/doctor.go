package doctor

import (
	"context"
	"fmt"
	"io"
)

// IncusClient is the read-only capability required by the doctor command.
type IncusClient interface {
	ServerVersion(context.Context) (string, error)
}

type httpsClient interface {
	HTTPSAddress(context.Context) (string, error)
}

// Run performs doctor checks after the Cobra leaf has validated syntax and
// configuration. The caller supplies the narrowly scoped HTTPS setter.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, incusClient IncusClient, setHTTPSAddress func(context.Context, string) error) int {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--fix") {
		fmt.Fprintln(stderr, "Usage: aginctus doctor [--fix]")
		return 2
	}
	fix := len(args) == 1
	version, err := incusClient.ServerVersion(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "incus daemon: unreachable: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "incus daemon: reachable (%s)\n", version)
	client, ok := incusClient.(httpsClient)
	if !ok {
		fmt.Fprintln(stderr, "incus HTTPS listener: cannot inspect with this client")
		return 1
	}
	address, err := client.HTTPSAddress(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "incus HTTPS listener: check failed: %v\n", err)
		return 1
	}
	if address != "" {
		if !IsLoopbackHTTPSAddress(address) {
			// Do not rewrite a pre-existing listener even with --fix: doing
			// so can sever another operator's deliberate remote connection.
			fmt.Fprintln(stderr, "incus HTTPS listener: unsafe or invalid (requires a numeric loopback IP and TCP port); inspect core.https_address and remediate explicitly")
			return 1
		}
		fmt.Fprintf(stdout, "incus HTTPS listener: configured (%s)\n", address)
		return 0
	}
	if !fix {
		fmt.Fprintln(stderr, "incus HTTPS listener: absent (required by incus-compose image caching); run 'aginctus doctor --fix' to configure a loopback-only listener")
		return 1
	}
	// Never implicitly expose the Incus management API on all host interfaces.
	const loopback = "127.0.0.1:8443"
	if err := setHTTPSAddress(ctx, loopback); err != nil {
		fmt.Fprintf(stderr, "incus HTTPS listener: remediation failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "incus HTTPS listener: configured (%s); verify image-cache connectivity in your Incus setup\n", loopback)
	return 0
}
