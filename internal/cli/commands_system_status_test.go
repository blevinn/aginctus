package cli

import(
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)
func TestSystemStatusTypedOutput(t *testing.T){
	for _,tc:=range []struct{args []string;want string}{
		{[]string{"system","status"},"INCUS VERSION"},
		{[]string{"system","status","-o","json"},`"kind": "SystemStatus"`},
		{[]string{"-o","yaml","system","status"},"scope: server-observation"},
		{[]string{"system","status","-o","name"},"7.5.1"},
	}{
		var out,errOut bytes.Buffer
		code:=Run(context.Background(),tc.args,&out,&errOut,&fakeIncusClient{version:"7.5.1"},nil)
		if code!=0||errOut.Len()!=0||!strings.Contains(out.String(),tc.want){
			t.Fatalf("args=%v code=%d out=%q stderr=%q",tc.args,code,out.String(),errOut.String())
		}
		if strings.Contains(strings.Join(tc.args," "),"-o json"){
			var result api.SystemStatus
			if err:=json.Unmarshal(out.Bytes(),&result);err!=nil||result.Scope!="server-observation"{
				t.Fatalf("result=%+v err=%v",result,err)
			}
		}
	}
}
func TestSystemStatusRejectsUnavailableServerAndUnrelatedOutput(t *testing.T){
	for _,args:=range [][]string{
		{"system","status","-o","json"},
		{"system","status","-o","invalid"},
		{"doctor","-o","json"},
	}{
		var out,stderr bytes.Buffer
		code:=Run(context.Background(),args,&out,&stderr,nil,nil)
		if code==0||out.Len()!=0{t.Fatalf("args=%v code=%d out=%q err=%q",args,code,out.String(),stderr.String())}
	}
}
