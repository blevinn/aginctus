package environment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func TestLocalEnvironmentPinExcludesSameEnvironmentCommitNotPeers(t *testing.T){
	store,a:=newLocalProjectFixture(t)
	ctx:=context.Background()
	first,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,nil,a)
	if err!=nil{t.Fatal(err)}
	b:=a
	b.Environment.ID="7c82e6d1-1212-4123-8123-0123456789ac"
	b.Environment.Name="other"
	peer,err:=store.CommitLocalCandidate(ctx,b.Environment,b.Environment.Binding,nil,b)
	if err!=nil{t.Fatal(err)}
	pin,err:=store.PinLocalAccepted(ctx,a.Environment,first,"beta")
	if err!=nil{t.Fatal(err)}
	if err:=pin.Check(ctx);err!=nil{t.Fatal(err)}
	copyCandidate,err:=pin.SnapshotCandidate(ctx)
	if err!=nil{t.Fatal(err)}
	copyCandidate.Workloads[0].Profiles[0]="mutated"
	copyCandidate.SelectedWorkloads[0]="mutated"
	again,err:=pin.SnapshotCandidate(ctx)
	if err!=nil||again.Workloads[0].Profiles[0]=="mutated"||again.SelectedWorkloads[0]=="mutated"{
		t.Fatalf("snapshot leaked mutable aliases: %+v %v",again,err)
	}
	modified:=a
	modified.Workloads=append([]acceptedstate.WorkloadSpec(nil),a.Workloads...)
	modified.Workloads[0].ImageFingerprint=strings.Repeat("f",64)
	if _,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,&first,modified);err==nil{
		t.Fatal("concurrent accepted promotion crossed held environment pin")
	} else if !errors.Is(err,filestate.ErrRegistryBusy) &&
		!strings.Contains(err.Error(),"local environment CAS lock unavailable"){
		t.Fatalf("unexpected same-environment lock outcome: %v",err)
	}
	// A different environment in the SAME project must not be blocked.
	changedPeer:=b
	changedPeer.Environment.Name="peer-renamed"
	if _,err:=store.CommitLocalCandidate(ctx,b.Environment,b.Environment.Binding,&peer,changedPeer);err!=nil{
		t.Fatalf("A pin blocked independently versioned B: %v",err)
	}
	if err:=pin.Close();err!=nil{t.Fatal(err)}
	if err:=pin.Check(ctx);!errors.Is(err,ErrAcceptedConflict){
		t.Fatalf("closed pin remained valid: %v",err)
	}
	if err:=pin.Close();err!=nil{t.Fatal(err)}
	newRevision,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,&first,modified)
	if err!=nil || newRevision.ParentID!=first.ID{t.Fatalf("promotion after release: %+v %v",newRevision,err)}
	if _,err:=store.PinLocalAccepted(ctx,a.Environment,first,"beta");!errors.Is(err,ErrAcceptedConflict){
		t.Fatalf("stale accepted revision pinned: %v",err)
	}
}

func TestLocalEnvironmentStartJournalPersistsUnverifiedAfterRestart(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	revision,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	pin,err:=store.PinLocalAccepted(ctx,c.Environment,revision,"beta")
	if err!=nil{t.Fatal(err)}
	record,err:=pin.RecordLocalUnverifiedStart(ctx)
	if err!=nil||record.Outcome!="unverified"||record.Operation!="start"||
		record.RevisionID!=revision.ID||record.EnvironmentID!=c.Environment.ID{
		t.Fatalf("failed to journal exact unverified intent: %+v %v",record,err)
	}
	if err:=pin.Close();err!=nil{t.Fatal(err)}
	restarted,err:=OpenLocalProjectStateStore(store.root,store.binding)
	if err!=nil{t.Fatal(err)}
	historical,err:=restarted.ReadLocalUnverifiedStart(ctx,c.Environment,record.ID)
	if err!=nil||historical!=record{t.Fatalf("restart lost unresolved attempt: %+v %v",historical,err)}
	changed:=c
	changed.Environment.Name="new-name"
	_,err=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,&revision,changed)
	if err!=nil{t.Fatal(err)}
	reloaded,err:=restarted.ReadLocalUnverifiedStart(ctx,c.Environment,record.ID)
	if err!=nil||reloaded!=record||reloaded.Outcome!="unverified"{
		t.Fatalf("later acceptance silently completed historical operation: %+v %v",reloaded,err)
	}
	other:=c.Environment
	other.ID="7c82e6d1-1212-4123-8123-0123456789ac"
	if _,err:=restarted.ReadLocalUnverifiedStart(ctx,other,record.ID);err==nil{
		t.Fatal("read another environment's journal")
	}
	if _,err:=restarted.ReadLocalUnverifiedStart(ctx,c.Environment,"../../etc/passwd");err==nil{
		t.Fatal("accepted traversal as immutable operation ID")
	}
}

