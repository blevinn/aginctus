package client

import (
	"context"
	"errors"
	"testing"
)
type testConfigSelection struct{project string;err error;calls int}
func (s *testConfigSelection) SelectedIncusProject(context.Context)(string,error){s.calls++;return s.project,s.err}
func TestConfigSelectionDoesNotClaimIncusAuthority(t *testing.T){
	source:=&testConfigSelection{project:"aginctus"}
	c:=New(Options{ConfigSelectionReader:source})
	result,err:=c.Config.Selection(context.Background())
	if err!=nil||result.Kind!="ConfigSelection"||result.IncusProject!="aginctus"||result.Authority!="preference-only"{t.Fatalf("selection=%+v err=%v",result,err)}
	for _,project:=range []string{"","default"}{
		source.project=project
		if _,err:=c.Config.Selection(context.Background());err==nil{t.Fatalf("unsafe project %q accepted",project)}
	}
	source.project="aginctus";source.err=errors.New("sensitive source error")
	if _,err:=c.Config.Selection(context.Background());err==nil||err.Error()=="sensitive source error"{t.Fatalf("raw source error leaked: %v",err)}
	if _,err:=New(Options{}).Config.Selection(context.Background());err==nil{t.Fatal("missing reader returned empty success")}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	before:=source.calls
	if _,err:=c.Config.Selection(ctx);!errors.Is(err,context.Canceled)||source.calls!=before{t.Fatalf("cancelled read reached source: %v",err)}
}
