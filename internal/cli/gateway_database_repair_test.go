package cli

import (
 "bytes"
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/blevinn/aginctus/internal/config"
)

func TestGatewayDatabaseHostnameRepairCLI(t *testing.T) {
 cfg,err:=(&config.Loader{}).Load(config.Options{})
 if err!=nil{t.Fatal(err)}
 saved:=repairLiteLLMDatabaseHost
 t.Cleanup(func(){repairLiteLLMDatabaseHost=saved})
 callCount:=0
 repairLiteLLMDatabaseHost=func(_ context.Context,project string)(bool,error){
  callCount++
  if project!="aginctus-gateway"{t.Fatalf("wrong project %q",project)}
  return true,nil
 }
 var out,errs bytes.Buffer
 rc:=runGateway(context.Background(),[]string{"database","repair-hostname"},&out,&errs,nil,cfg)
 if rc!=0||callCount!=1||!strings.Contains(out.String(),"remains stopped"){t.Fatalf("rc=%d out=%q err=%q",rc,out.String(),errs.String())}
 repairLiteLLMDatabaseHost=func(context.Context,string)(bool,error){return false,errors.New("ownership mismatch")}
 out.Reset();errs.Reset()
 rc=runGateway(context.Background(),[]string{"database","repair-hostname"},&out,&errs,nil,cfg)
 if rc!=1||!strings.Contains(errs.String(),"ownership mismatch")||out.Len()!=0{t.Fatalf("rc=%d out=%q err=%q",rc,out.String(),errs.String())}
}
