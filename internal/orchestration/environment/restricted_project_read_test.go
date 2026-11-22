package environment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type restrictedReadTestSession struct {
	before Binding
	after Binding
	revision *AcceptedRevision
	candidate *Candidate
	err error
	verifications int
	reads int
	readEnvironment string
}

func (s *restrictedReadTestSession) VerifiedBinding(context.Context) (Binding,error) {
	s.verifications++
	if s.err != nil { return Binding{},s.err }
	if s.verifications==1 { return s.before,nil }
	return s.after,nil
}
func (s *restrictedReadTestSession) ReadPublished(_ context.Context, id string) (*AcceptedRevision,*Candidate,error) {
	s.reads++
	s.readEnvironment=id
	if s.err != nil { return nil,nil,s.err }
	return s.revision,s.candidate,nil
}

func TestRestrictedReadRequiresSameConfinedBindingBeforeAndAfter(t *testing.T) {
	c:=candidateFixture()
	rev,err:=BuildAcceptedRevision(nil,nil,c)
	if err!=nil { t.Fatal(err) }
	s:=&restrictedReadTestSession{before:c.Environment.Binding,after:c.Environment.Binding,revision:&rev,candidate:&c}
	got,body,err:=ReadRestrictedProjectAccepted(context.Background(),s,c.Environment)
	if err!=nil || got==nil || got.ID!=rev.ID || body==nil || s.readEnvironment!=c.Environment.ID || s.verifications!=2 {
		t.Fatalf("scoped read: %+v %v",got,err)
	}
	s.verifications=0
	s.reads=0
	s.after.Project="foreign"
	if got,body,err:=ReadRestrictedProjectAccepted(context.Background(),s,c.Environment);err==nil || got!=nil || body!=nil || s.reads!=1 {
		t.Fatal("accepted retargeted project")
	}
	s.verifications=0
	s.reads=0
	s.before.ServerID="foreign"
	if _,_,err:=ReadRestrictedProjectAccepted(context.Background(),s,c.Environment);err==nil || s.reads!=0 {
		t.Fatal("read before authenticated matching server")
	}
}

func TestRestrictedReadRejectsCorruptionErrorsAndForeignScope(t *testing.T) {
	c:=candidateFixture()
	s:=&restrictedReadTestSession{before:c.Environment.Binding,after:c.Environment.Binding}
	if r,b,err:=ReadRestrictedProjectAccepted(context.Background(),s,c.Environment);err!=nil || r!=nil || b!=nil {
		t.Fatalf("verified absence: %v",err)
	}
	rev,err:=BuildAcceptedRevision(nil,nil,c)
	if err!=nil {t.Fatal(err)}
	s.verifications=0
	s.revision=&rev
	if _,_,err:=ReadRestrictedProjectAccepted(context.Background(),s,c.Environment);err==nil {
		t.Fatal("accepted incomplete record")
	}
	s.verifications=0
	foreign:=c
	foreign.Environment.ID="7c82e6d1-1212-4123-8123-0123456789ac"
	s.candidate=&foreign
	if _,_,err:=ReadRestrictedProjectAccepted(context.Background(),s,c.Environment);err==nil {
		t.Fatal("accepted foreign candidate")
	}
	s.verifications=0
	s.candidate=&c
	s.err=errors.New("sensitive transport certificate")
	if _,_,err:=ReadRestrictedProjectAccepted(context.Background(),s,c.Environment);err==nil || strings.Contains(err.Error(),"sensitive") {
		t.Fatal("source error leaked or passed")
	}
	var nilContext context.Context
	if _,_,err:=ReadRestrictedProjectAccepted(nilContext,s,c.Environment);err==nil {
		t.Fatal("accepted nil context")
	}
	ctx,cancel:=context.WithCancel(context.Background())
	cancel()
	if _,_,err:=ReadRestrictedProjectAccepted(ctx,s,c.Environment);!errors.Is(err,context.Canceled) {
		t.Fatal("canceled operation continued")
	}
}
