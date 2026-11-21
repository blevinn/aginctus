package gatewaycontrol

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetActionsPreserveReadOnlySelectionAndMutations(t *testing.T) {
	s := testStore(t)
	if _, err := s.MCPInventory("missing"); err == nil { t.Fatal("accepted unknown provider filter") }
	if _, err := s.SelectMCP("missing", "", true); err == nil { t.Fatal("accepted missing local MCP") }
	if _, err := s.SelectModel(context.Background(), "", "missing"); err == nil { t.Fatal("accepted missing model") }
	if _, err := os.Stat(s.Directory); !os.IsNotExist(err) { t.Fatal("read-only access initialized store") }
	if _, err := s.ApplyProviderInstance("not-compiled", "x", Configuration{}); err == nil { t.Fatal("accepted unknown plugin") }
	if _, err := os.Stat(s.Directory); !os.IsNotExist(err) { t.Fatal("invalid plugin mutated store") }
	p, err := s.ApplyProviderInstance("chatgpt", "personal", Configuration{Models: map[string]Model{"coding":{Source:"chatgpt/fixture-model"}}})
	if err != nil || p.Name != "personal" { t.Fatalf("apply provider: %+v %v", p, err) }
	got, err := s.FindProvider("personal")
	if err != nil || got.Plugin != "chatgpt" { t.Fatalf("find provider: %+v %v",got,err) }
	models, err := s.ProviderModels(context.Background(), "personal")
	if err != nil || len(models["personal"]) != 1 { t.Fatalf("models: %v %v",models,err) }
	selected, err := s.SelectModel(context.Background(), "personal", "coding")
	if err != nil || selected["personal"].Name != "coding" { t.Fatalf("model selection: %v %v",selected,err) }
	mcp := []byte(`{"transport":"streamable-http","url":"https://tools.example.invalid/mcp","credentialRef":"fixture","allowedTools":["search"]}`)
	if _, err := s.ApplyMCPDeclaration("search", mcp); err != nil { t.Fatal(err) }
	if _, err := s.SelectMCP("search", "", true); err != nil { t.Fatal(err) }
	inventory, err := s.MCPInventory("")
	if err != nil || inventory["local"] == nil { t.Fatalf("MCP inventory: %v %v",inventory,err) }
	catalog := Catalog{Models:map[string]Asset{"coding":{Provider:"personal",Name:"coding"}}}
	if _, err := s.ApplyCatalogDeclaration("work",catalog); err != nil { t.Fatal(err) }
	names, err := s.CatalogNames()
	if err != nil || len(names)!=1 || names[0]!="work" { t.Fatalf("catalog list: %v %v",names,err) }
	if _,err := s.SelectCatalog("work","coding",""); err!=nil { t.Fatal(err) }
	if _,err := s.SelectCatalog("work","missing",""); err==nil { t.Fatal("unknown catalog model accepted") }
	if _,err := s.ApplyMCPDeclaration("secret",[]byte(`{"transport":"stdio","command":"/tool","env":{"SECRET":"private"}}`)); err==nil {
		t.Fatal("unsafe MCP accepted")
	}
	if _,err := s.SelectMCP("secret","",true); err==nil { t.Fatal("failed MCP mutation left state") }
}

func TestProviderSessionRetainsSelectionAndConsentBoundaries(t *testing.T) {
	ctx:=context.Background()
	calls:=[]string{}
	var got LoginOptions
	s:=&Store{Directory:filepath.Join(t.TempDir(),"gateway"),Plugins:Plugins{
		"fixture":Plugin{
			Name:"fixture",DefaultProvider:"default",
			Login:func(_ context.Context,_ Provider,options LoginOptions)error{calls=append(calls,"login");got=options;return nil},
			Status:func(context.Context,Provider)(any,error){calls=append(calls,"status");return map[string]string{"status":"ok"},nil},
			Logout:func(context.Context,Provider)error{calls=append(calls,"logout");return nil},
		},
	}}
	if err:=s.LoginProvider(ctx,"",LoginOptions{});err==nil{t.Fatal("empty registry accepted implicit login")}
	if _,err:=s.ApplyProviderInstance("fixture","first",Configuration{});err!=nil{t.Fatal(err)}
	if _,err:=s.ApplyProviderInstance("fixture","second",Configuration{});err!=nil{t.Fatal(err)}
	if err:=s.LoginProvider(ctx,"",LoginOptions{});err==nil{t.Fatal("ambiguous provider login")}
	if len(calls)!=0 { t.Fatal("login invoked on denied selection") }
	options:=LoginOptions{Output:io.Discard,EnablePlan:true,ConsentConnectedApp:true,ConsentBackground:false}
	if err:=s.LoginProvider(ctx,"first",options);err!=nil{t.Fatal(err)}
	if got.Output!=io.Discard||!got.EnablePlan||!got.ConsentConnectedApp||got.ConsentBackground{
		t.Fatalf("consent widened or lost: %+v",got)
	}
	if _,err:=s.ProviderSession(ctx,"first","status");err!=nil{t.Fatal(err)}
	if _,err:=s.ProviderSession(ctx,"first","logout");err!=nil{t.Fatal(err)}
	if _,err:=s.ProviderSession(ctx,"first","login");err==nil{t.Fatal("status endpoint accepted login")}
	if strings.Join(calls,",")!="login,status,logout"{t.Fatalf("unexpected plugin calls: %v",calls)}
	_,err:=json.Marshal(s.Plugins)
	if err!=nil {t.Fatal(err)}
}
