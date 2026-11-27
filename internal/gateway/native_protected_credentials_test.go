package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
)
func TestProtectedCredentialFileBindsExactGrantAndVersion(t *testing.T){
	g:=environment.ProtectedGatewayGrant{Project:"logical",ProtectedProject:"protected",RevisionID:strings.Repeat("a",64)}
	record:=protectedCredentialFile{"aginctus.dev/protected-gateway-credentials/v1alpha1",g,"v1",strings.Repeat("p",32),strings.Repeat("m",32),strings.Repeat("s",32)}
	raw,err:=json.Marshal(record);if err!=nil{t.Fatal(err)}
	if _,err:=parseProtectedCredentialFile(raw,g,"v1");err!=nil{t.Fatal("valid bound source rejected")}
	foreign:=g;foreign.ProtectedProject="foreign"
	if _,err:=parseProtectedCredentialFile(raw,foreign,"v1");err==nil{t.Fatal("foreign source accepted")}
	if _,err:=parseProtectedCredentialFile(raw,g,"v2");err==nil{t.Fatal("stale version accepted")}
	duplicate:=append([]byte(`{"version":"v1",`),raw[1:]...)
	if _,err:=parseProtectedCredentialFile(duplicate,g,"v1");err==nil{t.Fatal("duplicate key accepted")}
}
