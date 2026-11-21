package client

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/plugin"
)

func TestPluginsGetTypedAndCopySafe(t *testing.T) {
	source := fakeInventory{{Name:"example", Version:"1", APIVersion:plugin.APIVersion,
		Capabilities:[]plugin.Capability{{Name:"assistant", Kind:plugin.Agent}},
		Dependencies:[]string{"other"},
	}}
	c := New(Options{PluginInventory:source})
	detail, err:=c.Plugins.Get(context.Background(),GetPluginRequest{Name:"example"})
	if err!=nil {t.Fatal(err)}
	if detail.Kind!="Plugin" || detail.Name!="example" || len(detail.Capabilities)!=1 {t.Fatalf("unexpected document: %#v",detail)}
	encoded,err:=json.Marshal(detail)
	if err!=nil {t.Fatal(err)}
	var document map[string]any
	if err:=json.Unmarshal(encoded,&document);err!=nil {t.Fatal(err)}
	if _,ok:=document["name"];!ok {t.Fatalf("name missing from root: %s",encoded)}
	if _,ok:=document["data"];ok {t.Fatalf("generic wrapper present: %s",encoded)}
	detail.Dependencies[0]="changed"
	detail.Capabilities[0].Name="changed"
	if source[0].Dependencies[0]!="other" || source[0].Capabilities[0].Name!="assistant" {t.Fatal("public projection aliases registry")}
}

func TestPluginsGetMissingAndCancellation(t *testing.T) {
	c:=New(Options{})
	for _,tc:=range []struct{name string; code api.ErrorCode}{
		{"",api.CodeInvalidArgument},
		{"no-such-plugin",api.CodeNotFound},
	} {
		_,err:=c.Plugins.Get(context.Background(),GetPluginRequest{Name:tc.name})
		var typed *api.Error
		if !errors.As(err,&typed) || typed.Code!=tc.code {t.Fatalf("name=%q error=%v",tc.name,err)}
	}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	_,err:=c.Plugins.Get(ctx,GetPluginRequest{Name:"example"})
	if !errors.Is(err,context.Canceled) {t.Fatalf("cancelled request: %v",err)}
}
