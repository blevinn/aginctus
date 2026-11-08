package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

// A parser error must not enter even the injected configuration loader,
// let alone an Incus/host operation. Each case uses a new Cobra tree.
type syntaxParityLoader struct{ calls int }

func (s *syntaxParityLoader) Load(config.Options) (*config.Config, error) {
	s.calls++
	return nil, errors.New("unexpected configuration access")
}

func TestCobraSyntaxBeforeConfigurationMatrix(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"config missing key", []string{"config", "get"}},
		{"config extra argument", []string{"config", "show", "extra"}},
		{"doctor unknown flag", []string{"doctor", "--dry-run"}},
		{"aggregate incompatible flags", []string{"apply", "--render", "--from", "plan.json"}},
		{"aggregate extra argument", []string{"remove", "extra"}},
		{"network unknown flag", []string{"network", "ensure", "--bogus"}},
		{"network firewall write flag on policy", []string{"network", "firewall", "policy", "--force"}},
		{"Herdr authorization missing ID", []string{"herdr", "authorize"}},
		{"Herdr stop cannot be dry-run", []string{"herdr", "client", "stop", "--dry-run"}},
		{"workload two IDs", []string{"workload", "apply", "a", "b"}},
		{"workload scoped start unconfirmed", []string{"workload", "start", "--project=aginctus", "dev"}},
		{"workload scoped stop unconfirmed", []string{"workload", "stop", "--project=aginctus", "dev"}},
		{"workload legacy alias unknown flag", []string{"workload", "dev", "ensure", "--bogus"}},
		{"gateway remove unexpected flag", []string{"gateway", "remove", "--dry-run"}},
		{"gateway provider register missing type", []string{"gateway", "provider", "register"}},
		{"gateway model render missing name", []string{"gateway", "model", "render"}},
		{"gateway firewall plan missing zone", []string{"gateway", "egress", "firewall", "plan"}},
		{"gateway firewall plan unknown flag", []string{"gateway", "egress", "firewall", "plan", "--ranges=bad"}},
		{"gateway reconcile not confirmed", []string{"gateway", "egress", "reconcile"}},
		{"gateway reconcile explicitly false", []string{"gateway", "egress", "reconcile", "--confirm-stopped-instance=false"}},
		{"gateway reconcile conflicting confirmations", []string{"gateway", "egress", "reconcile", "--dry-run", "--confirm-stopped-instance"}},
		{"gateway DHCP apply unconfirmed", []string{"gateway", "egress", "dhcp", "apply", "--ranges=a-b"}},
		{"gateway Responses unknown flag", []string{"gateway", "responses", "verify", "--force"}},
		{"operator project create unconfirmed", []string{"operator", "project-ensure", "--project=aginctus"}},
		{"operator unrecognized owner", []string{"operator", "units-audit", "--owner=unknown"}},
		{"image import unconfirmed", []string{"images", "import", "--project=aginctus", "alias", "path"}},
		{"image claimed owner missing", []string{"images", "import-owned", "--project=aginctus", "alias", "path", "--confirm-create"}},
		{"testenv VM requires scenario", []string{"testenv", "vm", "test"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loader := &syntaxParityLoader{}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), tc.args, &out, &errOut, nil, loader)
			if code != 2 || out.Len() != 0 || errOut.Len() == 0 {
				t.Fatalf("args=%q code=%d stdout=%q stderr=%q", tc.args, code, out.String(), errOut.String())
			}
			if loader.calls != 0 {
				t.Fatalf("args=%q invoked configuration loader %d times", tc.args, loader.calls)
			}
		})
	}
}

func TestCobraHelpOfflineNestedMatrix(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"help"},
		{"help", "gateway", "egress", "firewall"},
		{"gateway", "provider", "--help"},
		{"network", "firewall", "egress", "reconcile", "--help"},
		{"workload", "apply", "--help"},
		{"images", "import-owned", "--help"},
		{"operator", "project-ensure", "--help"},
		{"--configuration-file=/definitely/missing", "gateway", "egress", "--help"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			loader := &syntaxParityLoader{}
			var out, errOut bytes.Buffer
			code := Run(context.Background(), args, &out, &errOut, nil, loader)
			if code != 0 || out.Len() == 0 || errOut.Len() != 0 {
				t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
			}
			if loader.calls != 0 {
				t.Fatalf("help attempted configuration access: %q", args)
			}
		})
	}
}
