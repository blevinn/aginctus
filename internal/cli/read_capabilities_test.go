package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestUnifiedReadCapabilitiesShareOneInvocation(t *testing.T) {
	readers := ReadCapabilities{
		Environments: testEnrolledReader{},
		Workloads: testLogicalWorkloadReader{},
		Configured: stateCLIFixture{},
		Accepted: stateCLIFixture{},
		Diff: stateCLIFixture{},
	}
	for _,tc:=range []struct{args []string;expected string}{
		{[]string{"environment","list","-o","json"},`"kind": "EnvironmentList"`},
		{[]string{"--environment",stateCLIEnv,"workloads","list","-o","json"},`"kind": "WorkloadList"`},
		{[]string{"--environment",stateCLIEnv,"state","show","-o","json"},`"kind": "AcceptedStateStatus"`},
		{[]string{"--environment",stateCLIEnv,"state","diff","-o","json"},`"kind": "ConfiguredAcceptedDiff"`},
		{[]string{"--environment",stateCLIEnv,"state","plan-accept","-o","json"},`"kind": "AcceptancePlan"`},
	} {
		args:=append([]string{"--incus-project","team"},tc.args...)
		var out,errOut bytes.Buffer
		status:=RunWithReadCapabilities(context.Background(),args,&out,&errOut,nil,nil,nil,readers)
		if status!=0||errOut.Len()!=0||!strings.Contains(out.String(),tc.expected) {
			t.Fatalf("args=%v code=%d out=%q stderr=%q",args,status,out.String(),errOut.String())
		}
	}
}

func TestUnifiedReadCapabilitiesFailClosedWhenMissing(t *testing.T) {
	for _,tc:=range []struct{args []string; readers ReadCapabilities}{
		{[]string{"environment","list"},ReadCapabilities{Workloads:testLogicalWorkloadReader{}}},
		{[]string{"--environment",stateCLIEnv,"workloads","list"},ReadCapabilities{Environments:testEnrolledReader{}}},
		{[]string{"--environment",stateCLIEnv,"state","show"},ReadCapabilities{Environments:testEnrolledReader{}}},
		{[]string{"--environment",stateCLIEnv,"state","plan-accept"},ReadCapabilities{Accepted:stateCLIFixture{}}},
	} {
		args:=append([]string{"--incus-project","team"},tc.args...)
		args=append(args,"-o","json")
		var out,errOut bytes.Buffer
		status:=RunWithReadCapabilities(context.Background(),args,&out,&errOut,nil,nil,nil,tc.readers)
		if status==0||out.Len()!=0||errOut.Len()==0 {
			t.Fatalf("partial capabilities bypassed checks: args=%v code=%d out=%q stderr=%q",args,status,out.String(),errOut.String())
		}
	}
}
