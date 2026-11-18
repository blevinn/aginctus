package testenvcmd

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func TestTestenvRegistrationRejectsInvalidInputWithoutExecution(t *testing.T) {
	for _,tc:=range []struct{
		name string
		args []string
		opts config.Options
	}{
		{"missing test scenario",[]string{"testenv","vm","test"},config.Options{}},
		{"extra plan arg",[]string{"testenv","vm","plan","one","two"},config.Options{}},
		{"invalid group",[]string{"testenv","vm","unknown"},config.Options{}},
		{"host config override",[]string{"testenv","vm","policy"},config.Options{ConfigurationFile:"/tmp/host-config"}},
	} {
		t.Run(tc.name,func(t *testing.T){
			var output bytes.Buffer
			root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(tc.args)
			status,calls:=0,0
			Register(root,&output,&output,&status,&tc.opts,func(context.Context,[]string,io.Writer,io.Writer)int{
				calls++;return 0
			})
			if err:=root.Execute();err==nil{t.Fatal("invalid syntax accepted")}
			if calls!=0{t.Fatal("invalid command invoked test environment")}
		})
	}
}

func TestTestenvRegistrationRoutesOnlyExplicitLeaves(t *testing.T) {
	for _,tc:=range []struct{args,want []string}{
		{[]string{"testenv","mcp"},[]string{"mcp"}},
		{[]string{"testenv","vm","policy"},[]string{"vm","policy"}},
		{[]string{"testenv","vm","backend","nixos"},[]string{"vm","backend","nixos"}},
		{[]string{"testenv","vm","plan","smoke"},[]string{"vm","plan","smoke"}},
		{[]string{"testenv","vm","test","smoke"},[]string{"vm","test","smoke"}},
	} {
		t.Run(tc.args[1],func(t *testing.T){
			var output bytes.Buffer
			root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
			root.SetOut(&output);root.SetErr(&output);root.SetArgs(tc.args)
			status,calls:=0,0
			opts:=config.Options{}
			Register(root,&output,&output,&status,&opts,func(_ context.Context,args []string,_,_ io.Writer)int{
				calls++
				if !reflect.DeepEqual(args,tc.want){t.Fatalf("got %q, want %q",args,tc.want)}
				return 0
			})
			if err:=root.Execute();err!=nil{t.Fatal(err)}
			if calls!=1{t.Fatalf("calls=%d",calls)}
		})
	}
}

func TestTestenvHelpNeverStartsWorker(t *testing.T) {
	var output bytes.Buffer
	root:=&cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
	root.SetOut(&output);root.SetErr(&output);root.SetArgs([]string{"testenv","vm","test","--help"})
	opts:=config.Options{}
	status,calls:=0,0
	Register(root,&output,&output,&status,&opts,func(context.Context,[]string,io.Writer,io.Writer)int{calls++;return 0})
	if err:=root.Execute();err!=nil{t.Fatal(err)}
	if calls!=0{t.Fatal("help invoked VM backend")}
}
