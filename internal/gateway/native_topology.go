package gateway

import "fmt"

// NativeGatewayTopology is a side-effect-free description of the two gateway
// services. Names are logical service identities, not Incus instance names.
// The native reconciler must resolve and verify physical resource identities
// before any mutation. No secret values are included in this representation.
type NativeGatewayTopology struct {
	Deployment        string
	Project           string
	ManagementNetwork string
	EgressNetwork     string
	DatabaseVolume    string
	Services          []NativeGatewayService
}

type NativeGatewayService struct {
	Name              string
	ImageReference    string
	Networks          []NativeGatewayNetwork
	Volume            string
	DependsOn         []string
	SecretEnvironment []string
}

type NativeGatewayNetwork struct {
	Name        string
	IPv4Address string
}

// NativeTopology exposes the narrow native gateway resource graph without
// interpreting Compose or authorizing a deployment. The inputs remain
// configured candidates until accepted-state selection is implemented.
// Image references must be resolved to immutable identities before execution.
func (s Spec) NativeTopology() (NativeGatewayTopology, error) {
	if err := s.Validate(); err != nil {
		return NativeGatewayTopology{}, fmt.Errorf("native gateway intent: %w", err)
	}
	litellmNetworks := []NativeGatewayNetwork{{Name: s.Network}}
	if s.EgressNetwork != "" {
		litellmNetworks = append(litellmNetworks, NativeGatewayNetwork{
			Name: s.EgressNetwork, IPv4Address: s.EgressIPv4Address,
		})
	}
	return NativeGatewayTopology{
		Deployment:        s.ID,
		Project:           s.Project,
		ManagementNetwork: s.Network,
		EgressNetwork:     s.EgressNetwork,
		DatabaseVolume:    "gateway-postgres",
		Services: []NativeGatewayService{
			{
				Name:              "postgres",
				ImageReference:    s.PostgresImage,
				Networks:          []NativeGatewayNetwork{{Name: s.Network}},
				Volume:            "gateway-postgres",
				SecretEnvironment: []string{"AGINCTUS_GATEWAY_POSTGRES_PASSWORD"},
			},
			{
				Name:           "litellm",
				ImageReference: s.LiteLLMImage,
				Networks:       litellmNetworks,
				DependsOn:      []string{"postgres"},
				SecretEnvironment: []string{
					"AGINCTUS_GATEWAY_POSTGRES_PASSWORD",
					"AGINCTUS_GATEWAY_MASTER_KEY",
					"AGINCTUS_GATEWAY_SALT_KEY",
				},
			},
		},
	}, nil
}
