package credentialdaemon

import (
	"context"
	"fmt"

	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/orchestration"
	applydriver "github.com/blevinn/aginctus/internal/orchestration/drivers/apply"
	"github.com/blevinn/aginctus/internal/statevolume"
)

type Spec struct {
	GatewayID, Name, ImageAlias, Pool, ManagementNetwork, EgressNetwork string
}

func FromConfig(c *config.Config) (Spec, error) {
	s := Spec{}
	for _, field := range []struct {
		key  string
		dest *string
	}{
		{"gateway.id", &s.GatewayID},
		{"gateway.credentials.storage.pool", &s.Pool},
		{"incus.management.network.name", &s.ManagementNetwork},
		{"gateway.egress.network.name", &s.EgressNetwork},
	} {
		value, err := c.String(field.key)
		if err != nil {
			return Spec{}, err
		}
		*field.dest = value
	}
	s.Name = "aginctus-gateway-" + s.GatewayID + "-credential-daemon"
	s.ImageAlias = "aginctus-oauth-gateway"
	return s, s.Validate()
}
func (s Spec) Validate() error {
	if err := (statevolume.GatewayCredentials{Pool: s.Pool, GatewayID: s.GatewayID}).Validate(); err != nil {
		return err
	}
	if s.ManagementNetwork == "" || s.EgressNetwork == "" || s.ManagementNetwork == s.EgressNetwork {
		return fmt.Errorf("credential daemon requires distinct management and egress networks")
	}
	if s.Name != "aginctus-gateway-"+s.GatewayID+"-credential-daemon" || s.ImageAlias != "aginctus-oauth-gateway" {
		return fmt.Errorf("unsupported credential daemon identity or image")
	}
	return nil
}
func (s Spec) Plan() (orchestration.Plan, error) {
	if err := s.Validate(); err != nil {
		return orchestration.Plan{}, err
	}
	g, err := orchestration.NewGenerator()
	if err != nil {
		return orchestration.Plan{}, err
	}
	return g.Evaluate(`local ag=import 'aginctus/orchestration.libsonnet';
local c=std.extVar('config');
ag.orchestration('credential-daemon', [
 ag.apply('credential-daemon', {
  operation:'upsert',
  rejectUnsupportedChanges:true,
  ensureRunning:true,
  requireExistingConfig:{
   'user.aginctus.managed':'true',
   'user.aginctus.resource':'infrastructure',
   'user.aginctus.role':'credential-daemon',
   'user.aginctus.gateway':c.gatewayID,
  },
  documents:[{
   kind:'instance',
   name:c.name,
   image:c.image,
   profiles:[],
   description:'Aginctus private credential manager (no inference enabled)',
   config:{
    'user.aginctus.managed':'true',
    'user.aginctus.resource':'infrastructure',
    'user.aginctus.role':'credential-daemon',
    'user.aginctus.gateway':c.gatewayID,
   },
   devices:{
    root:{type:'disk',path:'/',pool:c.pool},
    credentials:{type:'disk',pool:c.pool,source:c.volume,path:'/var/lib/aginctus/oauth'},
    management:{type:'nic',network:c.management,name:'eth0'},
    egress:{type:'nic',network:c.egress,name:'eth1'},
   },
  }],
 }),
])`, map[string]any{
		"gatewayID": s.GatewayID, "name": s.Name, "image": s.ImageAlias, "pool": s.Pool,
		"volume":     (statevolume.GatewayCredentials{Pool: s.Pool, GatewayID: s.GatewayID}).Name(),
		"management": s.ManagementNetwork, "egress": s.EgressNetwork,
	})
}

// Provisioning is deliberately limited to the explicit CLI operation.
var ensureStorage = statevolume.EnsureGatewayCredentialsOnHost
var deployPlan = func(ctx context.Context, p orchestration.Plan) error {
	engine := orchestration.NewEngine(map[string]orchestration.Driver{"apply": applydriver.New()})
	_, err := engine.Execute(ctx, p, orchestration.ExecuteOptions{})
	return err
}

func (s Spec) Apply(ctx context.Context) error {
	p, err := s.Plan()
	if err != nil {
		return err
	}
	if err := ensureStorage(ctx, statevolume.GatewayCredentials{Pool: s.Pool, GatewayID: s.GatewayID}); err != nil {
		return fmt.Errorf("credential volume preflight: %w", err)
	}
	if err := deployPlan(ctx, p); err != nil {
		return fmt.Errorf("credential daemon deployment: %w", err)
	}
	return nil
}
