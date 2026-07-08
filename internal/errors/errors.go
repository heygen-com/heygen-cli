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

const APIKeySettingsURL = "https://app.heygen.com/settings?nav=API"

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

// isNonSpecific reports whether an API-provided error code is too generic to
// trust for classification. v3 always sends a specific code; an empty or literal
// "error" code means we should classify from the HTTP status instead.
func isNonSpecific(code string) bool {
	return code == "" || code == "error"
}

// forbiddenHint is the shared non-login guidance for 403 responses: the caller is
// authenticated but not permitted, so re-authenticating will not help.
const forbiddenHint = "Your credentials are valid but not permitted for this. Check your plan, permissions, or workspace — re-authenticating will not help. Contact support if this is unexpected."

// FromAPIError converts an API error envelope and HTTP status code into a CLIError.
//
// The v3 API always returns a specific lowercase code, which is preserved. When the
// code is absent or non-specific (empty or literal "error" — e.g. a non-envelope
// edge/gateway response routed here by parseErrorResponse), the code is derived from
// the HTTP status instead.
func FromAPIError(statusCode int, apiErr *APIError, requestID string) *CLIError {
	exitCode := ExitGeneral
	code := apiErr.Code
	if isNonSpecific(code) {
		code = "" // fall through to status-derived classification
	}

	switch {
	case statusCode == 401:
		exitCode = ExitAuth
		if code == "" {
			code = "unauthorized"
		}
	case statusCode == 403:
		// Authenticated but not permitted — auth-family exit, but NOT "log in".
		exitCode = ExitAuth
		if code == "" {
			code = "forbidden"
		}
	case statusCode == 400:
		if code == "" {
			code = "validation_error"
		}
	case statusCode == 402:
		if code == "" {
			code = "insufficient_credit"
		}
	case statusCode == 404:
		if code == "" {
			code = "not_found"
		}
	case statusCode == 409:
		if code == "" {
			code = "conflict"
		}
	case statusCode == 413:
		if code == "" {
			code = "payload_too_large"
		}
	case statusCode == 429:
		if code == "" {
			code = "rate_limited"
		}
	case statusCode >= 500:
		// A v3 app 5xx carries its own code (internal_error); a 5xx with no specific
		// code did not come through the app's normal error path.
		if code == "" {
			code = "unclassified_server_error"
		}
	default:
		// Unmapped 4xx (405, 410, 415, 422, ...) with no specific code: the request
		// was rejected in a way we don't classify — client-side.
		if code == "" {
			code = "unclassified_client_error"
		}
	}

	message := apiErr.Message
	if message == "" {
		// Never render an empty message (e.g. a non-envelope body routed here).
		message = fmt.Sprintf("API returned HTTP %d", statusCode)
	}

	hint := hintForAPICode(code)
	if code == "invalid_parameter" && apiErr.Param != nil && *apiErr.Param != "" {
		hint = fmt.Sprintf("Invalid field %q. %s", *apiErr.Param, hint)
	}
	// Generic non-login hint for any unmapped 403 code (hintForAPICode is code-only
	// and cannot see the status).
	if hint == "" && statusCode == 403 {
		hint = forbiddenHint
	}

	return &CLIError{
		Code:      code,
		Message:   message,
		Hint:      hint,
		RequestID: requestID,
		ExitCode:  exitCode,
	}
}

// hintForAPICode returns a CLI-specific actionable hint for known API error
// codes. Returns "" if the code has no associated hint.
//
// Note: "unauthorized" deliberately has NO hint here — the source-aware
// enrichAuthHint (cmd/heygen) supplies better guidance (which credential is bad).
func hintForAPICode(code string) string {
	switch code {
	case "avatar_not_found":
		return "This avatar does not exist. Retrying the same ID is unlikely to help. List avatars: heygen avatar list"
	case "video_not_found":
		return "This resource does not exist. Retrying the same ID is unlikely to help. List your videos: heygen video list"
	case "voice_not_found":
		return "This voice does not exist. Retrying the same ID is unlikely to help. List voices: heygen voice list"
	case "insufficient_credit":
		return "Check your credit balance: heygen user me get. Purchase API credits: " + APIKeySettingsURL
	case "invalid_parameter":
		return "Use --request-schema on the command to see expected fields"
	case "rate_limited":
		return "The CLI retries rate-limited requests automatically. If this persists, reduce request frequency"
	case "resource_not_found", "not_found":
		return "The requested resource does not exist. Retrying the same ID is unlikely to help"
	case "asset_not_available":
		return "The asset may still be processing or was deleted"
	case "timeout":
		return "The operation may still be in progress. Check status with the corresponding get command"
	case "forbidden", "resource_access_denied", "ai_vendor_access_restricted":
		return forbiddenHint
	case "voice_not_usable":
		return "This voice can't be used for this request (e.g. not permitted on your plan). Choose another: heygen voice list"
	case "payload_too_large":
		return "The request or upload exceeds the size limit. Reduce the file size or payload"
	case "conflict":
		return "This conflicts with the current state (e.g. a duplicate or in-progress request). Retrying the same request is unlikely to help"
	case "unclassified_server_error":
		return "The server returned an error with no details. This is often transient — retry shortly; if it persists, contact support"
	}
	return ""
}