func TestLocalEnvironmentJournalRejectsFalseSuccessAndAlteredRevision(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	rev,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	pin,err:=store.PinLocalAccepted(ctx,c.Environment,rev,"beta")
	if err!=nil{t.Fatal(err)}
	record,err:=pin.RecordLocalUnverifiedStart(ctx)
	if err!=nil{t.Fatal(err)}
	if err:=pin.Close();err!=nil{t.Fatal(err)}
	path:=filepath.Join(store.root,"environments",c.Environment.ID,"operations",record.ID+".json")
	raw,err:=os.ReadFile(path)
	if err!=nil{t.Fatal(err)}
	for _,testCase:=range []struct{
		name string
		modify func(map[string]any)
	}{
		{"claimed-success",func(m map[string]any){m["outcome"]="verified"}},
		{"foreign-project",func(m map[string]any){m["project"]="foreign"}},
		{"changed-revision",func(m map[string]any){m["revisionId"]=strings.Repeat("e",64)}},
		{"unknown-operation",func(m map[string]any){m["operation"]="delete"}},
		{"mismatched-generation",func(m map[string]any){m["generation"]=float64(40)}},
		{"unexpected-field",func(m map[string]any){m["hostPolicy"]=true}},
	}{
		t.Run(testCase.name,func(t *testing.T){
			var m map[string]any
			if err:=json.Unmarshal(raw,&m);err!=nil{t.Fatal(err)}
			testCase.modify(m)
			bad,err:=json.Marshal(m)
			if err!=nil{t.Fatal(err)}
			if err:=os.WriteFile(path,bad,0o600);err!=nil{t.Fatal(err)}
			if _,err:=store.ReadLocalUnverifiedStart(ctx,c.Environment,record.ID);err==nil{
				t.Fatal("tampered operation history was trusted")
			}
		})
	}
	if err:=os.WriteFile(path,raw,0o600);err!=nil{t.Fatal(err)}
	if _,err:=store.ReadLocalUnverifiedStart(ctx,c.Environment,record.ID);err!=nil{t.Fatal(err)}
}

func TestLocalEnvironmentJournalRefusesNonStartAndCancellation(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	rev,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	pin,err:=store.PinLocalAccepted(ctx,c.Environment,rev,"alpha")
	if err!=nil{t.Fatal(err)}
	if _,err:=pin.RecordLocalUnverifiedStart(ctx);err==nil{
		t.Fatal("ensureRunning=false recorded implicit stop or start")
	}
	if err:=pin.Close();err!=nil{t.Fatal(err)}
	startPin,err:=store.PinLocalAccepted(ctx,c.Environment,rev,"beta")
	if err!=nil{t.Fatal(err)}
	cancelled,cancel:=context.WithCancel(ctx)
	cancel()
	if _,err:=startPin.RecordLocalUnverifiedStart(cancelled);!errors.Is(err,context.Canceled){
		t.Fatalf("canceled operation recorded: %v",err)
	}
	if err:=startPin.Close();err!=nil{t.Fatal(err)}
	if _,err:=store.PinLocalAccepted(ctx,c.Environment,rev,"foreign-workload");!errors.Is(err,ErrAcceptedConflict){
		t.Fatalf("foreign workload pinned: %v",err)
	}
}
