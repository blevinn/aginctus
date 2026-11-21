package workloadcmd

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func TestWorkloadRegisterDeniedSyntaxNeverLoads(t *testing.T) {
	for _, args := range [][]string{
		{"workload", "apply", "workload", "--project", "tenant"},
		{"workload", "apply", "workload", "--project", "tenant", "--dry-run", "--confirm-apply"},
		{"workload", "apply", "workload", "--render"},
		{"workload", "start", "workload", "--project", "tenant"},
		{"workload", "stop", "workload", "--project", "tenant"},
		{"workload", "gateway", "provision", "workload"},
		{"workload", "gateway", "provision", "workload", "--model", "m"},
	} {
		t.Run(args[1], func(t *testing.T) {
			root := &cobra.Command{Use:"aginctus",SilenceUsage:true,SilenceErrors:true}
			var output bytes.Buffer
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(args)
			loads,status:=0,0
			Register(root,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{})
			if err:=root.Execute();err==nil{t.Fatalf("accepted invalid arguments: %q",args)}
			if loads!=0{t.Fatalf("loaded config on invalid args %q",args)}
		})
	}
}

func TestWorkloadRegisterRoutesScopedApplyModes(t *testing.T) {
	for _,tc:=range []struct{mode string;args,want []string;confirmed bool}{
		{"render",[]string{"workload","apply","demo","--project","tenant","--render"},[]string{"--project","tenant","demo","--render"},false},
		{"dry-run",[]string{"workload","apply","demo","--project","tenant","--dry-run"},[]string{"--project","tenant","demo","--dry-run"},false},
		{"confirm",[]string{"workload","apply","demo","--project","tenant","--confirm-apply"},[]string{"--project","tenant","demo","--confirm-apply"},true},
	} {
		t.Run(tc.mode,func(t *testing.T){
			root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
			var output bytes.Buffer
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(tc.args)
			calls,loads,status:=0,0,0
			handler:=func(_ context.Context,args []string,_,_ io.Writer,_ *config.Config)int{
				calls++
				if !reflect.DeepEqual(args,tc.want){t.Fatalf("args=%q want=%q",args,tc.want)}
				return 0
			}
			Register(root,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{
				ScopedApply:func(ctx context.Context,args []string,out,err io.Writer,cfg *config.Config)int{
					if tc.confirmed{t.Fatal("preview handler on confirmed mutation")}
					return handler(ctx,args,out,err,cfg)
				},
				ScopedApplyConfirm:func(ctx context.Context,args []string,out,err io.Writer,cfg *config.Config)int{
					if !tc.confirmed{t.Fatal("confirmed mutation handler on preview")}
					return handler(ctx,args,out,err,cfg)
				},
			})
			if err:=root.Execute();err!=nil{t.Fatal(err)}
			if calls!=1||loads!=1{t.Fatalf("calls=%d loads=%d",calls,loads)}
		})
	}
}

func TestWorkloadRegisterHelpNeverLoads(t *testing.T) {
	root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
	var output bytes.Buffer
	root.SetOut(&output);root.SetErr(&output)
	root.SetArgs([]string{"workload","gateway","provision","--help"})
	loads,status:=0,0
	Register(root,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{})
	if err:=root.Execute();err!=nil{t.Fatal(err)}
	if loads!=0{t.Fatal("help loaded configuration")}
}
