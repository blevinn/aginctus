package cli

import (
	"context"
	"encoding/json"

	operatorops "github.com/blevinn/aginctus/internal/cli/operations/operator"
)

type operatorReadStatus = operatorops.ReadStatus

const (
	operatorReadOK          operatorReadStatus = operatorops.ReadOK
	operatorReadDenied      operatorReadStatus = operatorops.ReadDenied
	operatorReadNoIdentity  operatorReadStatus = operatorops.ReadNoIdentity
	operatorReadUnavailable operatorReadStatus = operatorops.ReadUnavailable
	operatorReadNoCLI       operatorReadStatus = operatorops.ReadNoCLI
	operatorReadCanceled    operatorReadStatus = operatorops.ReadCanceled
	operatorReadTimeout     operatorReadStatus = operatorops.ReadTimeout
	operatorReadInvalid     operatorReadStatus = operatorops.ReadInvalid
)

func readOperatorAPI(ctx context.Context, path string) (json.RawMessage, operatorReadStatus) {
	return operatorops.ReadAPI(ctx, path, inspectOperatorIncus)
}
func operatorReadFailure(err error) operatorReadStatus {
	return operatorops.ReadFailure(err)
}
func inspectOperatorIdentity(ctx context.Context, server json.RawMessage, project string) (string, bool) {
	return operatorops.InspectIdentity(ctx, server, project, readOperatorAPI)
}
