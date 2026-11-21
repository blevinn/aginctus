package registrycmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func TestRegistryRegistrationRejectsInvalidSyntaxBeforeLoad(t *testing.T) {
	for _,args:=range [][]string{
		{"gateway","provider","register"},
		{"gateway","model","register"},
		{"gateway","model","render"},
		{"gateway","provider","register","one","two"},
		{"gateway","model","register","one","two"},
	} {
		t.Run(strings.Join(args,"/"),func(t *testing.T){
			var output bytes.Buffer
			root:=&cobra.Command{Use:"aginctus",SilenceUsage:true,SilenceErrors:true}
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(args)
			gateway:=&cobra.Command{Use:"gateway"};root.AddCommand(gateway)
			status,loads,assets:=0,0,0
			Register(gateway,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,config.Options{},Handlers{
				AddAssets:func(*cobra.Command,*cobra.Command,*cobra.Command,*int,config.Options,func()(*config.Config,error)){assets++},
			})
			if err:=root.Execute();err==nil{t.Fatalf("accepted %q",args)}
			if loads!=0||assets!=1{t.Fatalf("loads=%d assets=%d",loads,assets)}
		})
	}
}

func TestRegistryRegistrationDryRunSkipsMutationAuthorization(t *testing.T) {
	for _,tc:=range []struct{args []string;kind,name string}{
		{[]string{"gateway","provider","register","openai","--dry-run"},"provider","openai"},
		{[]string{"gateway","model","register","gpt","--dry-run"},"model","gpt"},
	} {
		t.Run(tc.kind,func(t *testing.T){
			var output bytes.Buffer
			root:=&cobra.Command{Use:"aginctus",SilenceUsage:true,SilenceErrors:true}
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(tc.args)
			gateway:=&cobra.Command{Use:"gateway"};root.AddCommand(gateway)
			status,loads,checks,calls:=0,0,0,0
			Register(gateway,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,config.Options{},Handlers{
				CheckMutation:func(*config.Config,string)error{checks++;return nil},
				Register:func(kind,name,provider,source,alias string,dry bool,_,_ io.Writer)int{
					calls++
					if !dry||kind!=tc.kind||name!=tc.name{t.Fatalf("dispatch kind=%q name=%q dry=%v",kind,name,dry)}
					return 0
				},
				AddAssets:func(*cobra.Command,*cobra.Command,*cobra.Command,*int,config.Options,func()(*config.Config,error)){ },
			})
			if err:=root.Execute();err!=nil{t.Fatal(err)}
			if checks!=0||calls!=1||loads!=1{t.Fatalf("checks=%d calls=%d loads=%d",checks,calls,loads)}
		})
	}
}

func TestRegistryRegistrationHelpDoesNotLoad(t *testing.T) {
	var output bytes.Buffer
	root:=&cobra.Command{Use:"aginctus",SilenceUsage:true,SilenceErrors:true}
	root.SetOut(&output);root.SetErr(&output);root.SetArgs([]string{"gateway","model","register","--help"})
	gateway:=&cobra.Command{Use:"gateway"};root.AddCommand(gateway)
	status,loads:=0,0
	Register(gateway,&output,&output,func()(*config.Config,error){loads++;return &config.Config{},nil},&status,config.Options{},Handlers{
		AddAssets:func(*cobra.Command,*cobra.Command,*cobra.Command,*int,config.Options,func()(*config.Config,error)){ },
	})
	if err:=root.Execute();err!=nil{t.Fatal(err)}
	if loads!=0{t.Fatal("help triggered configuration load")}
}
