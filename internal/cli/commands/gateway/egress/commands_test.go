package egresscmd

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func TestGatewayEgressRegistrationRequiresMutationConfirmation(t *testing.T) {
	for _,args:=range [][]string{
		{"gateway","egress","reconcile"},
		{"gateway","egress","reconcile","--dry-run","--confirm-stopped-instance"},
		{"gateway","egress","dhcp","apply","--ranges","10.0.0.2-10.0.0.3"},
		{"gateway","egress","dhcp","apply","--confirm-bridge-only"},
		{"gateway","egress","firewall","plan"},
		{"gateway","egress","dhcp","plan"},
	} {
		t.Run(args[2],func(t *testing.T){
			root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
			gateway:=&cobra.Command{Use:"gateway"}
			root.AddCommand(gateway)
			var output bytes.Buffer
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(args)
			loads,status:=0,0
			Register(gateway,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{})
			if err:=root.Execute();err==nil{t.Fatalf("accepted mutation without valid confirmation: %q",args)}
			if loads!=0{t.Fatalf("invalid command loaded configuration: %q",args)}
		})
	}
}

func TestGatewayEgressRegistrationDispatchesExplicitModes(t *testing.T) {
	for _,tc:=range []struct{args []string;operation,zone,ranges string}{
		{[]string{"gateway","egress","reconcile","--dry-run"},"reconcile-dry","",""},
		{[]string{"gateway","egress","reconcile","--confirm-stopped-instance"},"reconcile-write","",""},
		{[]string{"gateway","egress","firewall","audit","--uplink-zone","public"},"firewall-audit","public",""},
		{[]string{"gateway","egress","dhcp","plan","--ranges","10.0.0.2-10.0.0.3"},"dhcp-plan","","10.0.0.2-10.0.0.3"},
		{[]string{"gateway","egress","dhcp","apply","--ranges","10.0.0.2-10.0.0.3","--confirm-bridge-only"},"dhcp-apply","","10.0.0.2-10.0.0.3"},
	} {
		t.Run(tc.operation,func(t *testing.T){
			root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
			gateway:=&cobra.Command{Use:"gateway"};root.AddCommand(gateway)
			var output bytes.Buffer
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(tc.args)
			loads,calls,status:=0,0,0
			Register(gateway,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{
				Egress:func(_ context.Context,op,zone,ranges string,_,_ io.Writer,_ *config.Config)int{
					calls++
					if op!=tc.operation||zone!=tc.zone||ranges!=tc.ranges{
						t.Fatalf("op=%q zone=%q ranges=%q",op,zone,ranges)
					}
					return 0
				},
			})
			if err:=root.Execute();err!=nil{t.Fatal(err)}
			if loads!=1||calls!=1{t.Fatalf("loads=%d calls=%d",loads,calls)}
		})
	}
}

func TestGatewayEgressRetiredStageCannotMutate(t *testing.T) {
	root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
	gateway:=&cobra.Command{Use:"gateway"};root.AddCommand(gateway)
	var output bytes.Buffer
	root.SetOut(&output);root.SetErr(&output);root.SetArgs([]string{"gateway","egress","firewall","stage","anything"})
	loads,status:=0,0
	Register(gateway,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{})
	if err:=root.Execute();err!=nil{t.Fatal(err)}
	if loads!=0||status!=2{t.Fatalf("retired stage: loads=%d status=%d",loads,status)}
}

func TestGatewayEgressHelpNoLoad(t *testing.T) {
	root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
	gateway:=&cobra.Command{Use:"gateway"};root.AddCommand(gateway)
	var output bytes.Buffer
	root.SetOut(&output);root.SetErr(&output);root.SetArgs([]string{"gateway","egress","dhcp","apply","--help"})
	loads,status:=0,0
	Register(gateway,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{})
	if err:=root.Execute();err!=nil{t.Fatal(err)}
	if loads!=0{t.Fatal("help loaded config")}
}
