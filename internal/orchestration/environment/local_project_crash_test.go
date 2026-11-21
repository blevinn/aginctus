package environment

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalProjectOrphanRevisionIsNotPublishedHistory(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	original,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	nextCandidate:=c
	nextCandidate.Environment.Name="after-crash"
	orphan,err:=BuildAcceptedRevision(&original,&c,nextCandidate)
	if err!=nil{t.Fatal(err)}
	data,err:=nextCandidate.Canonical()
	if err!=nil{t.Fatal(err)}
	record,err:=json.Marshal(localEnvironmentRecord{Revision:orphan,Candidate:data})
	if err!=nil{t.Fatal(err)}
	namespace:=filepath.Join(store.root,"environments",c.Environment.ID)
	if err:=createImmutableLocalRecord(filepath.Join(namespace,"revisions"),orphan.ID+".json",record);err!=nil{
		t.Fatal(err)
	}
	// A process may die at this exact point, before active.json changes.
	active,body,err:=store.ReadLocalAccepted(ctx,c.Environment)
	if err!=nil||active==nil||*active!=original||body.Environment.Name!=c.Environment.Name{
		t.Fatalf("crash orphan silently promoted: %+v %+v %v",active,body,err)
	}
	if _,_,err:=store.ReadLocalRevision(ctx,c.Environment,orphan.ID);err==nil{
		t.Fatal("unpublished immutable record was treated as accepted history")
	}
	// An identical deliberate retry under the expected old pointer can
	// reuse the immutable record, but it must perform its own CAS publish.
	published,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,&original,nextCandidate)
	if err!=nil||published!=orphan {
		t.Fatalf("failed to explicitly publish an orphan after rechecking CAS: %+v %v",published,err)
	}
	confirmed,_,err:=store.ReadLocalRevision(ctx,c.Environment,orphan.ID)
	if err!=nil||confirmed!=orphan {t.Fatalf("published lineage missing: %+v %v",confirmed,err)}
	parent,_,err:=store.ReadLocalRevision(ctx,c.Environment,original.ID)
	if err!=nil||parent!=original{t.Fatalf("accepted parent chain missing: %+v %v",parent,err)}
}

func TestLocalProjectHistoricalLineageRejectsCorruptParent(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	first,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	modified:=c
	modified.Environment.Name="second"
	second,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,&first,modified)
	if err!=nil||second.ParentID!=first.ID{t.Fatalf("second accepted revision: %+v %v",second,err)}
	parentPath:=filepath.Join(store.root,"environments",c.Environment.ID,"revisions",first.ID+".json")
	if err:=os.WriteFile(parentPath,[]byte("corrupt"),0o600);err!=nil{t.Fatal(err)}
	if _,_,err:=store.ReadLocalRevision(ctx,c.Environment,first.ID);err==nil{
		t.Fatal("corrupt accepted parent reported as historical revision")
	}
	// Current pointer remains a separately verifiable record; this fixture
	// never infers that a broken history can be automatically repaired.
	current,_,err:=store.ReadLocalAccepted(ctx,c.Environment)
	if err!=nil||current==nil||*current!=second{
		t.Fatalf("valid active pointer lost: %+v %v",current,err)
	}
}

