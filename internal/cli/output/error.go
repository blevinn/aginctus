package output

import (
	"errors"
	"io"

	"github.com/blevinn/aginctus/pkg/api"
)

// WriteError emits one versioned error document on the supplied diagnostic
// stream. Only explicitly safe api.Error projections retain their message.
// Unclassified backend, filesystem and transport errors are not disclosed.
func WriteError(w io.Writer, format Format, cause error) error {
	if cause == nil {
		return errors.New("cannot encode a successful operation as an error")
	}
	if format != JSON && format != YAML {
		return errors.New("structured error output requires json or yaml")
	}
	document := api.NewError(api.CodeInternal, "operation failed")
	var typed *api.Error
	if errors.As(cause, &typed) && typed != nil {
		switch typed.Code {
		case api.CodeInvalidArgument, api.CodeNotFound, api.CodePermissionDenied,
			api.CodeConflict, api.CodeUnavailable:
			// These classifications are explicitly public-facing; their
			// messages must already have been sanitized at the boundary.
			if typed.Message != "" {
				document = api.NewError(typed.Code, typed.Message)
			}
		case api.CodeInternal:
			// An internal error category never authorizes disclosure of an
			// arbitrary internal message, even when wrapped as api.Error.
			document = api.NewError(api.CodeInternal, "operation failed")
		}
	}
	return WriteStructured(w, format, document)
}
