package client

import (
	"context"
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
type EnvironmentsService struct {reader EnvironmentReader}
func (s *EnvironmentsService) List(ctx context.Context,request ListEnvironmentsRequest)(api.EnvironmentList,error){
	if err:=ctx.Err();err!=nil{return api.EnvironmentList{},err}
	if request.Project=="" {return api.EnvironmentList{},fmt.Errorf("explicit project required for environment inventory")}
	if s==nil||s.reader==nil {return api.EnvironmentList{},fmt.Errorf("authenticated environment enrollment reader unavailable")}
	return s.reader.ListEnvironments(ctx,request.Project)
}
func (s *EnvironmentsService) Get(ctx context.Context,request GetEnvironmentRequest)(api.EnvironmentDetail,error){
	if err:=ctx.Err();err!=nil{return api.EnvironmentDetail{},err}
	if request.Project==""||request.ID=="" {return api.EnvironmentDetail{},fmt.Errorf("explicit project and immutable environment ID required")}
	if s==nil||s.reader==nil {return api.EnvironmentDetail{},fmt.Errorf("authenticated environment enrollment reader unavailable")}
	return s.reader.GetEnvironment(ctx,request.Project,request.ID)
}
