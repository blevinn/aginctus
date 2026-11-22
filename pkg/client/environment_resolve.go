package client

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// ResolveEnvironmentRequest names a project-scoped enrolled environment by
// immutable UUID or display name. Names are convenience selectors only:
// they are never ownership or authorization evidence.
type ResolveEnvironmentRequest struct {
	Project string
	Selector string
}

// Resolve verifies the project inventory before selecting a display name,
// fails on ambiguity, then re-reads the immutable UUID and requires identity
// and binding to match the inventory snapshot. The underlying reader must
// independently authenticate both operations.
func (s *EnvironmentsService) Resolve(ctx context.Context, req ResolveEnvironmentRequest) (api.EnvironmentDetail,error) {
	if ctx == nil { return api.EnvironmentDetail{},errors.New("environment resolution requires context") }
	if err:=ctx.Err();err!=nil{return api.EnvironmentDetail{},err}
	if req.Project==""||req.Selector==""{return api.EnvironmentDetail{},errors.New("explicit project and environment selector required")}
	if environmentUUIDPattern.MatchString(req.Selector) {
		return s.Get(ctx,GetEnvironmentRequest{Project:req.Project,ID:req.Selector})
	}
	list,err:=s.List(ctx,ListEnvironmentsRequest{Project:req.Project})
	if err!=nil{return api.EnvironmentDetail{},err}
	var match *api.Environment
	for _,candidate:=range list.Items {
		if candidate.Name!=req.Selector {continue}
		if match!=nil {return api.EnvironmentDetail{},errors.New("ambiguous environment name in project")}
		copied:=candidate
		match=&copied
	}
	if match==nil{return api.EnvironmentDetail{},errors.New("enrolled environment not found")}
	detail,err:=s.Get(ctx,GetEnvironmentRequest{Project:req.Project,ID:match.ID})
	if err!=nil{return api.EnvironmentDetail{},err}
	if detail.Environment!=*match {
		return api.EnvironmentDetail{},errors.New("enrolled environment identity changed during selection")
	}
	return detail,nil
}
