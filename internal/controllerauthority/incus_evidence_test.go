package controllerauthority

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/state"
	incusscope "github.com/blevinn/aginctus/internal/incus"
)

type testMetadata map[string]json.RawMessage

func goodMetadata(t *testing.T) (ReadIncusMetadata, testMetadata) {
	t.Helper()
	scope,err:=incusscope.NewProjectScope("aginctus")
	if err!=nil{t.Fatal(err)}
	config,err:=scope.ProposedRestrictedOperatorConfig("hm-mgmt","default")
	if err!=nil{t.Fatal(err)}
	config["restricted.devices.disk"]="managed"
	fingerprint:=strings.Repeat("a",64)
	marshal:=func(v any) json.RawMessage{
		out,e:=json.Marshal(v);if e!=nil{t.Fatal(e)};return out
	}
	m:=testMetadata{
		"/1.0?project=aginctus":marshal(map[string]any{"auth":"trusted","auth_user_method":"tls","auth_user_name":fingerprint}),
		"/1.0/certificates/"+fingerprint:marshal(map[string]any{"fingerprint":fingerprint,"restricted":true,"projects":[]string{"aginctus"}}),
		"/1.0/projects/aginctus":marshal(map[string]any{"name":"aginctus","config":config}),
	}
	return func(ctx context.Context,path string)(json.RawMessage,error){
		if err:=ctx.Err();err!=nil{return nil,err}
		v,ok:=m[path];if !ok{return nil,errors.New("not found")}
		return v,nil
	},m
}
func TestRestrictedOperatorObservesEffectiveDaemonEvidence(t *testing.T){
	reader,_:=goodMetadata(t)
	actual,err:=ObserveRestrictedOperator(context.Background(),"aginctus","hm-mgmt","default",reader)
	if err!=nil||!actual.VerifiedRestriction||actual.Project!="aginctus"||
		actual.CertificateFingerprint!=strings.Repeat("a",64) {
		t.Fatalf("restricted daemon evidence refused: %+v %v",actual,err)
	}
	if _,err:=ObserveRestrictedOperator(context.Background(),"default","hm-mgmt","default",reader);err==nil{
		t.Fatal("accepted default project")
	}
	if _,err:=ObserveRestrictedOperator(context.Background(),"aginctus","*","default",reader);err==nil{
		t.Fatal("accepted wildcard host network")
	}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if _,err:=ObserveRestrictedOperator(ctx,"aginctus","hm-mgmt","default",reader);!errors.Is(err,context.Canceled){
		t.Fatalf("canceled reader succeeded: %v",err)
	}
}
func TestRestrictedOperatorRejectsTLSAndPolicyDrift(t *testing.T){
	for _,tc:=range []struct{
		name,path string
		modify func(map[string]any)
	}{
		{"untrusted","/1.0?project=aginctus",func(v map[string]any){v["auth"]="untrusted"}},
		{"unix","/1.0?project=aginctus",func(v map[string]any){v["auth_user_method"]="unix"}},
		{"broad-cert","/1.0/certificates/"+strings.Repeat("a",64),func(v map[string]any){v["restricted"]=false}},
		{"extra-project","/1.0/certificates/"+strings.Repeat("a",64),func(v map[string]any){v["projects"]=[]string{"aginctus","default"}}},
		{"fingerprint-mismatch","/1.0/certificates/"+strings.Repeat("a",64),func(v map[string]any){v["fingerprint"]=strings.Repeat("c",64)}},
		{"policy-unrestricted","/1.0/projects/aginctus",func(v map[string]any){v["config"].(map[string]any)["restricted"]="false"}},
		{"policy-disk","/1.0/projects/aginctus",func(v map[string]any){v["config"].(map[string]any)["restricted.devices.disk"]="allow"}},
		{"policy-network","/1.0/projects/aginctus",func(v map[string]any){v["config"].(map[string]any)["restricted.networks.access"]="other"}},
		{"policy-pool","/1.0/projects/aginctus",func(v map[string]any){v["config"].(map[string]any)["restricted.storage-pools.access"]="*"}},
		{"policy-extra","/1.0/projects/aginctus",func(v map[string]any){v["config"].(map[string]any)["restricted.networks.uplinks"]="any"}},
		{"policy-host-owner","/1.0/projects/aginctus",func(v map[string]any){v["config"].(map[string]any)["user.aginctus.deployment"]="foreign"}},
	}{
		t.Run(tc.name,func(t *testing.T){
			reader,data:=goodMetadata(t)
			var obj map[string]any
			if err:=json.Unmarshal(data[tc.path],&obj);err!=nil{t.Fatal(err)}
			tc.modify(obj)
			mutated,err:=json.Marshal(obj);if err!=nil{t.Fatal(err)}
			data[tc.path]=mutated
			if _,err:=ObserveRestrictedOperator(context.Background(),"aginctus","hm-mgmt","default",reader);err==nil{
				t.Fatal("accepted unsafe TLS or project policy")
			}
		})
	}
}
func TestRestrictedOperatorRejectsMissingIdentityReads(t *testing.T){
	reader,data:=goodMetadata(t)
	for _,path:=range []string{"/1.0?project=aginctus","/1.0/certificates/"+strings.Repeat("a",64),"/1.0/projects/aginctus"} {
		saved:=data[path];delete(data,path)
		if _,err:=ObserveRestrictedOperator(context.Background(),"aginctus","hm-mgmt","default",reader);err==nil{
			t.Fatalf("accepted missing daemon evidence %s",path)
		}
		data[path]=saved
	}
}
func TestEnrollmentAssessmentCannotBecomeWriteAuthority(t *testing.T){
	dir:=t.TempDir()
	rec:=testRecord()
	if err:=os.WriteFile(filepath.Join(dir,hostEnrollmentFile),encode(t,rec),0o600);err!=nil{t.Fatal(err)}
	grant,err:=loadOwnerFile(dir,uint32(os.Geteuid()))
	if err!=nil{t.Fatal(err)}
	pin:=acceptedstate.PinnedWorkloadSnapshot{
		Revision:acceptedstate.Revision{ID:rec.RevisionID,Generation:rec.Generation},
		DeploymentID:rec.DeploymentID,IncusProject:rec.IncusProject,
		Workload:acceptedstate.WorkloadSpec{ID:rec.WorkloadID},
	}
	tls:=DaemonTLSObservation{Project:rec.IncusProject,CertificateFingerprint:rec.Certificate,VerifiedRestriction:true}
	if a:=grant.AssessAcceptedOperatorSelection(pin,rec.UID,tls,"start");a.Code!=EnrollmentMatched{
		t.Fatalf("matching evidence not reported: %+v",a)
	}
	tls.VerifiedRestriction=false
	if a:=grant.AssessAcceptedOperatorSelection(pin,rec.UID,tls,"start");a.Code==EnrollmentMatched{
		t.Fatal("unverified TLS treated as matching owner")
	}
	tls.VerifiedRestriction=true
	pin.Revision.Generation++
	if a:=grant.AssessAcceptedOperatorSelection(pin,rec.UID,tls,"start");a.Code==EnrollmentMatched{
		t.Fatal("stale accepted selection matched")
	}
}
