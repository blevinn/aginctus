package cli

import (
 "bytes"
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/blevinn/aginctus/internal/config"
)

func TestDoctorGatewayDiagnosisAndFix(t *testing.T) {
 cfg,err:=(&config.Loader{}).Load(config.Options{})
 if err!=nil{t.Fatal(err)}
 savedRepair,savedInspect:=repairLiteLLMDatabaseHost,inspectLiteLLMDatabaseHost
 t.Cleanup(func(){repairLiteLLMDatabaseHost=savedRepair;inspectLiteLLMDatabaseHost=savedInspect})
 changes:=0
 inspectLiteLLMDatabaseHost=func(_ context.Context,project string)(bool,error){
  if project!="aginctus-gateway"{t.Fatalf("wrong project %q",project)}
  return true,nil
 }
 repairLiteLLMDatabaseHost=func(_ context.Context,project string)(bool,error){changes++;return true,nil}
 var out,errs bytes.Buffer
 rc:=runDoctorGateway(context.Background(),nil,&out,&errs,cfg)
 if rc!=1||changes!=0||!strings.Contains(errs.String(),"--fix"){t.Fatalf("read-only rc=%d err=%q changes=%d",rc,errs.String(),changes)}
 out.Reset();errs.Reset()
 rc=runDoctorGateway(context.Background(),[]string{"--fix"},&out,&errs,cfg)
 if rc!=0||changes!=1||!strings.Contains(out.String(),"remains stopped"){t.Fatalf("rc=%d out=%q err=%q changes=%d",rc,out.String(),errs.String(),changes)}
 inspectLiteLLMDatabaseHost=func(context.Context,string)(bool,error){return false,nil}
 out.Reset();errs.Reset()
 rc=runDoctorGateway(context.Background(),nil,&out,&errs,cfg)
 if rc!=0||!strings.Contains(out.String(),"correct"){t.Fatalf("healthy rc=%d out=%q err=%q",rc,out.String(),errs.String())}
 inspectLiteLLMDatabaseHost=func(context.Context,string)(bool,error){return false,errors.New("ownership mismatch")}
 out.Reset();errs.Reset()
 rc=runDoctorGateway(context.Background(),[]string{"--fix"},&out,&errs,cfg)
 if rc!=1||!strings.Contains(errs.String(),"ownership mismatch"){t.Fatalf("failure rc=%d err=%q",rc,errs.String())}
}
