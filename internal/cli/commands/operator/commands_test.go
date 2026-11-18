package operatorcmd

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func TestOperatorRegisterRejectsBeforeCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		options config.Options
	}{
		{"missing preflight project", []string{"operator", "preflight"}, config.Options{}},
		{"missing plan project", []string{"operator", "project-plan"}, config.Options{}},
		{"missing confirmation", []string{"operator", "project-ensure", "--project", "tenant"}, config.Options{}},
		{"unexpected args", []string{"operator", "units-audit", "extra"}, config.Options{}},
		{"offline config override", []string{"operator", "project-plan", "--project", "tenant"}, config.Options{ConfigurationFile: "/etc/untrusted"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(tc.args)
			status, loads, handlers := 0, 0, 0
			unit := func(context.Context, []string, io.Writer, io.Writer) int { handlers++; return 0 }
			Register(root, &output, &output, func() (*config.Config,error) {
				loads++
				return &config.Config{},nil
			}, &status, &tc.options, Handlers{
				UnitsAudit: unit,
				Preflight: unit,
				ProjectPlan: unit,
				ProjectEnsure: unit,
			})
			if err := root.Execute(); err == nil { t.Fatal("accepted invalid operator input") }
			if loads != 0 || handlers != 0 { t.Fatalf("loads=%d handlers=%d",loads,handlers) }
		})
	}
}

func TestOperatorRegisterDispatchesExplicitProject(t *testing.T) {
	for _, tc := range []struct {
		name string
		args, expected []string
		loads int
	}{
		{"plan", []string{"operator", "project-plan", "--project", "tenant"}, []string{"--project", "tenant"}, 0},
		{"ensure", []string{"operator", "project-ensure", "--project", "tenant", "--confirm-create"}, []string{"--project", "tenant", "--confirm-create"}, 0},
		{"preflight", []string{"operator", "preflight", "--project", "tenant"}, []string{"preflight", "--project", "tenant"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceErrors: true, SilenceUsage: true}
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(tc.args)
			status, loads, calls := 0, 0, 0
			options := config.Options{}
			record := func(_ context.Context, args []string, _, _ io.Writer) int {
				calls++
				if !reflect.DeepEqual(args, tc.expected) { t.Fatalf("args = %q; want %q",args,tc.expected) }
				return 0
			}
			Register(root,&output,&output,func()(*config.Config,error){
				loads++
				return &config.Config{},nil
			},&status,&options,Handlers{
				ProjectPlan:record,
				ProjectEnsure:record,
				Preflight:record,
			})
			if err := root.Execute(); err != nil { t.Fatal(err) }
			if loads != tc.loads || calls != 1 { t.Fatalf("loads=%d calls=%d",loads,calls) }
		})
	}
}

func TestOperatorRegisterHelpDoesNotLoadOrMutate(t *testing.T) {
	var output bytes.Buffer
	root := &cobra.Command{Use:"aginctus",SilenceErrors:true,SilenceUsage:true}
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"operator","project-ensure","--help"})
	options := config.Options{}
	loads, calls, status := 0, 0, 0
	Register(root,&output,&output,func()(*config.Config,error){
		loads++
		return &config.Config{},nil
	},&status,&options,Handlers{
		ProjectEnsure:func(context.Context,[]string,io.Writer,io.Writer)int{calls++;return 0},
	})
	if err := root.Execute(); err != nil { t.Fatal(err) }
	if loads != 0 || calls != 0 { t.Fatalf("help triggered loads=%d calls=%d",loads,calls) }
}
