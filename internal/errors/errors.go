package errors

import "fmt"

// Exit codes. Kept minimal — the primary consumer is an LLM agent that reads
// the error JSON for details; exit codes just provide coarse routing.
const (
	ExitSuccess = 0
	ExitGeneral = 1
	ExitUsage   = 2
	ExitAuth    = 3
)

// CLIError is the canonical error type for all CLI operations.
type CLIError struct {
	Code      string // machine-readable: "auth_error", "not_found", "network_error"
	Message   string // human-readable description
	Hint      string // actionable fix: "Run heygen auth login"
	RequestID string // from API X-Request-Id header (if applicable)
	ExitCode  int    // process exit code (0/1/2/3)
}

// Error implements the error interface.
func (e *CLIError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s (hint: %s)", e.Message, e.Hint)
	}
	return e.Message
}

// ToErrorEnvelope returns the canonical JSON error envelope shape:
// {"error": {"code": ..., "message": ..., "hint": ..., "request_id": ...}}
func (e *CLIError) ToErrorEnvelope() map[string]any {
	inner := map[string]any{
		"code":    e.Code,
		"message": e.Message,
	}
	if e.Hint != "" {
		inner["hint"] = e.Hint
	}
	if e.RequestID != "" {
		inner["request_id"] = e.RequestID
	}
	return map[string]any{"error": inner}
}

// New creates a CLIError with ExitGeneral.
func New(message string) *CLIError {
	return &CLIError{
		Code:     "error",
		Message:  message,
		ExitCode: ExitGeneral,
	}
}

// NewAuth creates a CLIError with ExitAuth.
func NewAuth(message, hint string) *CLIError {
	return &CLIError{
		Code:     "auth_error",
		Message:  message,
		Hint:     hint,
		ExitCode: ExitAuth,
	}
}

// NewUsage creates a CLIError with ExitUsage.
func NewUsage(message string) *CLIError {
	return &CLIError{
		Code:     "usage_error",
		Message:  message,
		ExitCode: ExitUsage,
	}
}

// FromAPIError converts an API error envelope and HTTP status code into a CLIError.
func FromAPIError(statusCode int, apiErr *APIError, requestID string) *CLIError {
	exitCode := ExitGeneral
	code := apiErr.Code

	switch {
	case statusCode == 401 || statusCode == 403:
		exitCode = ExitAuth
		if code == "" {
			code = "auth_error"
		}
	case statusCode == 400:
		if code == "" {
			code = "validation_error"
		}
	case statusCode == 404:
		if code == "" {
			code = "not_found"
		}
	case statusCode == 429:
		if code == "" {
			code = "rate_limited"
		}
	case statusCode >= 500:
		if code == "" {
			code = "server_error"
		}
	default:
		if code == "" {
			code = "error"
		}
	}

	return &CLIError{
		Code:      code,
		Message:   apiErr.Message,
		RequestID: requestID,
		ExitCode:  exitCode,
	}
}
