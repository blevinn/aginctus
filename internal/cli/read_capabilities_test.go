package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
	"github.com/blevinn/aginctus/pkg/client"
)

func TestUnifiedReadCapabilitiesShareOneInvocation(t *testing.T) {
	readers := ReadCapabilities{
		Environments: testEnrolledReader{},
		Workloads:    testLogicalWorkloadReader{},
		Configured:   stateCLIFixture{},
		Accepted:     stateCLIFixture{},
		Diff:         stateCLIFixture{},
	}
	for _, tc := range []struct {
		args     []string
		expected string
	}{
		{[]string{"environment", "list", "-o", "json"}, `"kind": "EnvironmentList"`},
		{[]string{"--environment", stateCLIEnv, "workloads", "list", "-o", "json"}, `"kind": "WorkloadList"`},
		{[]string{"--environment", stateCLIEnv, "state", "show", "-o", "json"}, `"kind": "AcceptedStateStatus"`},
		{[]string{"--environment", stateCLIEnv, "state", "diff", "-o", "json"}, `"kind": "ConfiguredAcceptedDiff"`},
		{[]string{"--environment", stateCLIEnv, "state", "plan-accept", "-o", "json"}, `"kind": "AcceptancePlan"`},
	} {
		args := append([]string{"--incus-project", "team"}, tc.args...)
		var out, errOut bytes.Buffer
		status := RunWithReadCapabilities(context.Background(), args, &out, &errOut, nil, nil, nil, readers)
		if status != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), tc.expected) {
			t.Fatalf("args=%v code=%d out=%q stderr=%q", args, status, out.String(), errOut.String())
		}
	}
}

func TestUnifiedReadCapabilitiesFailClosedWhenMissing(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		readers ReadCapabilities
	}{
		{[]string{"environment", "list"}, ReadCapabilities{Workloads: testLogicalWorkloadReader{}}},
		{[]string{"--environment", stateCLIEnv, "workloads", "list"}, ReadCapabilities{Environments: testEnrolledReader{}}},
		{[]string{"--environment", stateCLIEnv, "state", "show"}, ReadCapabilities{Environments: testEnrolledReader{}}},
		{[]string{"--environment", stateCLIEnv, "state", "plan-accept"}, ReadCapabilities{Accepted: stateCLIFixture{}}},
	} {
		args := append([]string{"--incus-project", "team"}, tc.args...)
		args = append(args, "-o", "json")
		var out, errOut bytes.Buffer
		status := RunWithReadCapabilities(context.Background(), args, &out, &errOut, nil, nil, nil, tc.readers)
		if status == 0 || out.Len() != 0 || errOut.Len() == 0 {
			t.Fatalf("partial capabilities bypassed checks: args=%v code=%d out=%q stderr=%q", args, status, out.String(), errOut.String())
		}
	}
}

type nameSelectionReader struct {
	list []api.Environment
	detail api.Environment
}

func (r nameSelectionReader) ListEnvironments(_ context.Context, _ string) (api.EnvironmentList,error) {
	return api.EnvironmentList{TypeMeta:api.NewTypeMeta("EnvironmentList"),Items:r.list},nil
}
func (r nameSelectionReader) GetEnvironment(_ context.Context, _, _ string) (api.EnvironmentDetail,error) {
	return api.EnvironmentDetail{TypeMeta:api.NewTypeMeta("Environment"),Environment:r.detail},nil
}

func TestUnifiedReadCapabilitiesResolveEnvironmentDisplayName(t *testing.T) {
	env:=api.Environment{ID:stateCLIEnv,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"server-1",Project:"team"}}
	readers:=ReadCapabilities{
		Environments:nameSelectionReader{list:[]api.Environment{env},detail:env},
		Workloads:testLogicalWorkloadReader{},
		Accepted:stateCLIFixture{},Diff:stateCLIFixture{},
	}
	for _,tc:=range []struct{args []string;kind string}{
		{[]string{"workloads","list","-o","json"},"WorkloadList"},
		{[]string{"state","show","-o","json"},"AcceptedStateStatus"},
		{[]string{"state","diff","-o","json"},"ConfiguredAcceptedDiff"},
		{[]string{"state","plan-accept","-o","json"},"AcceptancePlan"},
	} {
		args:=append([]string{"--incus-project","team","--environment","dev"},tc.args...)
		var out,errOut bytes.Buffer
		status:=RunWithReadCapabilities(context.Background(),args,&out,&errOut,nil,nil,nil,readers)
		if status!=0||errOut.Len()!=0||!strings.Contains(out.String(),`"kind": "`+tc.kind+`"`) {
			t.Fatalf("args=%v code=%d out=%q stderr=%q",args,status,out.String(),errOut.String())
		}
	}
}

func TestUnifiedReadCapabilitiesRejectAmbiguousAndStaleEnvironmentNames(t *testing.T) {
	env:=api.Environment{ID:stateCLIEnv,Name:"dev",Binding:api.EnvironmentBinding{ServerID:"server-1",Project:"team"}}
	other:=env
	other.ID="22222222-2222-4222-8222-222222222222"
	for _,tc:=range []struct{name string;reader client.EnvironmentReader}{
		{"missing reader",nil},
		{"ambiguous",nameSelectionReader{list:[]api.Environment{env,other},detail:env}},
		{"stale",nameSelectionReader{list:[]api.Environment{env},detail:other}},
		{"foreign",nameSelectionReader{list:[]api.Environment{env},detail:api.Environment{ID:env.ID,Name:env.Name,Binding:api.EnvironmentBinding{ServerID:"server-1",Project:"other"}}}},
	} {
		t.Run(tc.name,func(t *testing.T){
			readers:=ReadCapabilities{Environments:tc.reader,Workloads:testLogicalWorkloadReader{}}
			var out,errOut bytes.Buffer
			code:=RunWithReadCapabilities(context.Background(),
				[]string{"--incus-project","team","--environment","dev","workloads","list","-o","json"},
				&out,&errOut,nil,nil,nil,readers)
			if code==0||out.Len()!=0||errOut.Len()==0 {
				t.Fatalf("unsafe name resolved: code=%d stdout=%q stderr=%q",code,out.String(),errOut.String())
			}
		})
	}
}
