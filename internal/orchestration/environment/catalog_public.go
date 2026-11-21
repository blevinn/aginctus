package environment

import (
	"context"

	"github.com/blevinn/aginctus/pkg/api"
)

// ListEnvironments exposes only authenticated catalog identities, never
// candidate-file declarations or unverified Incus resource labels.
func (r Catalog) ListEnvironments(ctx context.Context, project string) (api.EnvironmentList, error) {
	records, err := r.List(ctx, project)
	if err != nil { return api.EnvironmentList{}, err }
	result := api.EnvironmentList{TypeMeta:api.NewTypeMeta("EnvironmentList"),Items:make([]api.Environment,0,len(records))}
	for _, d := range records {
		result.Items=append(result.Items,api.Environment{ID:d.ID,Name:d.Name,Binding:api.EnvironmentBinding{ServerID:d.Binding.ServerID,Project:d.Binding.Project}})
	}
	return result,nil
}

// GetEnvironment selects a verified enrollment by immutable ID, using
// exactly the same authority check as ListEnvironments.
func (r Catalog) GetEnvironment(ctx context.Context, project, id string) (api.EnvironmentDetail,error) {
	records,err:=r.List(ctx,project)
	if err!=nil {return api.EnvironmentDetail{},err}
	for _,d:=range records {
		if d.ID==id {
			return api.EnvironmentDetail{TypeMeta:api.NewTypeMeta("Environment"),Environment:api.Environment{
				ID:d.ID,Name:d.Name,Binding:api.EnvironmentBinding{ServerID:d.Binding.ServerID,Project:d.Binding.Project},
			}},nil
		}
	}
	return api.EnvironmentDetail{},api.NewError(api.CodeNotFound,"enrolled environment not found")
}
