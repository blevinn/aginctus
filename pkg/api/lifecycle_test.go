package api

import (
	"encoding/json"
	"strings"
	"testing"
)

const lifecycleEnv = "11111111-1111-4111-8111-111111111111"

func lifecycleBase() PlanBase {
	return PlanBase{Project:"team",EnvironmentID:lifecycleEnv,Changes:[]LifecycleChange{{Resource:"agent",Action:"update"}}}
}
func TestLifecyclePlansAreTypedReadOnlyPreviews(t *testing.T) {
	accept:=AcceptancePlan{TypeMeta:NewTypeMeta("AcceptancePlan"),PlanBase:lifecycleBase(),CandidateDigest:"sha256:candidate"}
	reconcile:=ReconciliationPlan{TypeMeta:NewTypeMeta("ReconciliationPlan"),PlanBase:lifecycleBase(),AcceptedRevision:"rev-1"}
	removeBase:=lifecycleBase()
	removeBase.Changes=[]LifecycleChange{{Resource:"agent",Action:"remove"}}
	remove:=RemovalPlan{TypeMeta:NewTypeMeta("RemovalPlan"),PlanBase:removeBase,ResourceName:"agent",AbsentConfigured:true}
	for _,p:=range []interface{Validate() error}{accept,reconcile,remove}{
		if err:=p.Validate();err!=nil{t.Fatal(err)}
	}
	for _,p:=range []Document{accept,reconcile,remove}{
		encoded,err:=json.Marshal(p)
		if err!=nil{t.Fatal(err)}
		if !strings.Contains(string(encoded),`"apiVersion":"aginctus.dev/v1alpha1"`){
			t.Fatalf("unversioned plan %s",encoded)
		}
		if strings.Contains(string(encoded),"permission")||strings.Contains(string(encoded),"authorizationToken"){
			t.Fatalf("plan incorrectly claims authority: %s",encoded)
		}
	}
	accept.CandidateDigest=""
	if err:=accept.Validate();err==nil{t.Fatal("no candidate content identity")}
	reconcile.AcceptedRevision=""
	if err:=reconcile.Validate();err==nil{t.Fatal("no pinned accepted revision")}
	remove.AbsentConfigured=false
	if err:=remove.Validate();err==nil{t.Fatal("removal without absence proof")}
	remove.AbsentConfigured=true
	remove.Changes=[]LifecycleChange{{Resource:"different",Action:"remove"}}
	if err:=remove.Validate();err==nil{t.Fatal("unrelated removal")}
}
func TestLifecycleResultCannotConfuseAcknowledgementWithConvergence(t *testing.T) {
	accept:=AcceptanceResult{TypeMeta:NewTypeMeta("AcceptanceResult"),Project:"team",EnvironmentID:lifecycleEnv,
		AcceptedRevision:AcceptedRevisionIdentity{ID:"rev-1",CandidateDigest:"digest",Generation:1}}
	if err:=accept.Validate();err!=nil{t.Fatal(err)}
	ack:=ReconciliationResult{TypeMeta:NewTypeMeta("ReconciliationResult"),Project:"team",EnvironmentID:lifecycleEnv,
		AcceptedRevision:"rev-1",Stage:ReconciliationAcknowledged}
	if err:=ack.Validate();err!=nil{t.Fatal(err)}
	attempt:=ack;attempt.Stage=ReconciliationCompleted;attempt.AttemptID="attempt-1"
	if err:=attempt.Validate();err!=nil{t.Fatal(err)}
	verified:=attempt;verified.Stage=ReconciliationVerified;verified.Converged=true
	if err:=verified.Validate();err!=nil{t.Fatal(err)}
	for _,bad:=range []ReconciliationResult{
		func()ReconciliationResult{v:=ack;v.Converged=true;return v}(),
		func()ReconciliationResult{v:=attempt;v.Converged=true;return v}(),
		func()ReconciliationResult{v:=verified;v.AttemptID="";return v}(),
		func()ReconciliationResult{v:=verified;v.AcceptedRevision="";return v}(),
		func()ReconciliationResult{v:=verified;v.EnvironmentID="";return v}(),
		func()ReconciliationResult{v:=verified;v.Stage="unknown";return v}(),
	}{
		if err:=bad.Validate();err==nil{t.Fatalf("invalid stage accepted: %+v",bad)}
	}
	removal:=RemovalResult{TypeMeta:NewTypeMeta("RemovalResult"),Project:"team",EnvironmentID:lifecycleEnv,
		AcceptedRevision:"rev-1",ResourceName:"agent",Removed:true}
	if err:=removal.Validate();err!=nil{t.Fatal(err)}
	removal.Removed=false
	if err:=removal.Validate();err==nil{t.Fatal("failed removal misrepresented as completed")}
}
