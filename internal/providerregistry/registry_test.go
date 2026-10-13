package providerregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry", "providers.json")
	p := Provider{Type: "chatgpt", Alias: "personal"}
	first, err := Register(path, p)
	if err != nil || !first {
		t.Fatalf("first=%v err=%v", first, err)
	}
	again, err := Register(path, p)
	if err != nil || again {
		t.Fatalf("again=%v err=%v", again, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "\"personal\"") != 1 {
		t.Fatal(string(b))
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file perms: %v, %v", info, err)
	}
}
func TestRejectInsecureOrUnsupported(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "providers.json")
	if _, err := Register(path, Provider{Type: "anthropic", Alias: "one"}); err == nil {
		t.Fatal("unsupported provider")
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "../bad"}); err == nil {
		t.Fatal("unsafe alias")
	}
	if err := os.Symlink("other", path); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "chatgpt"}); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestRegistryLockFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.Mkdir(path+".lock", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(path, Provider{Type: "chatgpt", Alias: "chatgpt"}); err == nil {
		t.Fatal("accepted concurrent lock")
	}
}

func TestLoadPrivateRegistry(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"registry","providers.json")
 reg,err:=Load(path);if err!=nil||len(reg.Providers)!=0{t.Fatalf("missing: %#v %v",reg,err)}
 if _,err:=Register(path,Provider{Type:"chatgpt",Alias:"personal"});err!=nil{t.Fatal(err)}
 reg,err=Load(path);if err!=nil||len(reg.Providers)!=1||reg.Providers[0].Alias!="personal"{t.Fatalf("read: %#v %v",reg,err)}
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=Load(path);err==nil{t.Fatal("insecure file accepted")}
}
func TestLoadRejectsSymlink(t *testing.T) {
 dir:=t.TempDir();path:=filepath.Join(dir,"providers.json")
 if err:=os.Symlink("missing",path);err!=nil{t.Fatal(err)}
 if _,err:=Load(path);err==nil{t.Fatal("symlink accepted")}
}
