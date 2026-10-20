package gateway

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestInterpretFirewalldUnassignedInterface(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 2").Run()
	if err == nil {
		t.Fatal("expected exit status")
	}
	got, normalized := interpretFirewalldResult([]string{"--get-zone-of-interface=aginctus-egress"}, "no zone", err)
	if normalized != nil || got != "no zone" {
		t.Fatalf("unassigned interface rejected: %q, %v", got, normalized)
	}
	for _, tc := range []struct {
		args   []string
		output string
		err    error
	}{
		{[]string{"--get-zone-of-interface=aginctus-egress"}, "different error", err},
		{[]string{"--get-zones"}, "no zone", err},
		{[]string{"--get-zone-of-interface=aginctus-egress"}, "no zone", errors.New("exit status 2")},
		{[]string{"--permanent", "--get-zone-of-interface=aginctus-egress"}, "no zone", err},
	} {
		_, fail := interpretFirewalldResult(tc.args, tc.output, tc.err)
		if fail == nil || !strings.Contains(fail.Error(), "firewall-cmd failed") {
			t.Errorf("masked failure: %+v", tc)
		}
	}
}
