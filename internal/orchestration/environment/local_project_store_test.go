package environment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blevinn/aginctus/internal/filestate"
	acceptedstate "github.com/blevinn/aginctus/internal/orchestration/state"
)

func newLocalProjectFixture(t *testing.T)(*LocalProjectStateStore,Candidate) {
	t.Helper()
	c:=candidateFixture()
	root:=filepath.Join(t.TempDir(),"project-state")
	store,err:=InitializeLocalProjectStateStore(root,c.Environment.Binding)
	if err!=nil{t.Fatal(err)}
	return store,c
}

func TestLocalProjectStorePersistsIndependentEnvironmentCAS(t *testing.T){
	store,a:=newLocalProjectFixture(t)
	ctx:=context.Background()
	b:=a
	b.Environment.ID="7c82e6d1-1212-4123-8123-0123456789ac"
	b.Environment.Name="secondary"
	b.Workloads=append([]acceptedstate.WorkloadSpec(nil),a.Workloads...)
	if r,c,err:=store.ReadLocalAccepted(ctx,a.Environment);err!=nil||r!=nil||c!=nil{
		t.Fatalf("new environment was not empty: %+v %+v %v",r,c,err)
	}
	one,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,nil,a)
	if err!=nil{t.Fatal(err)}
	two,err:=store.CommitLocalCandidate(ctx,b.Environment,b.Environment.Binding,nil,b)
	if err!=nil{t.Fatal(err)}
	if one.ID==two.ID || one.EnvironmentID==two.EnvironmentID || one.Generation!=1 || two.Generation!=1{
		t.Fatalf("two environments shared history: %+v %+v",one,two)
	}
	if _,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,nil,a);!errors.Is(err,ErrAcceptedConflict){
		t.Fatalf("stale A genesis not rejected: %v",err)
	}
	changed:=a
	changed.Environment.Name="renamed"
	next,err:=store.CommitLocalCandidate(ctx,changed.Environment,changed.Environment.Binding,&one,changed)
	if err!=nil||next.ParentID!=one.ID||next.Generation!=2 {t.Fatalf("rename CAS: %+v %v",next,err)}
	reopened,err:=OpenLocalProjectStateStore(store.root,store.binding)
	if err!=nil{t.Fatal(err)}
	got,record,err:=reopened.ReadLocalAccepted(ctx,changed.Environment)
	if err!=nil||got==nil||*got!=next||record.Environment.Name!="renamed"{
		t.Fatalf("restart lost A revision: %+v %+v %v",got,record,err)
	}
	still,original,err:=reopened.ReadLocalAccepted(ctx,b.Environment)
	if err!=nil||still==nil||*still!=two||original.Environment.Name!="secondary"{
		t.Fatalf("A promotion overwrote B: %+v %+v %v",still,original,err)
	}
	history,body,err:=reopened.ReadLocalRevision(ctx,changed.Environment,one.ID)
	if err!=nil||history!=one||body.Environment.Name!=a.Environment.Name{
		t.Fatalf("immutable revision overwritten: %+v %+v %v",history,body,err)
	}
	if _,_,err:=reopened.ReadLocalRevision(ctx,b.Environment,one.ID);err==nil{
		t.Fatal("foreign environment history could be read by another UUID")
	}
	if _,err:=reopened.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,&one,a);!errors.Is(err,ErrAcceptedConflict){
		t.Fatalf("stale expected revision succeeded: %v",err)
	}
}

