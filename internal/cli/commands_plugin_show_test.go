package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/plugins"
	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/plugin"
)

type showFixturePlugin struct{}

func (showFixturePlugin) Descriptor() plugin.Descriptor {
	return plugin.Descriptor{
		APIVersion: plugin.APIVersion,
		Name:"example-agent", Version:"1.0",
		Capabilities:[]plugin.Capability{{Name:"assistant",Kind:plugin.Agent}},
	}
}
func (showFixturePlugin) ValidateConfiguration(context.Context, json.RawMessage) error {
	panic("read-only plugin detail invoked runtime validation")
}

func TestPluginShowClientBackedOutput(t *testing.T) {
	registry, err := plugins.New(context.Background(), showFixturePlugin{})
	if err != nil {t.Fatal(err)}
	for _,tc:=range []struct{args []string;contains string}{
		{[]string{"plugins","show","example-agent"}, "NAME"},
		{[]string{"plugins","show","example-agent","-o","wide"}, "CAPABILITY"},
		{[]string{"plugins","show","example-agent","-o","name"}, "example-agent\n"},
		{[]string{"--output=yaml","plugins","show","example-agent"}, "kind: Plugin"},
	} {
		var out,errs bytes.Buffer
		code:=RunWithPlugins(context.Background(),tc.args,&out,&errs,nil,nil,registry)
		if code!=0 || errs.Len()!=0 || !strings.Contains(out.String(),tc.contains) {
			t.Fatalf("args=%v code=%d out=%q err=%q",tc.args,code,out.String(),errs.String())
		}
	}
	var out,errs bytes.Buffer
	code:=RunWithPlugins(context.Background(),[]string{"plugins","show","example-agent","-o","json"},&out,&errs,nil,nil,registry)
	if code!=0 || errs.Len()!=0 {t.Fatalf("code=%d err=%q",code,errs.String())}
	var detail api.PluginDetail
	if err:=json.Unmarshal(out.Bytes(),&detail);err!=nil {t.Fatal(err)}
	if detail.Kind!="Plugin" || detail.Name!="example-agent" || len(detail.Capabilities)!=1 {t.Fatalf("wrong document: %#v",detail)}
}

func TestPluginShowMissingAndUnsupportedOutput(t *testing.T) {
	for _,args:=range [][]string{
		{"plugins","show","missing","-o","json"},
		{"plugins","show"},
		{"plugins","show","missing","-o","bad"},
	} {
		var out,errs bytes.Buffer
		code:=Run(context.Background(),args,&out,&errs,nil,nil)
		if code==0 || out.Len()!=0 || errs.Len()==0 {t.Fatalf("args=%v code=%d out=%q err=%q",args,code,out.String(),errs.String())}
	}
}
