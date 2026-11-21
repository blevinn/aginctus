package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/pkg/api"
)

func TestConfigScopeStructuredOutputAndNoAuthorityClaim(t *testing.T) {
	loader:=config.NewLoader()
	loader.SystemPath=""
	loader.UserPath=""
	loader.Environment=nil
	for _,tc:=range []struct{args []string;want string}{
		{[]string{"config","scope","-o","json"},`"kind": "ConfigSelection"`},
		{[]string{"--output=yaml","config","scope"},"authority: preference-only"},
		{[]string{"config","scope","-o","name"},"aginctus"},
		{[]string{"config","scope"},"AUTHORITY"},
	}{
		var out,errs bytes.Buffer
		code:=Run(context.Background(),tc.args,&out,&errs,nil,loader)
		if code!=0||errs.Len()!=0||!strings.Contains(out.String(),tc.want){
			t.Fatalf("args=%v code=%d out=%q err=%q",tc.args,code,out.String(),errs.String())
		}
		if strings.Contains(strings.Join(tc.args," "),"json"){
			var doc api.ConfigSelection
			if err:=json.Unmarshal(out.Bytes(),&doc);err!=nil||doc.Authority!="preference-only"||doc.IncusProject!="aginctus"{
				t.Fatalf("incorrect selection=%+v err=%v",doc,err)
			}
		}
	}
}
func TestConfigScopeDoesNotExtendLegacyOutputGate(t *testing.T){
	loader:=config.NewLoader();loader.SystemPath="";loader.UserPath="";loader.Environment=nil
	for _,args:=range [][]string{
		{"config","get","incus.project","-o","json"},
		{"config","scope","-o","invalid"},
	}{
		var out,errs bytes.Buffer
		if code:=Run(context.Background(),args,&out,&errs,nil,loader);code==0||out.Len()!=0{
			t.Fatalf("args=%v code=%d out=%q err=%q",args,code,out.String(),errs.String())
		}
	}
}
