package environment

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureUnixClient(socket string)*http.Client{
	return &http.Client{Timeout:2*time.Second,Transport:&http.Transport{
		DialContext:func(ctx context.Context,_,_ string)(net.Conn,error){
			return (&net.Dialer{}).DialContext(ctx,"unix",socket)
		},
	}}
}

func TestProjectReadOnlySocketReportsOnlyExactAcceptedEnvironment(t *testing.T){
	store,c:=newLocalProjectFixture(t)
	rev,err:=store.CommitLocalCandidate(context.Background(),c.Environment,c.Environment.Binding,nil,c)
	if err!=nil{t.Fatal(err)}
	directory:=filepath.Join(t.TempDir(),"run")
	if err:=os.Mkdir(directory,0o700);err!=nil{t.Fatal(err)}
	socket:=filepath.Join(directory,"status.sock")
	ctx,cancel:=context.WithCancel(context.Background())
	defer cancel()
	svc,err:=StartLocalProjectReadOnly(ctx,store,socket)
	if err!=nil{t.Fatal(err)}
	defer svc.Close()
	client:=fixtureUnixClient(socket)
	get:=func(path string,method string)(int,[]byte){
		t.Helper()
		req,err:=http.NewRequest(method,"http://unix"+path,nil)
		if err!=nil{t.Fatal(err)}
		response,err:=client.Do(req)
		if err!=nil{t.Fatal(err)}
		defer response.Body.Close()
		var body json.RawMessage
		_ = json.NewDecoder(response.Body).Decode(&body)
		return response.StatusCode,body
	}
	if status,_:=get("/v1/health","GET");status!=http.StatusNoContent{t.Fatalf("health code %d",status)}
	status,body:=get("/v1/accepted?environment="+c.Environment.ID,"GET")
	if status!=http.StatusOK{t.Fatalf("unexpected accepted response: %d: %s",status,body)}
	var result struct{EnvironmentID string `json:"environmentId"`; Revision AcceptedRevision `json:"revision"`}
	if err:=json.Unmarshal(body,&result);err!=nil || result.EnvironmentID!=c.Environment.ID || result.Revision!=rev{
		t.Fatalf("bad accepted response: %+v %v",result,err)
	}
	for _,path:=range []string{
		"/v1/accepted", "/v1/accepted?environment=default",
		"/v1/accepted?environment="+c.Environment.ID+"&project=other",
		"/v1/accepted?environment="+c.Environment.ID+"&environment="+c.Environment.ID,
		"/v1/health?project=other",
	}{
		status,_:=get(path,"GET")
		if status!=http.StatusBadRequest {t.Fatalf("accepted ambiguous selector %s: %d",path,status)}
	}
	if status,_:=get("/v1/accepted?environment="+c.Environment.ID,"POST");status!=http.StatusMethodNotAllowed{
		t.Fatalf("write method accepted: %d",status)
	}
	if status,_:=get("/v1/accepted?environment=22222222-2222-4222-8222-222222222222","GET");status!=http.StatusNotFound{
		t.Fatalf("invented missing accepted history: %d",status)
	}
	if _,err:=StartLocalProjectReadOnly(ctx,store,socket);err==nil{
		t.Fatal("adopted an already bound socket")
	}
	if err:=svc.Close();err!=nil{t.Fatal(err)}
	if _,err:=os.Lstat(socket);!errors.Is(err,os.ErrNotExist){
		t.Fatal("original service socket survived close")
	}
}

func TestProjectReadOnlySocketNeverDeletesForeignReplacement(t *testing.T){
	store,_:=newLocalProjectFixture(t)
	dir:=filepath.Join(t.TempDir(),"socket")
	if err:=os.Mkdir(dir,0o700);err!=nil{t.Fatal(err)}
	socket:=filepath.Join(dir,"status.sock")
	svc,err:=StartLocalProjectReadOnly(context.Background(),store,socket)
	if err!=nil{t.Fatal(err)}
	if err:=os.Remove(socket);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(socket,[]byte("foreign-owned-data"),0o600);err!=nil{t.Fatal(err)}
	if err:=svc.Close();err!=nil{t.Fatal(err)}
	if b,err:=os.ReadFile(socket);err!=nil||strings.TrimSpace(string(b))!="foreign-owned-data"{
		t.Fatalf("removed foreign socket replacement: %s %v",b,err)
	}
}
