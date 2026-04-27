package errors

import "fmt"

// Exit codes. Kept minimal — the primary consumer is an LLM agent that reads
// the error JSON for details; exit codes just provide coarse routing.
const (
	ExitSuccess = 0
	ExitGeneral = 1
	ExitUsage   = 2
	ExitAuth    = 3
	ExitTimeout = 4
)

// CLIError is the canonical error type for all CLI operations.
type CLIError struct {
	Code      string // machine-readable: "auth_error", "not_found", "network_error"
	Message   string // human-readable description
	Hint      string // actionable fix: "Run heygen auth login"
	RequestID string // from API X-Request-Id header (if applicable)
	ExitCode  int    // process exit code (0/1/2/3/4)
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

	hint := hintForAPICode(code)
	if code == "invalid_parameter" && apiErr.Param != nil && *apiErr.Param != "" {
		hint = fmt.Sprintf("Invalid field %q. %s", *apiErr.Param, hint)
	}

	return &CLIError{
		Code:      code,
		Message:   apiErr.Message,
		Hint:      hint,
		RequestID: requestID,
		ExitCode:  exitCode,
	}
}

// hintForAPICode returns a CLI-specific actionable hint for known API error
// codes. Returns "" if the code has no associated hint.
func hintForAPICode(code string) string {
	switch code {
	case "avatar_not_found":
		return "List available avatars: heygen avatar list"
	case "video_not_found":
		return "List your videos: heygen video list"
	case "voice_not_found":
		return "List available voices: heygen voice list"
	case "insufficient_credit":
		return "Check your credit balance: heygen user me get"
	case "invalid_parameter":
		return "Use --request-schema on the command to see expected fields"
	case "rate_limited":
		return "The CLI retries rate-limited requests automatically. If this persists, reduce request frequency"
	case "resource_not_found", "not_found":
		return "The requested resource does not exist. Verify the ID is correct"
	case "asset_not_available":
		return "The asset may still be processing or was deleted"
	case "timeout":
		return "The operation may still be in progress. Check status with the corresponding get command"
	}
	return ""
}
