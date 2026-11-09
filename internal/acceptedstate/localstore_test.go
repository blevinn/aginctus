package acceptedstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLocalStoreRestartAndCAS(t *testing.T){
	root:=filepath.Join(t.TempDir(),"private-state")
	store,err:=InitializeLocalStore(root)
	if err!=nil {t.Fatal(err)}
	if r,_,err:=store.ReadActive();err!=nil || r!=nil{t.Fatalf("new state: %v %+v",err,r)}
	s:=validSpec()
	first,err:=store.CommitLocal(0,"",s)
	if err!=nil {t.Fatal(err)}
	restarted,err:=OpenLocalStore(root)
	if err!=nil{t.Fatal(err)}
	got,decoded,err:=restarted.ReadActive()
	if err!=nil || got==nil || *got!=first {t.Fatalf("reload %v %+v",err,got)}
	if digest,_:=decoded.Digest();digest!=first.SpecDigest{t.Fatal("lost accepted spec")}
	if _,err:=restarted.CommitLocal(0,"",s);!errors.Is(err,ErrRevisionConflict){t.Fatalf("stale CAS: %v",err)}
	s.Workloads[0].ImageFingerprint=strings.Repeat("e",64)
	second,err:=restarted.CommitLocal(first.Generation,first.ID,s)
	if err!=nil || second.Generation!=2 || second.ParentID!=first.ID{t.Fatalf("new generation: %+v %v",second,err)}
	if _,err:=restarted.CommitLocal(second.Generation,second.ID,s);!errors.Is(err,ErrNoChanges){t.Fatalf("no-op accepted: %v",err)}
}

func TestLocalStoreRaceHasOneWinner(t *testing.T) {
	store,err:=InitializeLocalStore(filepath.Join(t.TempDir(),"state"))
	if err!=nil{t.Fatal(err)}
	s:=validSpec()
	var wg sync.WaitGroup
	out:=make(chan error,10)
	for i:=0;i<10;i++{
		wg.Add(1)
		go func(){defer wg.Done();_,err:=store.CommitLocal(0,"",s);out<-err}()
	}
	wg.Wait();close(out)
	wins:=0
	for err:=range out{
		if err==nil {wins++;continue}
		if !errors.Is(err,ErrRevisionConflict) && !strings.Contains(err.Error(),"lock local accepted revision"){t.Fatalf("unexpected race error: %v",err)}
	}
	if wins!=1 {t.Fatalf("expected one winner, got %d",wins)}
}

func TestLocalStoreRejectsCorruptStateAndSymlinks(t *testing.T){
	base:=t.TempDir()
	root:=filepath.Join(base,"state")
	s,err:=InitializeLocalStore(root)
	if err!=nil{t.Fatal(err)}
	_,err=s.CommitLocal(0,"",validSpec())
	if err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(root,"active.json"),[]byte("truncated"),0o600);err!=nil{t.Fatal(err)}
	if _,_,err:=s.ReadActive();err==nil{t.Fatal("accepted corrupt active pointer")}
	if _,err:=s.CommitLocal(1,"anything",validSpec());err==nil{t.Fatal("mutated with corrupt pointer")}
	if err:=os.Remove(filepath.Join(root,"active.json"));err!=nil{t.Fatal(err)}
	if err:=os.Symlink("/etc/passwd",filepath.Join(root,"active.json"));err!=nil{t.Fatal(err)}
	if _,_,err:=s.ReadActive();err==nil{t.Fatal("followed untrusted symlink")}
	foreign:=filepath.Join(base,"foreign")
	if err:=os.Symlink(root,foreign);err!=nil{t.Fatal(err)}
	if _,err:=OpenLocalStore(foreign);err==nil{t.Fatal("trusted symlink root")}
}

func TestLocalStoreRestrictivePermissions(t *testing.T){
	root:=filepath.Join(t.TempDir(),"state")
	s,err:=InitializeLocalStore(root)
	if err!=nil{t.Fatal(err)}
	if err:=os.Chmod(root,0o755);err!=nil{t.Fatal(err)}
	if _,err:=OpenLocalStore(root);err==nil{t.Fatal("trusted world-readable state")}
	if _,err:=s.CommitLocal(0,"",validSpec());err==nil{t.Fatal("wrote into untrusted state")}
}
