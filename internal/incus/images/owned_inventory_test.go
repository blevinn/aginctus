package images

import (
 "context"
 "errors"
 "strings"
 "testing"
 incusscope "github.com/blevinn/aginctus/internal/incus"
)
func TestOwnedInventoryFailClosed(t *testing.T) {
 scope,err:=incusscope.NewProjectScope("tenant");if err!=nil {t.Fatal(err)}
 for _,raw:=range []string{"null","{}","[",`[{"fingerprint":"short"}]`} {
  _,err=InspectOwnedScopedInventory(context.Background(),scope,func(context.Context,incusscope.ProjectScope)([]byte,error){return []byte(raw),nil},1024)
  if err==nil {t.Fatalf("accepted %q",raw)}
 }
 _,err=InspectOwnedScopedInventory(context.Background(),scope,nil,1024);if err==nil {t.Fatal("nil reader")}
 _,err=InspectOwnedScopedInventory(context.Background(),scope,func(context.Context,incusscope.ProjectScope)([]byte,error){return nil,errors.New("private error")},1024)
 if err==nil || strings.Contains(err.Error(),"private error"){t.Fatalf("unsafe error: %v",err)}
}
func TestOwnedInventoryRefusesAdoption(t *testing.T) {
 scope,err:=incusscope.NewProjectScope("tenant");if err!=nil{t.Fatal(err)}
 fp:=strings.Repeat("a",64)
 raw:=`[{"fingerprint":"`+fp+`","aliases":[{"name":"existing"}]}]`
 images,err:=InspectOwnedScopedInventory(context.Background(),scope,func(_ context.Context,got incusscope.ProjectScope)([]byte,error){
  if got.Name!="tenant"{t.Fatal("wrong scope")};return []byte(raw),nil
 },4096)
 if err!=nil{t.Fatal(err)}
 if AvailableForOwnedImport(images,"new",fp){t.Fatal("adopted fingerprint")}
 if AvailableForOwnedImport(images,"existing",strings.Repeat("b",64)){t.Fatal("replaced alias")}
 if !AvailableForOwnedImport(images,"new",strings.Repeat("b",64)){t.Fatal("rejected fresh image")}
}
