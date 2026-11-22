package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestAcceptancePlanCLIUsesVerifiedStateRead(t *testing.T) {
	fixture:=stateCLIFixture{}
	for _,tc:=range []struct{args []string;expected string}{
		{[]string{"state","plan-accept","-o","json"},`"kind": "AcceptancePlan"`},
		{[]string{"state","plan-accept","-o","yaml"},"candidateDigest: sha256:abc"},
		{[]string{"state","plan-accept","-o","table"},"ADVISORY ACTION"},
		{[]string{"state","plan-accept","-o","name"},"worker"},
	}{
		args:=append([]string{"--environment",stateCLIEnv,"--incus-project","team"},tc.args...)
		var out,errOut bytes.Buffer
		code:=RunWithStateReaders(context.Background(),args,&out,&errOut,nil,nil,nil,fixture,fixture,fixture)
		if code!=0||errOut.Len()!=0||!strings.Contains(out.String(),tc.expected){
			t.Fatalf("args=%v code=%d out=%q err=%q",args,code,out.String(),errOut.String())
		}
		if strings.Contains(out.String(),"remove")||strings.Contains(out.String(),"delete"){
			t.Fatalf("preview accidentally includes destructive operation: %q",out.String())
		}
	}
}

func TestAcceptancePlanCLIRejectsAbsentAuthority(t *testing.T) {
	for _,args:=range [][]string{
		{"state","plan-accept","-o","json"},
		{"--environment",stateCLIEnv,"--incus-project","team","state","plan-accept","-o","json"},
		{"--environment",stateCLIEnv,"state","plan-accept","-o","yaml"},
	}{
		var out,errOut bytes.Buffer
		if code:=Run(context.Background(),args,&out,&errOut,nil,nil);code==0||out.Len()!=0{
			t.Fatalf("args=%v code=%d out=%q err=%q",args,code,out.String(),errOut.String())
		}
	}
}
