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
		case api.CodeInvalidArgument, api.CodeNotFound, api.CodePermissionDenied, api.CodeConflict,
			api.CodeUnavailable, api.CodeInternal:
			if typed.Message != "" {
				document = api.NewError(typed.Code, typed.Message)
			}
		}
	}
	return WriteStructured(w, format, document)
}
