package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReconcileOwnedFirewalldNoMutationOnDeniedHost(t *testing.T) {
	for _, tc := range []struct { name string; uid int; zone, target, active, services string } {
		{"not root", 1000, "aginctus-smoke", "DROP", "target: DROP", ""},
		{"invalid zone", 0, "", "DROP", "target: DROP", ""},
		{"permanent target", 0, "aginctus-smoke", "ACCEPT", "target: DROP", ""},
		{"runtime target", 0, "aginctus-smoke", "DROP", "target: ACCEPT", ""},
		{"foreign service", 0, "aginctus-smoke", "DROP", "target: DROP", "ssh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			_, err := ReconcileOwnedFirewalld(context.Background(), tc.zone, FirewalldReconcileActions{
				EffectiveUID: func() int { return tc.uid },
				Command: func(_ context.Context, input, cmd string, args ...string) (string, error) {
					if input != "" || cmd != "firewall-cmd" { t.Fatalf("unexpected command %s",cmd) }
					for _, a := range args {
						if strings.HasPrefix(a,"--remove-") || strings.HasPrefix(a,"--add-") { writes++ }
					}
					switch args[len(args)-1] {
					case "--get-zones": return "aginctus-smoke", nil
					case "--get-target": return tc.target, nil
					case "--list-all": return tc.active, nil
					case "--list-services": return tc.services, nil
					case "--list-ports", "--list-sources", "--list-protocols", "--list-forward-ports": return "", nil
					default: return "", nil
					}
				},
				Discover: func(context.Context) (Policy,error) { t.Fatal("discovery before host validation"); return Policy{},nil },
			})
			if err == nil || writes != 0 { t.Fatalf("err=%v writes=%d",err,writes) }
		})
	}
}

func TestReconcileOwnedFirewalldRejectsMissingCapabilities(t *testing.T) {
	if _, err := ReconcileOwnedFirewalld(context.Background(), "aginctus", FirewalldReconcileActions{}); err == nil {
		t.Fatal("nil authority accepted")
	}
	if _, err := ReconcileOwnedFirewalld(context.Background(), "aginctus", FirewalldReconcileActions{
		EffectiveUID: func() int {return 0},
		Command: func(context.Context,string,string,...string)(string,error){return "",errors.New("unavailable")},
	}); err == nil {
		t.Fatal("nil discovery accepted")
	}
}
