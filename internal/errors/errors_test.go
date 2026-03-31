package errors

import (
	"encoding/json"
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
