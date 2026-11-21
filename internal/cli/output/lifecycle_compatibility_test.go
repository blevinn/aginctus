package output

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/blevinn/aginctus/pkg/api"
)

func TestLifecyclePublicWireGoldens(t *testing.T) {
	envID := "11111111-1111-4111-8111-111111111111"
	cases:=[]struct{name string;document api.Document}{
		{"acceptance-plan",api.AcceptancePlan{
			TypeMeta:api.NewTypeMeta("AcceptancePlan"),
			PlanBase:api.PlanBase{Project:"team",EnvironmentID:envID,
				Changes:[]api.LifecycleChange{{Resource:"agent",Action:"update"}}},
			CandidateDigest:"sha256:candidate",
		}},
		{"reconciliation-result",api.ReconciliationResult{
			TypeMeta:api.NewTypeMeta("ReconciliationResult"),Project:"team",EnvironmentID:envID,
			AcceptedRevision:"revision-1",Stage:api.ReconciliationAcknowledged,
		}},
	}
	for _,tc:=range cases{
		t.Run(tc.name,func(t *testing.T){
			want,err:=os.ReadFile(filepath.Join("testdata",tc.name+".json"))
			if err!=nil{t.Fatal(err)}
			var out bytes.Buffer
			if err:=WriteStructured(&out,JSON,tc.document);err!=nil{t.Fatal(err)}
			if !bytes.Equal(want,out.Bytes()){t.Fatalf("wire contract changed\nwant:\n%s\ngot:\n%s",want,out.Bytes())}
		})
	}
}
