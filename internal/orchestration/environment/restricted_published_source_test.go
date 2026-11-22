package environment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testRestrictedFactory struct {
	session RestrictedProjectReadSession
	err error
	calls int
}
func (s *testRestrictedFactory) OpenReadSession(context.Context,Descriptor)(RestrictedProjectReadSession,error){
	s.calls++
	return s.session,s.err
}

func TestRestrictedPublishedSourceBindsToAuthenticatedSession(t *testing.T){
	c:=candidateFixture()
	rev,err:=BuildAcceptedRevision(nil,nil,c)
	if err!=nil{t.Fatal(err)}
	session:=&restrictedReadTestSession{before:c.Environment.Binding,after:c.Environment.Binding,revision:&rev,candidate:&c}
	factory:=&testRestrictedFactory{session:session}
	source:=RestrictedPublishedSource{Factory:factory}
	result,body,err:=source.ReadAccepted(context.Background(),c.Environment)
	if err!=nil||result==nil||result.ID!=rev.ID||body==nil||session.readEnvironment!=c.Environment.ID||
		session.verifications!=2||factory.calls!=1 {
		t.Fatalf("restricted read failed: rev=%+v candidate=%+v err=%v",result,body,err)
	}
	session.verifications=0
	session.after.Project="foreign"
	if got,body,err:=source.ReadAccepted(context.Background(),c.Environment);err==nil||got!=nil||body!=nil{
		t.Fatalf("project retarget escaped: rev=%+v candidate=%+v err=%v",got,body,err)
	}
}

func TestRestrictedPublishedSourceRejectsMissingAuthorityAndLeakyErrors(t *testing.T){
	c:=candidateFixture()
	if _,_,err:=(RestrictedPublishedSource{}).ReadAccepted(context.Background(),c.Environment);err==nil {
		t.Fatal("missing session factory accepted")
	}
	factory:=&testRestrictedFactory{err:errors.New("secret private session credential")}
	source:=RestrictedPublishedSource{Factory:factory}
	if rev,body,err:=source.ReadAccepted(context.Background(),c.Environment);
		err==nil||rev!=nil||body!=nil||strings.Contains(err.Error(),"credential") {
		t.Fatalf("raw factory error leaked: %v",err)
	}
	factory.calls=0
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if _,_,err:=source.ReadAccepted(ctx,c.Environment);!errors.Is(err,context.Canceled)||factory.calls!=0{
		t.Fatalf("canceled session opened: %v calls=%d",err,factory.calls)
	}
	factory.err=nil
	factory.session=nil
	if _,_,err:=source.ReadAccepted(context.Background(),c.Environment);err==nil{
		t.Fatal("nil restricted session treated as verified absence")
	}
}
