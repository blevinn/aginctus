package environment

import (
	"strings"
	"testing"

	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func TestAcceptedLifecyclePins(t *testing.T) {
	c := candidateFixture()
	c.Workloads[0].Native = &acceptedstate.WorkloadNativeSpec{Runtime: "opencode", StoragePool: "root-pool", ManagementNetwork: "bridge"}
	c.Herdr = &HerdrSpec{InstanceName: "herdr", ImageFingerprint: strings.Repeat("a", 64), StoragePool: "state-pool", ManagementNetwork: "bridge"}
	r, err := BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	body, err := c.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ from, to string }{
		{`"native":{"runtime":"opencode","storagePool":"root-pool","managementNetwork":"bridge"}`, `"native":null`},
		{`,"storagePool":"root-pool"`, ``},
		{`"runtime":"opencode"`, `"Runtime":"opencode"`},
		{`"allowStateBootstrap":false`, `"allowStateBootstrap":null`},
		{`"allowStateBootstrap":false`, `"createState":false`},
	} {
		bad := strings.Replace(string(body), tc.from, tc.to, 1)
		if bad == string(body) {
			t.Fatalf("test mutation absent: %s", tc.from)
		}
		if _, err := ParseCandidate([]byte(bad)); err == nil {
			t.Fatalf("accepted malformed lifecycle body: %s", tc.to)
		}
	}
	for _, mutate := range []func(*Candidate){
		func(c *Candidate) { c.Workloads[0].Native.Runtime = "other" },
		func(c *Candidate) { c.Workloads[0].Native.StoragePool = "other" },
		func(c *Candidate) { c.Workloads[0].Native.ManagementNetwork = "other" },
		func(c *Candidate) { c.Herdr.InstanceName = "other" },
		func(c *Candidate) { c.Herdr.StoragePool = "other" },
		func(c *Candidate) { c.Herdr.ManagementNetwork = "other" },
		func(c *Candidate) { c.Herdr.ImageFingerprint = strings.Repeat("b", 64) },
		func(c *Candidate) { c.Herdr.AllowStateBootstrap = true },
	} {
		altered, err := ParseCandidate(body)
		if err != nil || r.ValidateAgainst(altered) != nil {
			t.Fatalf("lifecycle round trip: %v", err)
		}
		// Workload order is canonical; find the one with the optional pin.
		if altered.Workloads[0].Native == nil {
			altered.Workloads[0], altered.Workloads[1] = altered.Workloads[1], altered.Workloads[0]
		}
		mutate(&altered)
		if altered.Validate() != nil || r.ValidateAgainst(altered) == nil {
			t.Fatal("valid change was not pinned by accepted digest")
		}
		diff, err := CompareCandidates(c.Environment, c.Environment.Binding, c, altered)
		if err != nil || !diff.Changed || (!diff.HerdrChanged && len(diff.WorkloadChanges.Modified) != 1) {
			t.Fatalf("redacted lifecycle diff missing: %+v %v", diff, err)
		}
	}
	c.Herdr.InstanceName = c.Workloads[0].InstanceName
	if c.Validate() == nil {
		t.Fatal("allowed overlapping Herdr/workload physical name")
	}
}
