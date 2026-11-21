package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

func TestWriteErrorPreservesOnlySafeTypedFailures(t *testing.T) {
	for _,tc:=range []struct{name string;format Format;cause error;want string;leak string}{
		{"typed-json",JSON,api.NewError(api.CodeNotFound,"workload not found"),`"code": "not_found"`,""},
		{"typed-yaml",YAML,api.NewError(api.CodePermissionDenied,"access denied"),"code: permission_denied",""},
		{"wrapped",JSON,errors.Join(errors.New("outer"),api.NewError(api.CodeConflict,"revision changed")),`"code": "conflict"`,"outer"},
		{"unknown",JSON,errors.New("secret-storage-path"),`"code": "internal"`,"secret-storage-path"},
		{"unknown-code",JSON,api.NewError("private_backend_code","private message"),`"message": "operation failed"`,"private message"},
	}{
		t.Run(tc.name,func(t *testing.T){
			var out bytes.Buffer
			if err:=WriteError(&out,tc.format,tc.cause);err!=nil{t.Fatal(err)}
			if !strings.Contains(out.String(),tc.want)||tc.leak!=""&&strings.Contains(out.String(),tc.leak){
				t.Fatalf("unsafe output: %q",out.String())
			}
			if !strings.Contains(out.String(),"apiVersion")||!strings.Contains(out.String(),"Error"){t.Fatalf("missing envelope: %q",out.String())}
		})
	}
}
func TestWriteErrorRejectsInertAndUnsupportedCalls(t *testing.T) {
	var out bytes.Buffer
	for _,tc:=range []struct{format Format;cause error}{
		{JSON,nil},{Table,errors.New("failed")},{Name,errors.New("failed")},
	}{
		if err:=WriteError(&out,tc.format,tc.cause);err==nil{t.Fatalf("format=%q cause=%v accepted",tc.format,tc.cause)}
	}
	if out.Len()!=0{t.Fatalf("unexpected writes: %q",out.String())}
}
