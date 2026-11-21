package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicEnvironmentDocumentAndEmptyCollection(t *testing.T) {
	one := EnvironmentDetail{TypeMeta:NewTypeMeta("Environment"),Environment:Environment{
		ID:"11111111-1111-4111-8111-111111111111",Name:"dev",Binding:EnvironmentBinding{ServerID:"srv",Project:"aginctus"},
	}}
	var _ Document = one
	encoded,err:=json.Marshal(one)
	if err!=nil {t.Fatal(err)}
	if !strings.Contains(string(encoded),`"id":"11111111-1111-4111-8111-111111111111"`) ||
		!strings.Contains(string(encoded),`"binding":{"serverId":"srv","project":"aginctus"}`) {
		t.Fatalf("unexpected public projection: %s",encoded)
	}
	list:=EnvironmentList{TypeMeta:NewTypeMeta("EnvironmentList"),Items:[]Environment{}}
	var _ Document = list
	encoded,err=json.Marshal(list)
	if err!=nil {t.Fatal(err)}
	if string(encoded)!=`{"apiVersion":"aginctus.dev/v1alpha1","kind":"EnvironmentList","items":[]}` {t.Fatal(string(encoded))}
}
