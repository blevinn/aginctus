package herdrcmd

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/spf13/cobra"
)

func TestHerdrRegisterValidatesBeforeLoading(t *testing.T) {
	for _, args := range [][]string{
		{"herdr", "authorize"},
		{"herdr", "pin-host"},
		{"herdr", "register", "one", "two"},
		{"herdr", "client", "apply", "unexpected"},
		{"herdr", "client", "start", "unexpected"},
	} {
		t.Run(args[1], func(t *testing.T) {
			var output bytes.Buffer
			loads, calls := 0, 0
			root := &cobra.Command{Use: "aginctus", SilenceUsage: true, SilenceErrors: true}
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(args)
			status := 0
			noCall := func(context.Context, []string, io.Writer, io.Writer, *config.Config) int {
				calls++
				return 0
			}
			Register(root, &output, &output, func() (*config.Config, error) {
				loads++
				return nil, nil
			}, &status, Handlers{
				Console: noCall, Authorize: noCall, PinHost: noCall,
				RegisterWorkload: noCall, ClientStatus: noCall, ScopedClientStatus: noCall,
				Mutation: func(context.Context, string, bool, bool, io.Writer, io.Writer, *config.Config) int {
					calls++
					return 0
				},
				Transition: func(context.Context, string, io.Writer, io.Writer, *config.Config) int {
					calls++
					return 0
				},
			})
			if err := root.Execute(); err == nil {
				t.Fatal("accepted invalid syntax")
			}
			if loads != 0 || calls != 0 { t.Fatalf("invalid syntax loaded config %d times and called handlers %d times", loads, calls) }
		})
	}
}

func TestHerdrRegisterDispatchesScopedStatusAndAliases(t *testing.T) {
	for _, tc := range []struct {
		args []string
		operation string
		dryRun, force bool
		wantArgs []string
	}{
		{[]string{"herdr", "client", "ensure", "--dry-run"}, "upsert", true, false, nil},
		{[]string{"herdr", "client", "teardown", "--force"}, "delete", false, true, nil},
		{[]string{"herdr", "client", "status", "--project", "tenant"}, "scoped", false, false, []string{"--project", "tenant"}},
		{[]string{"herdr", "client", "status"}, "status", false, false, nil},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			var output bytes.Buffer
			root := &cobra.Command{Use: "aginctus", SilenceUsage: true, SilenceErrors: true}
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(tc.args)
			status, loads, called := 0, 0, 0
			Register(root, &output, &output, func() (*config.Config, error) {
				loads++
				return &config.Config{}, nil
			}, &status, Handlers{
				Mutation: func(_ context.Context, op string, dry, force bool, _, _ io.Writer, _ *config.Config) int {
					called++
					if op != tc.operation || dry != tc.dryRun || force != tc.force {
						t.Fatalf("mutation %q dry=%v force=%v", op, dry, force)
					}
					return 0
				},
				ClientStatus: func(_ context.Context, args []string, _, _ io.Writer, _ *config.Config) int {
					called++
					if tc.operation != "status" || len(args) != 0 { t.Fatal("unexpected ambient status") }
					return 0
				},
				ScopedClientStatus: func(_ context.Context, args []string, _, _ io.Writer, _ *config.Config) int {
					called++
					if tc.operation != "scoped" || !reflect.DeepEqual(args, tc.wantArgs) { t.Fatalf("scoped args %q", args) }
					return 0
				},
			})
			if err := root.Execute(); err != nil { t.Fatal(err) }
			if loads != 1 || called != 1 { t.Fatalf("loads=%d handler calls=%d", loads, called) }
		})
	}
}

func TestHerdrRegisterHelpHasNoSideEffects(t *testing.T) {
	var output bytes.Buffer
	root := &cobra.Command{Use:"aginctus", SilenceUsage:true, SilenceErrors:true}
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"herdr", "client", "--help"})
	status, loads := 0, 0
	Register(root, &output, &output, func() (*config.Config, error) {
		loads++
		return nil, nil
	}, &status, Handlers{})
	if err := root.Execute(); err != nil { t.Fatal(err) }
	if loads != 0 { t.Fatal("help loaded configuration") }
}
