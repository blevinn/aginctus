package client

import(
	"context"
	"fmt"

	"github.com/blevinn/aginctus/pkg/api"
)

// LogicalWorkloadReader MUST obtain authorization through the authenticated
// project/environment authority and resolve logical workloads through their
// environment-owned records, never Incus instance labels alone.
type LogicalWorkloadReader interface {
	ListWorkloads(context.Context,api.ListWorkloadsRequest)(api.WorkloadList,error)
	GetWorkload(context.Context,api.GetWorkloadRequest)(api.WorkloadDetail,error)
	WorkloadStatus(context.Context,api.WorkloadStatusRequest)(api.WorkloadStatus,error)
}
func readScope(project,environment string) error{
	if project==""||!environmentUUIDPattern.MatchString(environment){return fmt.Errorf("verified project and immutable environment UUID required")}
	return nil
}
func (s *WorkloadsService) List(ctx context.Context,req api.ListWorkloadsRequest)(api.WorkloadList,error){
	if err:=ctx.Err();err!=nil{return api.WorkloadList{},err}
	if err:=readScope(req.Project,req.EnvironmentID);err!=nil{return api.WorkloadList{},err}
	if s==nil||s.logical==nil{return api.WorkloadList{},fmt.Errorf("authenticated logical workload reader unavailable")}
	result,err:=s.logical.ListWorkloads(ctx,req)
	if err!=nil{return api.WorkloadList{},err}
	if result.APIVersion!=api.APIVersionV1Alpha1||result.EnvironmentID!=req.EnvironmentID||result.Kind!="WorkloadList"||result.Items==nil{return api.WorkloadList{},fmt.Errorf("logical workload response scope mismatch")}
	seen:=map[string]bool{}
	for _,w:=range result.Items{
		if validateLogicalWorkload(w,req.EnvironmentID)!=nil||seen[w.Name]||(req.Class!=""&&w.Class!=req.Class){
			return api.WorkloadList{},fmt.Errorf("logical workload response ownership mismatch")
		}
		seen[w.Name]=true
	}
	return result,nil
}
func (s *WorkloadsService) Get(ctx context.Context,req api.GetWorkloadRequest)(api.WorkloadDetail,error){
	if err:=ctx.Err();err!=nil{return api.WorkloadDetail{},err}
	if err:=readScope(req.Project,req.EnvironmentID);err!=nil{return api.WorkloadDetail{},err}
	if req.Name==""{return api.WorkloadDetail{},fmt.Errorf("workload name required")}
	if s==nil||s.logical==nil{return api.WorkloadDetail{},fmt.Errorf("authenticated logical workload reader unavailable")}
	result,err:=s.logical.GetWorkload(ctx,req)
	if err!=nil{return api.WorkloadDetail{},err}
	if result.APIVersion!=api.APIVersionV1Alpha1||result.Kind!="Workload"||result.Name!=req.Name||validateLogicalWorkload(result.Workload,req.EnvironmentID)!=nil{return api.WorkloadDetail{},fmt.Errorf("logical workload response ownership mismatch")}
	return result,nil
}
func (s *WorkloadsService) Status(ctx context.Context,req api.WorkloadStatusRequest)(api.WorkloadStatus,error){
	if err:=ctx.Err();err!=nil{return api.WorkloadStatus{},err}
	if err:=readScope(req.Project,req.EnvironmentID);err!=nil{return api.WorkloadStatus{},err}
	if req.Name==""{return api.WorkloadStatus{},fmt.Errorf("workload name required")}
	if s==nil||s.logical==nil{return api.WorkloadStatus{},fmt.Errorf("authenticated logical workload reader unavailable")}
	result,err:=s.logical.WorkloadStatus(ctx,req)
	if err!=nil{return api.WorkloadStatus{},err}
	if result.APIVersion!=api.APIVersionV1Alpha1||result.Kind!="WorkloadStatus"||result.EnvironmentID!=req.EnvironmentID||result.Name!=req.Name||validateLogicalObservations(result)!=nil{return api.WorkloadStatus{},fmt.Errorf("logical workload observation scope mismatch")}
	return result,nil
}
