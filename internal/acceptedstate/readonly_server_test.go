package acceptedstate

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func socketClient(path string) *http.Client {
	return &http.Client{
		Timeout: 5*time.Second,
		Transport: &http.Transport{DialContext:func(ctx context.Context,network,address string)(net.Conn,error){
			var d net.Dialer
			return d.DialContext(ctx,"unix",path)
		}},
	}
}

func TestReadOnlyUnixServiceRestart(t *testing.T) {
	root:=filepath.Join(t.TempDir(),"state")
	store,err:=InitializeLocalStore(root)
	if err!=nil{t.Fatal(err)}
	accepted,err:=store.CommitLocal(0,"",validSpec())
	if err!=nil{t.Fatal(err)}
	socket:=filepath.Join(root,"status.sock")
	ctx,cancel:=context.WithCancel(context.Background())
	svc,err:=StartReadOnlyUnixService(ctx,store,socket)
	if err!=nil{t.Fatal(err)}
	client:=socketClient(socket)
	check:=func(method,url string,want int){
		t.Helper()
		req,err:=http.NewRequest(method,"http://unix"+url,nil)
		if err!=nil{t.Fatal(err)}
		res,err:=client.Do(req)
		if err!=nil{t.Fatal(err)}
		defer res.Body.Close()
		if res.StatusCode!=want{t.Fatalf("%s %s got %d want %d",method,url,res.StatusCode,want)}
		if method=="GET" && want==http.StatusOK {
			var doc struct{ Revision Revision; Spec DeploymentSpec }
			if err:=json.NewDecoder(res.Body).Decode(&doc);err!=nil{t.Fatal(err)}
			if doc.Revision.ID!=accepted.ID || doc.Spec.Selection.DeploymentID!="demo" {t.Fatal("wrong accepted revision")}
		}
	}
	check("GET","/v1/health",204)
	check("GET","/v1/active?deployment=demo",200)
	check("GET","/v1/active?deployment=foreign",404)
	check("GET","/v1/active",400)
	check("POST","/v1/active?deployment=demo",405)
	cancel()
	if err:=svc.Close();err!=nil{t.Fatal(err)}
	reopened,err:=OpenLocalStore(root)
	if err!=nil{t.Fatal(err)}
	ctx2,cancel2:=context.WithCancel(context.Background())
	defer cancel2()
	next,err:=StartReadOnlyUnixService(ctx2,reopened,socket)
	if err!=nil{t.Fatal(err)}
	defer next.Close()
	check("GET","/v1/active?deployment=demo",200)
}

func TestReadOnlyUnixServiceRejectsUnsafeSocketPath(t *testing.T){
	base:=t.TempDir()
	root:=filepath.Join(base,"state")
	store,err:=InitializeLocalStore(root)
	if err!=nil{t.Fatal(err)}
	ctx,cancel:=context.WithCancel(context.Background())
	defer cancel()
	if _,err:=StartReadOnlyUnixService(ctx,store,filepath.Join(base,"exposed.sock"));err==nil{t.Fatal("accepted nonprivate socket parent")}
	path:=filepath.Join(root,"status.sock")
	if err:=os.WriteFile(path,[]byte("foreign"),0o600);err!=nil{t.Fatal(err)}
	if _,err:=StartReadOnlyUnixService(ctx,store,path);err==nil{t.Fatal("adopted existing path")}
	if data,err:=os.ReadFile(path);err!=nil || string(data)!="foreign"{t.Fatal("modified foreign path")}
}
