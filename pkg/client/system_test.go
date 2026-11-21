package client

import(
	"context"
	"errors"
	"testing"
)
type fakeServerVersion struct{version string;err error;calls int}
func (s *fakeServerVersion) ServerVersion(context.Context)(string,error){s.calls++;return s.version,s.err}
func TestSystemStatusReportsOnlyServerObservation(t *testing.T){
	backend:=&fakeServerVersion{version:"7.5.1"}
	result,err:=New(Options{SystemReader:backend}).System.Status(context.Background())
	if err!=nil||result.Kind!="SystemStatus"||result.IncusVersion!="7.5.1"||result.Scope!="server-observation"{t.Fatalf("result=%#v err=%v",result,err)}
	backend.err=errors.New("secret transport details")
	if _,err:=New(Options{SystemReader:backend}).System.Status(context.Background());err==nil||err.Error()=="secret transport details"{t.Fatalf("raw backend error leaked: %v",err)}
	backend.err=nil;backend.version=""
	if _,err:=New(Options{SystemReader:backend}).System.Status(context.Background());err==nil{t.Fatal("empty version treated as success")}
	if _,err:=New(Options{}).System.Status(context.Background());err==nil{t.Fatal("missing provider accepted")}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	before:=backend.calls
	if _,err:=New(Options{SystemReader:backend}).System.Status(ctx);!errors.Is(err,context.Canceled)||backend.calls!=before{t.Fatalf("cancelled: err=%v calls=%d",err,backend.calls)}
}
