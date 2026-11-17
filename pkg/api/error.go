package api

// ErrorCode is a machine-readable category, not an exit status or backend
// diagnostic. Map domain failures to these categories at an explicit boundary.
type ErrorCode string

const (
	CodeInvalidArgument  ErrorCode = "invalid_argument"
	CodeNotFound         ErrorCode = "not_found"
	CodePermissionDenied ErrorCode = "permission_denied"
	CodeConflict         ErrorCode = "conflict"
	CodeUnavailable      ErrorCode = "unavailable"
	CodeInternal         ErrorCode = "internal"
)

// Error is a public, serializable failure projection. Message must already be
// safe for disclosure; this type does not redact secrets automatically.
type Error struct {
	TypeMeta
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// NewError builds a machine-readable error without embedding private causes.
func NewError(code ErrorCode, message string) *Error {
	return &Error{TypeMeta: NewTypeMeta("Error"), Code: code, Message: message}
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}
