package gateway

import "testing"

func TestNativeGatewayCreatePlan(t *testing.T) {
	pair,_:=instanceReaderFixture(t)
	p:="gw"
	obs:=[]NativeGatewayInstanceObservation{
		{Name:"postgres-1",Project:p,Status:NativeGatewayAbsent},
		{Name:"litellm-1",Project:p,Status:NativeGatewayCompatible},
	}
	got,err:=PlanNativeGatewayCreate(pair,obs)
	if err!=nil {t.Fatal(err)}
	if len(got.Steps)!=2 || got.Steps[0].Action!=NativeGatewayCreateStopped ||
		got.Steps[1].Action!=NativeGatewayRetainExisting {t.Fatalf("unexpected create plan: %+v",got)}
}
func TestNativeGatewayCreatePlanRejectsUnverifiedObservations(t *testing.T) {
	pair,_:=instanceReaderFixture(t)
	fresh:=func()[]NativeGatewayInstanceObservation{return []NativeGatewayInstanceObservation{
		{Name:"postgres-1",Project:pair.Postgres.Project,Status:NativeGatewayAbsent},
		{Name:"litellm-1",Project:pair.Postgres.Project,Status:NativeGatewayAbsent},
	}}
	for name,change:=range map[string]func(*[]NativeGatewayInstanceObservation){
		"missing":func(s *[]NativeGatewayInstanceObservation){*s=(*s)[:1]},
		"reordered":func(s *[]NativeGatewayInstanceObservation){(*s)[0],(*s)[1]=(*s)[1],(*s)[0]},
		"foreign":func(s *[]NativeGatewayInstanceObservation){(*s)[1].Project="foreign"},
		"unknown":func(s *[]NativeGatewayInstanceObservation){(*s)[0].Status="unknown"},
		"duplicated":func(s *[]NativeGatewayInstanceObservation){(*s)[1]=(*s)[0]},
	} {
		t.Run(name,func(t *testing.T){
			obs:=fresh();change(&obs)
			if _,err:=PlanNativeGatewayCreate(pair,obs);err==nil {t.Fatal("unsafe observation accepted")}
		})
	}
}
