package cli

import (
	"context"

	gatewayops "github.com/blevinn/aginctus/internal/cli/operations/gateway"
	"github.com/blevinn/aginctus/internal/config"
	"github.com/blevinn/aginctus/internal/gateway"
	"github.com/blevinn/aginctus/internal/gatewaycontrol"
	siwc "github.com/blevinn/aginctus/internal/plugins/openai-siwc"
)

var exchangeGatewaySIWC = gateway.ExchangeSIWC
var prepareGatewaySIWC = gateway.PrepareSIWC

func siwcAttachment(effective *config.Config) (gateway.CredentialAttachment, error) {
	return gatewayops.SIWCAttachment(effective)
}

func runManagedSIWC(ctx context.Context, effective *config.Config, store *gatewaycontrol.Store, operation siwc.Operation) (siwc.Result, error) {
	return gatewayops.RunManagedSIWC(ctx, effective, store, operation, exchangeGatewaySIWC)
}

func configureSIWCOperations(store *gatewaycontrol.Store, effective *config.Config) {
	gatewayops.ConfigureSIWCOperations(store, effective, func(ctx context.Context, attachment gateway.CredentialAttachment, update gateway.CredentialUpdate) error {
		return exchangeGatewaySIWC(ctx, attachment, update)
	})
}

func syncGatewaySIWC(ctx context.Context, effective *config.Config) error {
	return gatewayops.SyncGatewaySIWC(ctx, effective, prepareGatewaySIWC, configuredGatewayStore, runManagedSIWC)
}
