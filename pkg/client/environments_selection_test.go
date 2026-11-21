package client

import (
	"context"
	"testing"
)
func TestEnvironmentSelectionIsPerClientAndExplicitOverrides(t *testing.T){
	reader:=&testEnvironmentReader{}
	one:=New(Options{EnvironmentReader:reader,DefaultProject:"project-a",DefaultEnvironmentID:"11111111-1111-4111-8111-111111111111"})
	two:=New(Options{EnvironmentReader:reader,DefaultProject:"project-b",DefaultEnvironmentID:"22222222-2222-4222-8222-222222222222"})
	a,err:=one.Environments.Selected(context.Background(),GetEnvironmentRequest{})
	if err!=nil||a.ID!="11111111-1111-4111-8111-111111111111"{t.Fatalf("first client: %#v %v",a,err)}
	b,err:=two.Environments.Selected(context.Background(),GetEnvironmentRequest{})
	if err!=nil||b.ID!="22222222-2222-4222-8222-222222222222"{t.Fatalf("second client: %#v %v",b,err)}
	override,err:=one.Environments.Selected(context.Background(),GetEnvironmentRequest{ID:"33333333-3333-4333-8333-333333333333",Project:"project-c"})
	if err!=nil||override.ID!="33333333-3333-4333-8333-333333333333"{t.Fatalf("explicit override: %#v %v",override,err)}
	again,err:=one.Environments.Selected(context.Background(),GetEnvironmentRequest{})
	if err!=nil||again.ID!=a.ID {t.Fatal("explicit override changed client default")}
	if _,err:=New(Options{EnvironmentReader:reader}).Environments.Selected(context.Background(),GetEnvironmentRequest{});err==nil {
		t.Fatal("missing environment selected ambient fallback")
	}
}
