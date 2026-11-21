package cli

import(
	"bytes"
	"context"
	"strings"
	"testing"
)
func TestWorkloadClassAliasesFilterVerifiedResources(t *testing.T){
	const id="11111111-1111-4111-8111-111111111111"
	for _,tc:=range []struct{name string;command string;success bool}{
		{"gateway list","gateways list",true},
		{"gateway show","gateways show gateway",true},
		{"gateway status","gateways status gateway",true},
		{"wrong class list","agents list",false},
		{"wrong class show","frontends show gateway",false},
		{"wrong class status","agents status gateway",false},
	}{
		args:=append([]string{"--environment",id,"--incus-project","aginctus"},strings.Fields(tc.command)...)
		args=append(args,"-o","json")
		var out,stderr bytes.Buffer
		code:=RunWithLogicalWorkloads(context.Background(),args,&out,&stderr,nil,nil,nil,testLogicalWorkloadReader{})
		if tc.success {
			if code!=0||stderr.Len()!=0||out.Len()==0{t.Fatalf("%s: code=%d out=%q err=%q",tc.name,code,out.String(),stderr.String())}
		}else if code==0||out.Len()!=0{t.Fatalf("%s allowed wrong class: code=%d out=%q err=%q",tc.name,code,out.String(),stderr.String())}
	}
}
