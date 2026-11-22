package api

import (
	"reflect"
	"testing"
)

func previewFixture() ConfiguredAcceptedDiff {
	return ConfiguredAcceptedDiff{
		TypeMeta:NewTypeMeta("ConfiguredAcceptedDiff"),
		Environment:Environment{ID:"11111111-1111-4111-8111-111111111111",Name:"dev",
			Binding:EnvironmentBinding{ServerID:"server-1",Project:"team"}},
		FromLayer:"accepted",ToLayer:"configured",HasAccepted:true,
		Revision:&AcceptedRevisionIdentity{ID:"revision-9",Generation:9,CandidateDigest:"old-digest"},
		ProposedDigest:"new-digest",Changed:true,
		NewlySelected:[]string{"z-agent"},Unselected:[]string{"a-old"},
		Modified:[]AcceptedWorkloadChange{{ID:"m-gateway",Fields:[]string{"imageFingerprint"}}},
	}
}

func TestAcceptancePreviewPreservesRevisionAndNeverGeneratesDeletion(t *testing.T) {
	diff:=previewFixture()
	got,err:=PlanAcceptanceFromDiff(diff)
	if err!=nil{t.Fatal(err)}
	if got.Kind!="AcceptancePlan" || got.BaseRevision!="revision-9" ||
		got.CandidateDigest!="new-digest" || got.Project!="team" ||
		got.EnvironmentID!=diff.Environment.ID {
		t.Fatalf("unexpected preview: %+v",got)
	}
	want:=[]LifecycleChange{
		{Resource:"a-old",Action:"unselect"},
		{Resource:"m-gateway",Action:"modify"},
		{Resource:"z-agent",Action:"select"},
	}
	if !reflect.DeepEqual(got.Changes,want){t.Fatalf("changes=%+v want=%+v",got.Changes,want)}
	for _,c:=range got.Changes {
		if c.Action=="remove"||c.Action=="delete"{t.Fatal("unselection produced mutation authorization")}
	}
	if err:=got.Validate();err!=nil{t.Fatal(err)}
}

func TestAcceptancePreviewFirstRevisionAndEmptyNoChange(t *testing.T) {
	d:=previewFixture()
	d.HasAccepted=false
	d.Revision=nil
	d.Unselected=[]string{}
	d.Modified=[]AcceptedWorkloadChange{}
	p,err:=PlanAcceptanceFromDiff(d)
	if err!=nil || p.BaseRevision!="" || len(p.Changes)!=1 {
		t.Fatalf("initial acceptance plan=%+v err=%v",p,err)
	}
	d.NewlySelected=[]string{}
	d.Changed=false
	p,err=PlanAcceptanceFromDiff(d)
	if err!=nil || p.Changes==nil || len(p.Changes)!=0 {
		t.Fatalf("empty plan=%+v err=%v",p,err)
	}
}

func TestAcceptancePreviewRejectsForeignAndContradictoryEvidence(t *testing.T) {
	for _,change:=range []func(*ConfiguredAcceptedDiff){
		func(d *ConfiguredAcceptedDiff){d.Environment.ID=""},
		func(d *ConfiguredAcceptedDiff){d.Environment.Binding.Project=""},
		func(d *ConfiguredAcceptedDiff){d.FromLayer="configured"},
		func(d *ConfiguredAcceptedDiff){d.ToLayer="observed"},
		func(d *ConfiguredAcceptedDiff){d.HasAccepted=false},
		func(d *ConfiguredAcceptedDiff){d.Revision.ID=""},
		func(d *ConfiguredAcceptedDiff){d.ProposedDigest=""},
		func(d *ConfiguredAcceptedDiff){d.Modified=nil},
		func(d *ConfiguredAcceptedDiff){d.Unselected=[]string{"z-agent"}},
		func(d *ConfiguredAcceptedDiff){d.Modified[0].Fields=nil},
		func(d *ConfiguredAcceptedDiff){d.Changed=false},
	}{
		d:=previewFixture()
		change(&d)
		p,err:=PlanAcceptanceFromDiff(d)
		if err==nil || p.Kind!="" {t.Fatalf("invalid evidence projected: %+v err=%v",p,err)}
	}
}
