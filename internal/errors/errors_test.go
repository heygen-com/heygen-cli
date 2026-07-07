package errors

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCLIError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *CLIError
		want string
	}{
		{
			name: "message only",
			err:  &CLIError{Message: "something failed"},
			want: "something failed",
		},
		{
			name: "message with hint",
			err:  &CLIError{Message: "auth failed", Hint: "run heygen auth login"},
			want: "auth failed (hint: run heygen auth login)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCLIError_ToErrorEnvelope(t *testing.T) {
	err := &CLIError{
		Code:      "not_found",
		Message:   "Video abc123 not found",
		Hint:      "Check the video ID with: heygen video list",
		RequestID: "req_xyz",
	}

	envelope := err.ToErrorEnvelope()

	// Marshal and re-parse to verify JSON shape
	data, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		t.Fatalf("failed to marshal envelope: %v", marshalErr)
	}

	var parsed map[string]map[string]string
	if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
		t.Fatalf("failed to unmarshal envelope: %v", jsonErr)
	}

	inner := parsed["error"]
	if inner["code"] != "not_found" {
		t.Errorf("code = %q, want %q", inner["code"], "not_found")
	}
	if inner["message"] != "Video abc123 not found" {
		t.Errorf("message = %q, want %q", inner["message"], "Video abc123 not found")
	}
	if inner["hint"] != "Check the video ID with: heygen video list" {
		t.Errorf("hint = %q, want expected value", inner["hint"])
	}
	if inner["request_id"] != "req_xyz" {
		t.Errorf("request_id = %q, want %q", inner["request_id"], "req_xyz")
	}
}

func TestCLIError_ToErrorEnvelope_OmitsEmpty(t *testing.T) {
	err := &CLIError{Code: "error", Message: "fail"}
	envelope := err.ToErrorEnvelope()
	inner := envelope["error"].(map[string]any)

	if _, ok := inner["hint"]; ok {
		t.Error("hint should be omitted when empty")
	}
	if _, ok := inner["request_id"]; ok {
		t.Error("request_id should be omitted when empty")
	}
}

func TestFromAPIError_ExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantExit   int
		wantCode   string
	}{
		{"401 → unauthorized", 401, ExitAuth, "unauthorized"},
		{"403 → forbidden", 403, ExitAuth, "forbidden"},
		{"400 → validation", 400, ExitGeneral, "validation_error"},
		{"402 → insufficient_credit", 402, ExitGeneral, "insufficient_credit"},
		{"404 → not_found", 404, ExitGeneral, "not_found"},
		{"409 → conflict", 409, ExitGeneral, "conflict"},
		{"413 → payload_too_large", 413, ExitGeneral, "payload_too_large"},
		{"429 → rate_limited", 429, ExitGeneral, "rate_limited"},
		{"500 → unclassified_server_error", 500, ExitGeneral, "unclassified_server_error"},
		{"502 → unclassified_server_error", 502, ExitGeneral, "unclassified_server_error"},
		{"503 → unclassified_server_error", 503, ExitGeneral, "unclassified_server_error"},
		{"405 (unmapped 4xx) → unclassified_client_error", 405, ExitGeneral, "unclassified_client_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := &APIError{Message: "test error"}
			cliErr := FromAPIError(tt.statusCode, apiErr, "req_123")

			if cliErr.ExitCode != tt.wantExit {
				t.Errorf("ExitCode = %d, want %d", cliErr.ExitCode, tt.wantExit)
			}
			if cliErr.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", cliErr.Code, tt.wantCode)
			}
			if cliErr.RequestID != "req_123" {
				t.Errorf("RequestID = %q, want %q", cliErr.RequestID, "req_123")
			}
		})
	}
}

func TestFromAPIError_PreservesAPICode(t *testing.T) {
	apiErr := &APIError{Code: "custom_code", Message: "custom error"}
	cliErr := FromAPIError(400, apiErr, "")

	if cliErr.Code != "custom_code" {
		t.Errorf("Code = %q, want %q (should preserve API code)", cliErr.Code, "custom_code")
	}
}

