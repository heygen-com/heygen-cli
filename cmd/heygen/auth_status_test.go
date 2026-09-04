package main

import (
	"encoding/json"
	"strings"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func successfulAPIKeySelfHandler() testHandler {
	return testHandler{
		StatusCode: 200,
		Body: `{"data":{"key_name":"production automation","status":"active","scope_mode":"custom","scopes":["video.read"],` +
			`"created_at":"2026-09-01T12:00:00Z","updated_at":"2026-09-02T12:00:00Z","expires_at":"2026-10-01T12:00:00Z",` +
			`"expires_in_seconds":2332800}}`,
	}
}

func TestAuthStatus_Success(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"user@example.com","username":"demo"}}`,
		},
		"GET /v3/api_keys/self": successfulAPIKeySelfHandler(),
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if res.Stderr != "" {
		t.Fatalf("stderr = %q, want empty", res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatalf("data field missing or not object: %v", parsed)
	}
	if data["email"] != "user@example.com" {
		t.Fatalf("data.email = %v, want %q", data["email"], "user@example.com")
	}
}

func TestAuthStatus_APIKeySelfError(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"user@example.com"}}`,
		},
		"GET /v3/api_keys/self": {
			StatusCode: 500,
			Body:       `{"error":{"message":"failed to load API key metadata"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "failed to load API key metadata") {
		t.Fatalf("stderr = %s, want API key metadata error", res.Stderr)
	}
}

func TestAuthStatus_APIKeyHumanOutputIncludesPolicy(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"user@example.com"}}`,
		},
		"GET /v3/api_keys/self": successfulAPIKeySelfHandler(),
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status", "--human")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	for _, want := range []string{"Credential:", "Key Name", "production automation", "Scope Mode", "custom", "video.read", "Expires At"} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, res.Stdout)
		}
	}
}

func TestMergeAPIKeyMetadata_RejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "invalid JSON", raw: "not-json"},
		{name: "missing data", raw: `{"meta":{}}`},
		{name: "null data", raw: `{"data":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := mergeAPIKeyMetadata([]byte(tc.raw), map[string]any{}); err == nil {
				t.Fatal("mergeAPIKeyMetadata() error = nil, want error")
			}
		})
	}
}

func TestAuthStatus_InvalidKey(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 401,
			Body:       `{"error":{"message":"invalid API key"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "invalid-key", "auth", "status")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"code":"unauthorized"`) {
		t.Fatalf("stderr = %s, want unauthorized code", res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"message":"invalid API key"`) {
		t.Fatalf("stderr = %s, want invalid API key message", res.Stderr)
	}
}

// TestAuthStatus_AuthError_AddsHint verifies that a 401 from the API gets the
// source-aware auth hint from centralized enrichment in the error path.
func TestAuthStatus_AuthError_AddsHint(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 401,
			Body:       `{"error":{"message":"unauthorized"}}`,
		},
	})
	defer srv.Close()

	// runCommand passes apiKey via HEYGEN_API_KEY env var, so the hint
	// should reference the environment variable source.
	res := runCommand(t, srv.URL, "bad-key", "auth", "status")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d", res.ExitCode, clierrors.ExitAuth)
	}
	if !strings.Contains(res.Stderr, "HEYGEN_API_KEY environment variable") {
		t.Fatalf("stderr should mention env var source:\n%s", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "app.heygen.com/settings?nav=API") {
		t.Fatalf("stderr should contain key generation URL:\n%s", res.Stderr)
	}
}

// TestAuthStatus_NonAuthError_NoHintMutation verifies that non-auth errors are
// not mutated by the centralized auth hint enrichment.
func TestAuthStatus_NonAuthError_NoHintMutation(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 500,
			Body:       `{"error":{"message":"internal server error"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d", res.ExitCode, clierrors.ExitGeneral)
	}
	// The hint injected by auth_status.go should NOT appear for non-auth errors.
	if strings.Contains(res.Stderr, "heygen auth login") {
		t.Fatalf("stderr = %s, should not contain auth hint for non-auth error", res.Stderr)
	}
}

func TestAuthStatus_NoKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	res := runCommand(t, "http://example.invalid", "", "auth", "status")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"message":"no API key found"`) {
		t.Fatalf("stderr = %s, want missing API key message", res.Stderr)
	}
	// context.go enriches cold-start auth errors with authGuidance.
	if !strings.Contains(res.Stderr, "Three ways to authenticate") {
		t.Fatalf("stderr = %s, want auth guidance", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "app.heygen.com/settings?nav=API") {
		t.Fatalf("stderr = %s, want key URL in hint", res.Stderr)
	}
}
