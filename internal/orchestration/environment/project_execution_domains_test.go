package environment

import (
	"testing"
)

func TestProjectDomainGrantsDoNotInheritWorkloadAuthority(t *testing.T) {
	c := candidateFixture()
	gateway := projectExecutionGrant{WorkloadID: "gateway-credentials", Operation: "gateway-deliver"}
	herdr := projectExecutionGrant{WorkloadID: "herdr-client", Operation: "herdr-reconcile"}
	if gateway.domainSelected(c) || herdr.domainSelected(c) {
		t.Fatal("absent component selected")
	}
	c.Gateway, c.Herdr = &GatewaySpec{}, &HerdrSpec{}
	if !gateway.domainSelected(c) || !herdr.domainSelected(c) {
		t.Fatal("explicit component not selected")
	}
	for _, tc := range []struct {
		grant projectExecutionGrant
		action string
		ok bool
	}{
		{gateway, "deliver", true},
		{gateway, "start", false},
		{herdr, "create", true},
		{herdr, "start", true},
		{herdr, "delete", false},
		{herdr, "deliver", false},
		{projectExecutionGrant{WorkloadID: "gateway-credentials", Operation: "reconcile"}, "deliver", false},
	} {
		e := LocalNativeEffectRecord{Resource: tc.grant.WorkloadID, Target: tc.grant.WorkloadID, Action: tc.action}
		if tc.grant.permits(e) != tc.ok {
			t.Fatalf("wrong effect authorization: %s %s", tc.grant.Operation, tc.action)
		}
	}
	ordinary := projectExecutionGrant{WorkloadID: gateway.WorkloadID, Operation: "inspect"}
	if ordinary.inspects(gateway) {
		t.Fatal("ordinary inspection inherited gateway authority")
	}
	ordinary.Operation = "gateway-inspect"
	if !ordinary.inspects(gateway) {
		t.Fatal("explicit gateway inspection denied")
	}
}