func TestFromAPIError_AvatarNotFound_AddsHint(t *testing.T) {
	apiErr := &APIError{Code: "avatar_not_found", Message: "avatar with id X not found"}
	cliErr := FromAPIError(404, apiErr, "")

	if !strings.Contains(cliErr.Hint,"heygen avatar list") {
		t.Errorf("Hint = %q, want heygen avatar list", cliErr.Hint)
	}
}

func TestFromAPIError_VideoNotFound_AddsHint(t *testing.T) {
	apiErr := &APIError{Code: "video_not_found", Message: "video not found"}
	cliErr := FromAPIError(404, apiErr, "")

	if !strings.Contains(cliErr.Hint,"heygen video list") {
		t.Errorf("Hint = %q, want heygen video list", cliErr.Hint)
	}
}

func TestFromAPIError_VoiceNotFound_AddsHint(t *testing.T) {
	apiErr := &APIError{Code: "voice_not_found", Message: "voice not found"}
	cliErr := FromAPIError(404, apiErr, "")

	if !strings.Contains(cliErr.Hint,"heygen voice list") {
		t.Errorf("Hint = %q, want heygen voice list", cliErr.Hint)
	}
}

func TestFromAPIError_UnknownCode_NoHint(t *testing.T) {
	apiErr := &APIError{Code: "something_obscure", Message: "not found"}
	cliErr := FromAPIError(404, apiErr, "")

	if cliErr.Hint != "" {
		t.Errorf("Hint = %q, want empty for unknown code", cliErr.Hint)
	}
}

func TestHintForAPICode_AllMappedCodes(t *testing.T) {
	codes := []struct {
		code         string
		wantContains string
	}{
		{"avatar_not_found", "heygen avatar list"},
		{"video_not_found", "heygen video list"},
		{"voice_not_found", "heygen voice list"},
		{"insufficient_credit", "heygen user me get"},
		{"invalid_parameter", "--request-schema"},
		{"rate_limited", "retries rate-limited"},
		{"resource_not_found", "does not exist"},
		{"not_found", "does not exist"},
		{"asset_not_available", "may still be processing"},
		{"timeout", "may still be in progress"},
		{"forbidden", "not permitted"},
		{"resource_access_denied", "not permitted"},
		{"ai_vendor_access_restricted", "not permitted"},
		{"voice_not_usable", "heygen voice list"},
		{"payload_too_large", "size limit"},
		{"conflict", "conflicts with the current state"},
		{"unclassified_server_error", "no details"},
	}
	for _, tt := range codes {
		t.Run(tt.code, func(t *testing.T) {
			hint := hintForAPICode(tt.code)
			if hint == "" {
				t.Fatalf("hintForAPICode(%q) returned empty", tt.code)
			}
			if !strings.Contains(hint, tt.wantContains) {
				t.Errorf("hint = %q, want it to contain %q", hint, tt.wantContains)
			}
		})
	}
}

// A 404 with no specific API code is now classified as not_found and carries the
// standard not_found hint. The hint lookup uses the effective (status-derived) code,
// so status-derived codes like insufficient_credit (402) also get their hints.
func TestFromAPIError_Generic404_GetsNotFoundHint(t *testing.T) {
	apiErr := &APIError{Message: "not found"}
	cliErr := FromAPIError(404, apiErr, "")

	if cliErr.Code != "not_found" {
		t.Errorf("Code = %q, want not_found", cliErr.Code)
	}
	if !strings.Contains(cliErr.Hint, "does not exist") {
		t.Errorf("Hint = %q, want the not_found hint", cliErr.Hint)
	}
}

// A non-specific API code (empty or literal "error") is treated as absent and the
// code is derived from the HTTP status instead.
func TestFromAPIError_NonSpecificCodeOverriddenByStatus(t *testing.T) {
	tests := []struct {
		name       string
		apiCode    string
		statusCode int
		wantCode   string
	}{
		{"literal error at 402", "error", 402, "insufficient_credit"},
		{"literal error at 500", "error", 500, "unclassified_server_error"},
		{"empty at 409", "", 409, "conflict"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cliErr := FromAPIError(tt.statusCode, &APIError{Code: tt.apiCode, Message: "x"}, "")
			if cliErr.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", cliErr.Code, tt.wantCode)
			}
		})
	}
}

