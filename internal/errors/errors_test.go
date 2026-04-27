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
	if _, ok := inner["retryable"]; ok {
		t.Error("retryable should be omitted when nil")
	}
}

func TestFromAPIError_ExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantExit   int
		wantCode   string
	}{
		{"401 → auth", 401, ExitAuth, "auth_error"},
		{"403 → auth", 403, ExitAuth, "auth_error"},
		{"400 → general", 400, ExitGeneral, "validation_error"},
		{"404 → general", 404, ExitGeneral, "not_found"},
		{"429 → general", 429, ExitGeneral, "rate_limited"},
		{"500 → general", 500, ExitGeneral, "server_error"},
		{"502 → general", 502, ExitGeneral, "server_error"},
		{"503 → general", 503, ExitGeneral, "server_error"},
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

func TestFromAPIError_KnownPermanentCode_NotRetryable(t *testing.T) {
	apiErr := &APIError{Code: "video_not_found", Message: "video not found"}
	cliErr := FromAPIError(404, apiErr, "")

	if cliErr.Retryable == nil {
		t.Fatal("Retryable should be non-nil for video_not_found")
	}
	if *cliErr.Retryable != false {
		t.Errorf("Retryable = %v, want false", *cliErr.Retryable)
	}
}

func TestFromAPIError_500_Retryable(t *testing.T) {
	apiErr := &APIError{Message: "internal server error"}
	cliErr := FromAPIError(500, apiErr, "")

	if cliErr.Retryable == nil {
		t.Fatal("Retryable should be non-nil for 500")
	}
	if *cliErr.Retryable != true {
		t.Errorf("Retryable = %v, want true", *cliErr.Retryable)
	}
}

func TestFromAPIError_429_Retryable(t *testing.T) {
	apiErr := &APIError{Message: "too many requests"}
	cliErr := FromAPIError(429, apiErr, "")

	if cliErr.Retryable == nil {
		t.Fatal("Retryable should be non-nil for 429")
	}
	if *cliErr.Retryable != true {
		t.Errorf("Retryable = %v, want true", *cliErr.Retryable)
	}
}

func TestFromAPIError_400_RetryableNil(t *testing.T) {
	apiErr := &APIError{Code: "validation_error", Message: "bad request"}
	cliErr := FromAPIError(400, apiErr, "")

	if cliErr.Retryable != nil {
		t.Errorf("Retryable = %v, want nil for generic 400", *cliErr.Retryable)
	}
}

func TestFromAPIError_404_UnknownCode_RetryableNil(t *testing.T) {
	apiErr := &APIError{Code: "something_unusual", Message: "not found"}
	cliErr := FromAPIError(404, apiErr, "")

	if cliErr.Retryable != nil {
		t.Errorf("Retryable = %v, want nil for unknown 404 code", *cliErr.Retryable)
	}
}

func TestFromAPIError_GenericEmpty404_RetryableNil(t *testing.T) {
	apiErr := &APIError{Message: "not found"}
	cliErr := FromAPIError(404, apiErr, "")

	if cliErr.Retryable != nil {
		t.Errorf("Retryable = %v, want nil for generic 404 without API code", *cliErr.Retryable)
	}
	if strings.Contains(cliErr.Hint, "unlikely to help") {
		t.Errorf("Hint = %q, generic 404 should not contain permanence language", cliErr.Hint)
	}
}

func TestFromAPIError_AuthError_NotRetryable(t *testing.T) {
	apiErr := &APIError{Code: "forbidden", Message: "access denied"}
	cliErr := FromAPIError(403, apiErr, "")

	if cliErr.Retryable == nil {
		t.Fatal("Retryable should be non-nil for auth errors")
	}
	if *cliErr.Retryable != false {
		t.Errorf("Retryable = %v, want false for auth errors", *cliErr.Retryable)
	}
}

func TestToErrorEnvelope_IncludesRetryable(t *testing.T) {
	retryable := false
	err := &CLIError{Code: "video_not_found", Message: "not found", Retryable: &retryable}
	envelope := err.ToErrorEnvelope()

	data, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		t.Fatalf("failed to marshal envelope: %v", marshalErr)
	}
	if !strings.Contains(string(data), `"retryable":false`) {
		t.Errorf("envelope = %s, want retryable:false", string(data))
	}
}

func TestToErrorEnvelope_OmitsRetryableWhenNil(t *testing.T) {
	err := &CLIError{Code: "error", Message: "fail"}
	envelope := err.ToErrorEnvelope()

	data, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		t.Fatalf("failed to marshal envelope: %v", marshalErr)
	}
	if strings.Contains(string(data), "retryable") {
		t.Errorf("envelope = %s, should not contain retryable when nil", string(data))
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
		if err.Retryable == nil || *err.Retryable != false {
			t.Errorf("Retryable = %v, want false for auth errors", err.Retryable)
		}
	})

	t.Run("NewUsage", func(t *testing.T) {
		err := NewUsage("bad flag")
		if err.ExitCode != ExitUsage {
			t.Errorf("ExitCode = %d, want %d", err.ExitCode, ExitUsage)
		}
	})
}
