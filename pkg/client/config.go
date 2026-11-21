package client

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// ConfigSelectionReader returns only the validated effective Incus project
// preference. A configured project name never grants project authorization.
type ConfigSelectionReader interface {
	SelectedIncusProject(context.Context) (string, error)
}

type ConfigService struct { reader ConfigSelectionReader }

// Selection reports only the configured project preference. This is useful
// for diagnostics but must never be used as Incus authorization evidence.
func (s *ConfigService) Selection(ctx context.Context) (api.ConfigSelection,error) {
	if ctx==nil {return api.ConfigSelection{},errors.New("configuration selection requires context")}
	if err:=ctx.Err();err!=nil{return api.ConfigSelection{},err}
	if s==nil||s.reader==nil {return api.ConfigSelection{},errors.New("configuration selection reader unavailable")}
	project,err:=s.reader.SelectedIncusProject(ctx)
	if err!=nil{return api.ConfigSelection{},errors.New("validated configuration selection unavailable")}
	if project==""||project=="default"{return api.ConfigSelection{},errors.New("invalid configured Incus project")}
	return api.ConfigSelection{
		TypeMeta:api.NewTypeMeta("ConfigSelection"),
		IncusProject:project,Authority:"preference-only",
	},nil
}