// A non-envelope response routed here with an empty APIError must still render a
// non-empty message including the HTTP status.
func TestFromAPIError_SynthesizesMessageWhenEmpty(t *testing.T) {
	cliErr := FromAPIError(502, &APIError{}, "req_1")
	if !strings.Contains(cliErr.Message, "502") {
		t.Errorf("Message = %q, want it to mention HTTP 502", cliErr.Message)
	}
	if cliErr.Code != "unclassified_server_error" {
		t.Errorf("Code = %q, want unclassified_server_error", cliErr.Code)
	}
}

// A 403 is auth-family (exit 3) but must NOT get a "log in" hint — it carries a
// permission-oriented hint instead.
func TestFromAPIError_Forbidden_NonLoginHint(t *testing.T) {
	cliErr := FromAPIError(403, &APIError{Message: "nope"}, "")
	if cliErr.Code != "forbidden" {
		t.Errorf("Code = %q, want forbidden", cliErr.Code)
	}
	if cliErr.ExitCode != ExitAuth {
		t.Errorf("ExitCode = %d, want %d", cliErr.ExitCode, ExitAuth)
	}
	if !strings.Contains(cliErr.Hint, "not permitted") {
		t.Errorf("Hint = %q, want a permission hint", cliErr.Hint)
	}
	if strings.Contains(strings.ToLower(cliErr.Hint), "auth login") {
		t.Errorf("Hint = %q, must not suggest logging in for a 403", cliErr.Hint)
	}
}

// An unknown 403 code (not in hintForAPICode) still gets the generic non-login hint.
func TestFromAPIError_Unknown403_GetsGenericForbiddenHint(t *testing.T) {
	cliErr := FromAPIError(403, &APIError{Code: "some_new_403_code", Message: "nope"}, "")
	if cliErr.Code != "some_new_403_code" {
		t.Errorf("Code = %q, want preserved", cliErr.Code)
	}
	if !strings.Contains(cliErr.Hint, "not permitted") {
		t.Errorf("Hint = %q, want generic forbidden hint", cliErr.Hint)
	}
}

func TestFromAPIError_InvalidParam_WithParamName(t *testing.T) {
	param := "avatar_id"
	apiErr := &APIError{Code: "invalid_parameter", Message: "bad value", Param: &param}
	cliErr := FromAPIError(400, apiErr, "")

	if !strings.Contains(cliErr.Hint, "avatar_id") {
		t.Errorf("Hint = %q, want param name in hint", cliErr.Hint)
	}
	if !strings.Contains(cliErr.Hint, "--request-schema") {
		t.Errorf("Hint = %q, want --request-schema in hint", cliErr.Hint)
	}
}

func TestFromAPIError_InvalidParam_NilParam(t *testing.T) {
	apiErr := &APIError{Code: "invalid_parameter", Message: "bad value"}
	cliErr := FromAPIError(400, apiErr, "")

	if strings.Contains(cliErr.Hint, "Invalid field") {
		t.Errorf("Hint = %q, should not mention field when param is nil", cliErr.Hint)
	}
	if !strings.Contains(cliErr.Hint, "--request-schema") {
		t.Errorf("Hint = %q, want --request-schema in hint", cliErr.Hint)
	}
}

func TestConstructors(t *testing.T) {
	t.Run("New", func(t *testing.T) {
		err := New("something broke")
		if err.ExitCode != ExitGeneral {
			t.Errorf("ExitCode = %d, want %d", err.ExitCode, ExitGeneral)
		}
	})

	t.Run("NewAuth", func(t *testing.T) {
		err := NewAuth("no key", "set HEYGEN_API_KEY")
		if err.ExitCode != ExitAuth {
			t.Errorf("ExitCode = %d, want %d", err.ExitCode, ExitAuth)
		}
		if err.Hint != "set HEYGEN_API_KEY" {
			t.Errorf("Hint = %q, want expected value", err.Hint)
		}
	})

	t.Run("NewUsage", func(t *testing.T) {
		err := NewUsage("bad flag")
		if err.ExitCode != ExitUsage {
			t.Errorf("ExitCode = %d, want %d", err.ExitCode, ExitUsage)
		}
	})
}
