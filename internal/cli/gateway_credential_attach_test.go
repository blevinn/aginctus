package cli

import (
 "bytes"
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/blevinn/aginctus/internal/config"
 "github.com/blevinn/aginctus/internal/gateway"
)

func TestGatewayCredentialAttachmentCommand(t *testing.T){
 cfg,err:=(&config.Loader{}).Load(config.Options{})
 if err!=nil{t.Fatal(err)}
 original:=attachGatewayCredentials
 t.Cleanup(func(){attachGatewayCredentials=original})
 called:=0
 attachGatewayCredentials=func(_ context.Context,a gateway.CredentialAttachment)error{
  called++
  if a.GatewayID!="local"||a.ComposeProject!="aginctus-gateway"||a.Pool!="default"{
   t.Fatalf("unexpected attachment: %#v",a)
  }
  return nil
 }
 var out,errs bytes.Buffer
 status:=runGateway(context.Background(),[]string{"credential-storage","attach"},&out,&errs,nil,cfg)
 if status!=0||called!=1||!strings.Contains(out.String(),"OAuth not activated"){
  t.Fatalf("status=%d called=%d out=%q err=%q",status,called,out.String(),errs.String())
 }
 attachGatewayCredentials=func(context.Context,gateway.CredentialAttachment)error{return errors.New("foreign volume")}
 out.Reset();errs.Reset()
 status=runGateway(context.Background(),[]string{"credential-storage","attach"},&out,&errs,nil,cfg)
 if status!=1||!strings.Contains(errs.String(),"foreign volume"){
  t.Fatalf("status=%d error=%q",status,errs.String())
 }
}