func TestLocalProjectAttemptProcessCrash(t *testing.T){
	if os.Getenv("AGINCTUS_PROJECT_CRASH_CHILD")=="1" {
		root:=os.Getenv("AGINCTUS_PROJECT_CRASH_ROOT")
		output:=os.Getenv("AGINCTUS_PROJECT_CRASH_ID")
		c:=candidateFixture()
		store,err:=OpenLocalProjectStateStore(root,c.Environment.Binding)
		if err!=nil{t.Fatal(err)}
		rev,_,err:=store.ReadLocalAccepted(context.Background(),c.Environment)
		if err!=nil||rev==nil{t.Fatalf("child accepted pointer unavailable: %v",err)}
		pin,err:=store.PinLocalAccepted(context.Background(),c.Environment,*rev,"beta")
		if err!=nil{t.Fatal(err)}
		record,err:=pin.RecordLocalUnverifiedStart(context.Background())
		if err!=nil{t.Fatal(err)}
		if err:=os.WriteFile(output,[]byte(record.ID),0o600);err!=nil{t.Fatal(err)}
		// Abrupt termination intentionally skips pin.Close and all defers.
		os.Exit(0)
	}

	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	revision,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	recordIDPath:=filepath.Join(t.TempDir(),"record-id")
	cmd:=exec.Command(os.Args[0],"-test.run=^TestLocalProjectAttemptProcessCrash$")
	cmd.Env=append(os.Environ(),
		"AGINCTUS_PROJECT_CRASH_CHILD=1",
		"AGINCTUS_PROJECT_CRASH_ROOT="+store.root,
		"AGINCTUS_PROJECT_CRASH_ID="+recordIDPath,
	)
	if output,err:=cmd.CombinedOutput();err!=nil{
		t.Fatalf("crashed fixture subprocess failed: %v: %s",err,output)
	}
	identity,err:=os.ReadFile(recordIDPath)
	if err!=nil{t.Fatal(err)}
	id:=strings.TrimSpace(string(identity))
	if !localEnvironmentAttemptID.MatchString(id){t.Fatalf("child did not persist a valid operation ID: %q",id)}
	reopened,err:=OpenLocalProjectStateStore(store.root,store.binding)
	if err!=nil{t.Fatal(err)}
	record,err:=reopened.ReadLocalUnverifiedStart(ctx,c.Environment,id)
	if err!=nil||record.Outcome!="unverified"||record.RevisionID!=revision.ID{
		t.Fatalf("process-exit changed unresolved operation outcome: %+v %v",record,err)
	}
	// The kernel released the child flock. No automatic retry or promotion
	// happened, and a second controller can inspect the same accepted state.
	pin,err:=reopened.PinLocalAccepted(ctx,c.Environment,revision,"beta")
	if err!=nil{t.Fatalf("abandoned revision pin survived child crash: %v",err)}
	if err:=pin.Check(ctx);err!=nil{t.Fatal(err)}
	if err:=pin.Close();err!=nil{t.Fatal(err)}
	still,_,err:=reopened.ReadLocalAccepted(ctx,c.Environment)
	if err!=nil||still==nil||*still!=revision{
		t.Fatalf("process crash changed accepted revision: %+v %v",still,err)
	}
	if _,err:=reopened.ReadLocalUnverifiedStart(ctx,c.Environment,"not-a-uuid");err==nil{
		t.Fatal("uncertain history lookup accepted invalid ID")
	}
}

func TestLocalProjectOrphanCollisionNeverClobbersExistingContent(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	ctx:=context.Background()
	first,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	next:=c
	next.Environment.Name="new-name"
	second,err:=BuildAcceptedRevision(&first,&c,next)
	if err!=nil{t.Fatal(err)}
	revisionFile:=filepath.Join(store.root,"environments",c.Environment.ID,"revisions",second.ID+".json")
	if err:=os.WriteFile(revisionFile,[]byte("foreign"),0o600);err!=nil{t.Fatal(err)}
	if _,err:=store.CommitLocalCandidate(ctx,c.Environment,c.Environment.Binding,&first,next);err==nil{
		t.Fatal("tampered content-addressed record was overwritten")
	}
	raw,err:=os.ReadFile(revisionFile)
	if err!=nil||string(raw)!="foreign"{
		t.Fatalf("mutation overwrote orphan collision: %q %v",raw,err)
	}
	active,_,err:=store.ReadLocalAccepted(ctx,c.Environment)
	if err!=nil||active==nil||*active!=first{
		t.Fatalf("failed CAS advanced pointer: %+v %v",active,err)
	}
}
