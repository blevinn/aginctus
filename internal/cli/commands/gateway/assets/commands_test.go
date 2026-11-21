package assetscmd

import (
	"bytes"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	"github.com/spf13/cobra"
)

func TestAssetRegistrationRejectsSelectorsBeforeStoreOpen(t *testing.T) {
	for _,args:=range [][]string{
		{"gateway","plugin","show"},
		{"gateway","provider","show"},
		{"gateway","provider","apply"},
		{"gateway","model","show"},
		{"gateway","mcp","show"},
		{"gateway","mcp","apply","local"},
		{"gateway","catalog","show"},
		{"gateway","catalog","apply","test"},
		{"gateway","catalog","apply","test","--config","{}","--config-file","/tmp/secret"},
	} {
		t.Run(args[1]+"/"+args[2],func(t *testing.T){
			var output bytes.Buffer
			root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(args)
			gateway:=&cobra.Command{Use:"gateway"};root.AddCommand(gateway)
			providers:=&cobra.Command{Use:"provider"};gateway.AddCommand(providers)
			models:=&cobra.Command{Use:"model"};gateway.AddCommand(models)
			status,loads,opens:=0,0,0
			Register(gateway,providers,models,&status,config.Options{},func()(*config.Config,error){
				loads++;return &config.Config{},nil
			},func(*config.Config)(*gatewaycontrol.Store,error){
				opens++;return nil,nil
			},gatewaycontrol.Plugins{},func(string,string,any)error{return nil})
			if err:=root.Execute();err==nil{t.Fatalf("accepted invalid syntax: %q",args)}
			if loads!=0||opens!=0{t.Fatalf("invalid syntax loaded=%d opened=%d: %q",loads,opens,args)}
		})
	}
}

func TestAssetRegistrationHelpCannotOpenStore(t *testing.T) {
	for _,args:=range [][]string{
		{"gateway","plugin","show","--help"},
		{"gateway","provider","apply","--help"},
		{"gateway","model","show","--help"},
		{"gateway","mcp","apply","--help"},
		{"gateway","catalog","apply","--help"},
	} {
		var output bytes.Buffer
		root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
		root.SetOut(&output);root.SetErr(&output);root.SetArgs(args)
		gateway:=&cobra.Command{Use:"gateway"};root.AddCommand(gateway)
		providers:=&cobra.Command{Use:"provider"};gateway.AddCommand(providers)
		models:=&cobra.Command{Use:"model"};gateway.AddCommand(models)
		status,loads,opens:=0,0,0
		Register(gateway,providers,models,&status,config.Options{},func()(*config.Config,error){
			loads++;return &config.Config{},nil
		},func(*config.Config)(*gatewaycontrol.Store,error){
			opens++;return nil,nil
		},gatewaycontrol.Plugins{},func(string,string,any)error{return nil})
		if err:=root.Execute();err!=nil{t.Fatal(err)}
		if loads!=0||opens!=0{t.Fatalf("help read host state: %q",args)}
	}
}

func TestAssetSelectionRejectsAmbiguity(t *testing.T) {
	for _,tc:=range []struct{flag string;args []string}{
		{"provider",[]string{"other"}},
		{"model",[]string{"another","extra"}},
	} {
		_,err:=selected(tc.flag,tc.args)
		if err==nil{t.Fatalf("accepted positional and flag selectors: %v",tc)}
	}
	if _,_,err:=modelSelection("provider","model",[]string{"extra"});err==nil{
		t.Fatal("accepted ambiguous provider/model selection")
	}
}
