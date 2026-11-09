package acceptedstate

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRevisionChainsAndDeterminism(t *testing.T){
	s:=validSpec()
	first,err:=BuildRevision(nil,s)
	if err!=nil {t.Fatal(err)}
	if first.Generation!=1 || first.ParentID!="" {t.Fatal(first)}
	if err:=first.validateDigest(s);err!=nil {t.Fatal(err)}
	other,err:=BuildRevision(nil,s)
	if err!=nil || first!=other {t.Fatal("unstable first revision")}
	_,err=BuildRevision(&first,s)
	if !errors.Is(err,ErrNoChanges) {t.Fatalf("unchanged proposal: %v",err)}
	s.Workloads[0].ImageFingerprint=strings.Repeat("c",64)
	second,err:=BuildRevision(&first,s)
	if err!=nil || second.Generation!=2 || second.ParentID!=first.ID {t.Fatalf("chaining: %+v, %v",second,err)}
	if err:=second.validateDigest(s);err!=nil {t.Fatal(err)}
	second.SpecDigest=first.SpecDigest
	if err:=second.validateDigest(s);err==nil {t.Fatal("accepted stale digest")}
}

func TestDiffOnlyFieldNamesAndNonDeletingSelection(t *testing.T){
	a:=validSpec()
	b:=validSpec()
	b.Workloads[0].ImageFingerprint=strings.Repeat("d",64)
	b.Workloads[0].EnsureRunning=false
	b.Workloads[1].Profiles=[]string{"stable"}
	diff,err:=CompareDeployments(a,b)
	if err!=nil {t.Fatal(err)}
	if len(diff.Modified)!=2 {t.Fatalf("diff = %+v",diff)}
	payload,_:=json.Marshal(diff)
	for _,s:=range []string{strings.Repeat("d",64),"stable"} {
		if strings.Contains(string(payload),s){t.Fatal("diff disclosed value")}
	}
	if !reflect.DeepEqual(diff.NewlySelected,[]string{}) {t.Fatal(diff)}
	b.Selection.SelectedWorkloads=[]string{"beta"}
	b.Workloads=b.Workloads[:1]
	diff,err=CompareDeployments(a,b)
	if err!=nil || !reflect.DeepEqual(diff.Unselected,[]string{"alpha"}) {t.Fatalf("unselection = %+v %v",diff,err)}
	payload,_=json.Marshal(diff)
	if strings.Contains(string(payload),`"delete"`){t.Fatal("unselection should not imply deletion")}
	b.Selection.IncusProject="another-project"
	if _,err:=CompareDeployments(a,b);err==nil {t.Fatal("project change allowed")}
}

func TestRevisionNotAuthorization(t *testing.T){
	r,err:=BuildRevision(nil,validSpec())
	if err!=nil {t.Fatal(err)}
	serialized,_:=json.Marshal(r)
	if strings.Contains(string(serialized),"owner") || strings.Contains(string(serialized),"permissions") {t.Fatal("revision confused with authority")}
}
