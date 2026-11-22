package client

import (
	"context"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

func TestPlanAcceptanceUsesVerifiedDiffAndPreservesScope(t *testing.T) {
	d := validStateDiff()
	d.Changed = true
	d.NewlySelected = []string{"z-agent"}
	d.Unselected = []string{"a-old"}
	d.Modified = []api.AcceptedWorkloadChange{{ID:"gateway",Fields:[]string{"imageFingerprint"}}}
	d.HasAccepted = true
	d.Revision = &api.AcceptedRevisionIdentity{ID:"rev-2",Generation:2,CandidateDigest:"old"}
	source := &testStateDiffSource{value:d}
	c := New(Options{ConfiguredAcceptedDiffReader:source})
	req := PlanAcceptanceRequest{Project:"team",EnvironmentID:diffEnvID}
	plan,err := c.State.PlanAcceptance(context.Background(),req)
	if err!=nil { t.Fatal(err) }
	if source.calls!=1 || plan.Kind!="AcceptancePlan" || plan.BaseRevision!="rev-2" ||
		plan.CandidateDigest!="candidate-digest" || len(plan.Changes)!=3 { t.Fatalf("plan=%+v calls=%d",plan,source.calls) }
	for _,change := range plan.Changes {
		if change.Action=="remove" || change.Action=="delete" { t.Fatal("preview granted deletion") }
	}
	if err:=plan.Validate();err!=nil { t.Fatal(err) }
}

func TestPlanAcceptanceFailsClosedWithoutReaderOrValidEvidence(t *testing.T) {
	req:=PlanAcceptanceRequest{Project:"team",EnvironmentID:diffEnvID}
	if got,err:=New(Options{}).State.PlanAcceptance(context.Background(),req);err==nil||got.Kind!="" {
		t.Fatalf("unexpected success without backend: %+v %v",got,err)
	}
	source:=&testStateDiffSource{value:validStateDiff()}
	c:=New(Options{ConfiguredAcceptedDiffReader:source})
	if _,err:=c.State.PlanAcceptance(context.Background(),PlanAcceptanceRequest{});err==nil||source.calls!=0{
		t.Fatalf("unscoped lookup reached source: %v",err)
	}
	// A diff that claims selection but reports no change is contradictory,
	// even though its wire envelope and scope are otherwise valid.
	if got,err:=c.State.PlanAcceptance(context.Background(),req);err==nil||got.Kind!=""{
		t.Fatalf("contradictory diff became plan: %+v %v",got,err)
	}
	source.value.Changed=true
	if plan,err:=c.State.PlanAcceptance(context.Background(),req);err!=nil||len(plan.Changes)!=1{
		t.Fatalf("valid initial plan: %+v %v",plan,err)
	}
	source.err=errors.New("private state reader details")
	if got,err:=c.State.PlanAcceptance(context.Background(),req);err==nil||got.Kind!=""||
		err.Error()=="private state reader details" {t.Fatalf("source error leaked: %+v %v",got,err)}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	calls:=source.calls
	if _,err:=c.State.PlanAcceptance(ctx,req);!errors.Is(err,context.Canceled)||source.calls!=calls{
		t.Fatalf("cancellation lost: %v calls=%d",err,source.calls)
	}
}
