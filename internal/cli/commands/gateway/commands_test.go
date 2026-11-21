package gatewaycmd

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func TestGatewayRegisterRejectsUnexpectedArgumentsBeforeLoading(t *testing.T) {
	for _, args := range [][]string{
		{"gateway", "apply", "unexpected"},
		{"gateway", "status", "unexpected"},
		{"gateway", "credential-storage", "attach", "unexpected"},
		{"gateway", "remove", "unexpected"},
	} {
		root := &cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
		var output bytes.Buffer
		root.SetOut(&output);root.SetErr(&output);root.SetArgs(args)
		loads,status:=0,0
		Register(root,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{})
		if err:=root.Execute();err==nil{t.Fatalf("accepted %q",args)}
		if loads!=0{t.Fatalf("loaded configuration for %q",args)}
	}
}

func TestGatewayRegisterRoutesLifecycleAndCredentialLeaves(t *testing.T) {
	for _,tc:=range []struct{
		args []string
		operation string
		force bool
	}{
		{[]string{"gateway","up"},"apply",false},
		{[]string{"gateway","remove","--force"},"remove",true},
		{[]string{"gateway","validate"},"validate",false},
		{[]string{"gateway","render"},"render",false},
		{[]string{"gateway","credential-storage","ensure"},"ensure",false},
		{[]string{"gateway","credential-storage","attach"},"attach",false},
		{[]string{"gateway","credential-storage","audit"},"audit",false},
	} {
		t.Run(tc.operation,func(t *testing.T){
			root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
			var output bytes.Buffer
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(tc.args)
			loads,calls,status:=0,0,0
			Register(root,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{
				Lifecycle:func(_ context.Context,op string,force bool,_,_ io.Writer,_ *config.Config)int{
					calls++
					if op!=tc.operation||force!=tc.force{t.Fatalf("lifecycle op=%q force=%v",op,force)}
					return 0
				},
				Credential:func(_ context.Context,op string,_,_ io.Writer,_ *config.Config)int{
					calls++
					if op!=tc.operation{t.Fatalf("credential op=%q",op)}
					return 0
				},
			})
			if err:=root.Execute();err!=nil{t.Fatal(err)}
			if loads!=1||calls!=1{t.Fatalf("loads=%d calls=%d",loads,calls)}
		})
	}
}

func TestGatewayRegisterHelpAndParentAreSideEffectFree(t *testing.T) {
	for _,args:=range [][]string{
		{"gateway","credential-storage","--help"},
		{"gateway","remove","--help"},
	} {
		root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
		var output bytes.Buffer
		root.SetOut(&output);root.SetErr(&output);root.SetArgs(args)
		loads,status:=0,0
		Register(root,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,Handlers{})
		if err:=root.Execute();err!=nil{t.Fatal(err)}
		if loads!=0{t.Fatalf("help loaded config for %q",args)}
	}
}