func TestLocalProjectStoreRejectsIdentityAndSchemaSubstitution(t *testing.T){
	store,a:=newLocalProjectFixture(t)
	ctx:=context.Background()
	if _,err:=OpenLocalProjectStateStore(store.root,Binding{ServerID:"foreign",Project:store.binding.Project});err==nil{
		t.Fatal("alternate server reopened project fixture")
	}
	if _,err:=OpenLocalProjectStateStore(store.root,Binding{ServerID:store.binding.ServerID,Project:"another"});err==nil{
		t.Fatal("alternate project reopened project fixture")
	}
	foreign:=a
	foreign.Environment.ID="7c82e6d1-1212-4123-8123-0123456789ac"
	if _,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,nil,foreign);err==nil{
		t.Fatal("forged environment UUID committed")
	}
	if _,err:=store.CommitLocalCandidate(ctx,a.Environment,Binding{ServerID:"foreign",Project:a.Environment.Binding.Project},nil,a);err==nil{
		t.Fatal("forge observed server context accepted")
	}
	if _,err:=store.CommitLocalCandidate(ctx,a.Environment,Binding{ServerID:a.Environment.Binding.ServerID,Project:"other"},nil,a);err==nil{
		t.Fatal("forge observed project context accepted")
	}
	first,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,nil,a)
	if err!=nil{t.Fatal(err)}
	authority:=filepath.Join(store.root,"authority.json")
	raw,err:=os.ReadFile(authority)
	if err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(authority,append(raw,[]byte(" {}")...),0o600);err!=nil{t.Fatal(err)}
	if _,err:=OpenLocalProjectStateStore(store.root,store.binding);err==nil{
		t.Fatal("corrupt project authority anchor trusted")
	}
	if err:=os.WriteFile(authority,raw,0o600);err!=nil{t.Fatal(err)}
	namespace:=filepath.Join(store.root,"environments",a.Environment.ID)
	pointer:=filepath.Join(namespace,"active.json")
	if err:=os.WriteFile(pointer,[]byte(`{"id":"unsafe"}`),0o600);err!=nil{t.Fatal(err)}
	if _,_,err:=store.ReadLocalAccepted(ctx,a.Environment);err==nil{
		t.Fatal("corrupt active pointer trusted")
	}
	if _,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,&first,a);err==nil{
		t.Fatal("state mutated despite invalid active pointer")
	}
}

func TestLocalProjectStoreRejectsSymlinksAndUntrustedDirectories(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	if _,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c);err!=nil{t.Fatal(err)}
	root:=store.root
	revisions:=filepath.Join(root,"environments",c.Environment.ID,"revisions")
	if err:=os.Chmod(revisions,0o755);err!=nil{t.Fatal(err)}
	if _,_,err:=store.ReadLocalAccepted(ctx,c.Environment);err==nil{
		t.Fatal("world-readable immutable revision directory accepted")
	}
	if err:=os.Chmod(revisions,0o700);err!=nil{t.Fatal(err)}
	if err:=os.Rename(revisions,revisions+"-original");err!=nil{t.Fatal(err)}
	if err:=os.Symlink(revisions+"-original",revisions);err!=nil{t.Fatal(err)}
	if _,_,err:=store.ReadLocalAccepted(ctx,c.Environment);err==nil{
		t.Fatal("symlink revision directory was followed")
	}
	if _,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c);err==nil{
		t.Fatal("wrote through symlink namespace")
	}
	if err:=os.Chmod(root,0o755);err!=nil{t.Fatal(err)}
	if _,err:=OpenLocalProjectStateStore(root,c.Environment.Binding);err==nil{
		t.Fatal("world-readable project store opened")
	}
}

func TestLocalProjectStoreConcurrentCASAndCancel(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	var wg sync.WaitGroup
	out:=make(chan error,12)
	for i:=0;i<12;i++{
		wg.Add(1)
		go func(){
			defer wg.Done()
			_,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
			out<-err
		}()
	}
	wg.Wait()
	close(out)
	wins:=0
	for err:=range out{
		if err==nil {wins++;continue}
		if !errors.Is(err,ErrAcceptedConflict) && !errors.Is(err,filestate.ErrRegistryBusy) &&
			!strings.Contains(err.Error(),"local environment CAS lock unavailable"){
			t.Fatalf("unexpected concurrent outcome: %v",err)
		}
	}
	if wins!=1 {t.Fatalf("expected one CAS genesis, got %d",wins)}
	cancelled, cancel:=context.WithCancel(ctx)
	cancel()
	if _,err:=store.CommitLocalCandidate(cancelled,c.Environment,c.Environment.Binding,nil,c);!errors.Is(err,context.Canceled){
		t.Fatalf("canceled commit advanced state: %v",err)
	}
	if _,_,err:=store.ReadLocalAccepted(cancelled,c.Environment);!errors.Is(err,context.Canceled){
		t.Fatalf("canceled read did not fail: %v",err)
	}
}

func TestLocalProjectStoreRejectsTamperedImmutableBody(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	rev,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	path:=filepath.Join(store.root,"environments",c.Environment.ID,"revisions",rev.ID+".json")
	raw,err:=os.ReadFile(path)
	if err!=nil{t.Fatal(err)}
	var record map[string]json.RawMessage
	if err:=json.Unmarshal(raw,&record);err!=nil{t.Fatal(err)}
	record["candidate"]=json.RawMessage(`{}`)
	corrupted,err:=json.Marshal(record)
	if err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(path,corrupted,0o600);err!=nil{t.Fatal(err)}
	if _,_,err:=store.ReadLocalAccepted(ctx,c.Environment);err==nil{
		t.Fatal("accepted immutable candidate tampering")
	}
}
