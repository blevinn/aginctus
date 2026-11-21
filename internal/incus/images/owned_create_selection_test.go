package images

import (
	"context"
	"testing"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

func TestClaimedCreateDomainSelectionMustPrecedeLease(t *testing.T) {
	good,err:=incusscope.NewProjectScope("tenant")
	if err!=nil{t.Fatal(err)}
	for _,tc:=range []struct{name string; scope incusscope.ProjectScope; owner,alias,dir string}{
		{"invalid scope",incusscope.ProjectScope{},"system","image","dir"},
		{"unknown owner",good,"administrator","image","dir"},
		{"empty alias",good,"user","","dir"},
		{"invalid alias",good,"user","bad alias","dir"},
		{"empty artifact directory",good,"system","image",""},
	} {
		t.Run(tc.name,func(t *testing.T){
			leased:=false
			err:=CreateClaimedImage(context.Background(),tc.scope,tc.owner,tc.alias,tc.dir,OwnedCreateActions{
				Lease:func(incusscope.ProjectScope)(func(),error){leased=true;return func(){},nil},
			})
			if err!=OwnedCreateFailure("selection"){t.Fatalf("expected selection rejection, got %v",err)}
			if leased{t.Fatal("invalid image identity reached writer lease")}
		})
	}
}
