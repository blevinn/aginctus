package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevinn/aginctus/pkg/api"
)

// EnvironmentReader is an authenticated, read-only project-state capability.
// The client deliberately does not know or implement Incus credential checks.
type EnvironmentReader interface {
	ListEnvironments(context.Context,string)(api.EnvironmentList,error)
	GetEnvironment(context.Context,string,string)(api.EnvironmentDetail,error)
}

type ListEnvironmentsRequest struct { Project string }
type GetEnvironmentRequest struct { Project string; ID string }

// EnvironmentsService delegates verified reads to the injected domain reader.
// It never invents enrollment based on a configured candidate.
type EnvironmentsService struct {reader EnvironmentReader; defaultProject string; defaultID string}
func (s *EnvironmentsService) List(ctx context.Context,request ListEnvironmentsRequest)(api.EnvironmentList,error){
	if ctx==nil {return api.EnvironmentList{},errors.New("environment inventory requires context")}
	if err:=ctx.Err();err!=nil{return api.EnvironmentList{},err}
	if request.Project=="" {return api.EnvironmentList{},fmt.Errorf("explicit project required for environment inventory")}
	if s==nil||s.reader==nil {return api.EnvironmentList{},fmt.Errorf("authenticated environment enrollment reader unavailable")}
	result,err:=s.reader.ListEnvironments(ctx,request.Project)
	if err!=nil{
		if ctx.Err()!=nil{return api.EnvironmentList{},ctx.Err()}
		return api.EnvironmentList{},fmt.Errorf("authenticated environment inventory unavailable")
	}
	if err:=validatedEnvironmentList(ctx,result,request.Project);err!=nil{return api.EnvironmentList{},err}
	return result,nil
}
func (s *EnvironmentsService) Get(ctx context.Context,request GetEnvironmentRequest)(api.EnvironmentDetail,error){
	if ctx==nil {return api.EnvironmentDetail{},errors.New("environment detail requires context")}
	if err:=ctx.Err();err!=nil{return api.EnvironmentDetail{},err}
	if request.Project==""||request.ID=="" {return api.EnvironmentDetail{},fmt.Errorf("explicit project and immutable environment ID required")}
	if s==nil||s.reader==nil {return api.EnvironmentDetail{},fmt.Errorf("authenticated environment enrollment reader unavailable")}
	if !environmentUUIDPattern.MatchString(request.ID){return api.EnvironmentDetail{},fmt.Errorf("immutable environment UUID required")}
	result,err:=s.reader.GetEnvironment(ctx,request.Project,request.ID)
	if err!=nil{
		if ctx.Err()!=nil{return api.EnvironmentDetail{},ctx.Err()}
		return api.EnvironmentDetail{},fmt.Errorf("authenticated environment identity unavailable")
	}
	if err:=validatedEnvironmentDetail(ctx,result,request.Project,request.ID);err!=nil{return api.EnvironmentDetail{},err}
	return result,nil
}

// Selected resolves an invocation-scoped default or explicit immutable UUID.
// Explicit targets override defaults, but neither changes Incus credentials.
// The reader must authenticate and verify the requested project each time.
func (s *EnvironmentsService) Selected(ctx context.Context,request GetEnvironmentRequest)(api.EnvironmentDetail,error){
	if s==nil {return api.EnvironmentDetail{},fmt.Errorf("environment client unavailable")}
	if request.Project=="" {request.Project=s.defaultProject}
	if request.ID=="" {request.ID=s.defaultID}
	return s.Get(ctx,request)
}
