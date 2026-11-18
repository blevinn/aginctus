package images

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAmbientImageUpdateIdempotent(t *testing.T) {
	fp:=strings.Repeat("a",64)
	commands:=0
	a:=LegacyUpdateActions{
		Artifact:func(_,role,_ string)(string,error){return role,nil},
		Fingerprint:func(string,string)(string,error){return fp,nil},
		LocalImages:func(context.Context)([]LocalImage,error){
			return []LocalImage{{Fingerprint:fp,Aliases:[]struct{Name string `json:"name"`}{{Name:"target"}}}},nil
		},
		Run:func(context.Context,...string)([]byte,error){commands++;return nil,nil},
	}
	if err:=UpdateAmbientImage(context.Background(),"target","dir",a);err!=nil{t.Fatal(err)}
	if commands!=0{t.Fatal("idempotent update mutated Incus")}
}

func TestAmbientImageUpdateRestoresBackupOnPromotionFailure(t *testing.T) {
	fp:=strings.Repeat("a",64)
	var commands [][]string
	a:=LegacyUpdateActions{
		Artifact:func(_,role,_ string)(string,error){return role,nil},
		Fingerprint:func(string,string)(string,error){return fp,nil},
		LocalImages:func(context.Context)([]LocalImage,error){
			return []LocalImage{{Fingerprint:strings.Repeat("b",64),Aliases:[]struct{Name string `json:"name"`}{{Name:"target"}}}},nil
		},
		Run:func(_ context.Context,args ...string)([]byte,error){
			commands=append(commands,append([]string(nil),args...))
			for _,s:=range args {if s=="--project"||s=="--force-local"||s=="--reuse"{t.Fatalf("legacy path gained scoped or replacement flags: %q",args)}}
			if len(args)>=5 && args[0]=="image"&&args[1]=="alias"&&args[2]=="rename" &&
				args[3]!="target" && args[4]=="target" && !strings.Contains(args[3],"previous") {
				return nil,errors.New("promotion failed")
			}
			return nil,nil
		},
	}
	if err:=UpdateAmbientImage(context.Background(),"target","dir",a);err==nil{t.Fatal("promotion should fail")}
	foundBackup:=false
	for _,args:=range commands {
		if len(args)>=5&&args[0]=="image"&&args[1]=="alias"&&args[2]=="rename"&&
			strings.Contains(args[3],"previous")&&args[4]=="target"{foundBackup=true}
	}
	if !foundBackup{t.Fatalf("missing rollback: %#v",commands)}
}
