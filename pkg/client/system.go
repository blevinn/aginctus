package client

import (
	"context"
	"errors"

	"github.com/blevinn/aginctus/pkg/api"
)

// SystemReader is a narrow observed-server capability. It does not
// authenticate or enumerate any Incus project on the caller's behalf.
type SystemReader interface {
	ServerVersion(context.Context)(string,error)
}

type SystemService struct{reader SystemReader}

// Status reports the connected daemon version as diagnostic evidence only.
func (s *SystemService) Status(ctx context.Context)(api.SystemStatus,error){
	if ctx==nil{return api.SystemStatus{},errors.New("system status requires context")}
	if err:=ctx.Err();err!=nil{return api.SystemStatus{},err}
	if s==nil||s.reader==nil{return api.SystemStatus{},errors.New("Incus server observation unavailable")}
	version,err:=s.reader.ServerVersion(ctx)
	if err!=nil{return api.SystemStatus{},errors.New("Incus server observation unavailable")}
	if version==""{return api.SystemStatus{},errors.New("Incus server returned no version")}
	return api.SystemStatus{TypeMeta:api.NewTypeMeta("SystemStatus"),IncusVersion:version,Scope:"server-observation"},nil
}
