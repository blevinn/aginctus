package environment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRecoveryInventoryTwoEnvironmentScopes(t *testing.T) {
	store,a:=newLocalProjectFixture(t)
	ctx:=context.Background()
	b:=a
	b.Environment.ID="7c82e6d1-1212-4123-8123-0123456789ac"
	b.Environment.Name="second"
	first,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,nil,a)
	if err!=nil{t.Fatal(err)}
	second,err:=store.CommitLocalCandidate(ctx,b.Environment,b.Environment.Binding,nil,b)
	if err!=nil{t.Fatal(err)}
	out,err:=store.InspectLocalRecoveryInventory(ctx,[]Descriptor{b.Environment,a.Environment})
	if err!=nil {t.Fatal(err)}
	if out.Project!=a.Environment.Binding.Project || out.ServerID!=a.Environment.Binding.ServerID ||
		len(out.Environments)!=2 || out.Environments[0].EnvironmentID!=a.Environment.ID ||
		out.Environments[0].HeadID!=first.ID || out.Environments[1].HeadID!=second.ID ||
		out.Environments[0].Generation!=1 || out.Environments[1].Reachable!=1 {
		t.Fatalf("invalid recovery inventory: %+v",out)
	}
	reopened,err:=OpenLocalProjectStateStore(store.root,store.binding)
	if err!=nil {t.Fatal(err)}
	again,err:=reopened.InspectLocalRecoveryInventory(ctx,[]Descriptor{a.Environment,b.Environment})
	if err!=nil || again.Environments[0].HeadID!=first.ID {
		t.Fatalf("reopened inventory: %+v %v",again,err)
	}
}

func TestLocalRecoveryInventoryRejectsAmbiguousAndCorruptScope(t *testing.T) {
	store,a:=newLocalProjectFixture(t)
	ctx:=context.Background()
	if _,err:=store.InspectLocalRecoveryInventory(ctx,nil);err==nil {t.Fatal("accepted implicit inventory")}
	if _,err:=store.InspectLocalRecoveryInventory(ctx,[]Descriptor{a.Environment,a.Environment});err==nil {
		t.Fatal("accepted duplicate environment")
	}
	foreign:=a.Environment
	foreign.Binding.Project="foreign"
	if _,err:=store.InspectLocalRecoveryInventory(ctx,[]Descriptor{a.Environment,foreign});err==nil {
		t.Fatal("accepted cross-project scope")
	}
	rev,err:=store.CommitLocalCandidate(ctx,a.Environment,a.Environment.Binding,nil,a)
	if err!=nil {t.Fatal(err)}
	path:=filepath.Join(store.root,"environments",a.Environment.ID,"revisions",rev.ID+".json")
	if err:=os.WriteFile(path,[]byte("{}"),0o600);err!=nil{t.Fatal(err)}
	if _,err:=store.InspectLocalRecoveryInventory(ctx,[]Descriptor{a.Environment});err==nil {
		t.Fatal("accepted corrupt published revision")
	}
	cancelled,cancel:=context.WithCancel(ctx)
	cancel()
	if _,err:=store.InspectLocalRecoveryInventory(cancelled,[]Descriptor{a.Environment});err==nil {
		t.Fatal("accepted cancelled recovery inspection")
	}
}
