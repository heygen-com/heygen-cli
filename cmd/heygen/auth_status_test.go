package main

import (
	"encoding/json"
	"strings"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestAuthStatus_Success(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"user@example.com","username":"demo"}}`,
		},
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
	if !strings.Contains(res.Stderr, `"code":"auth_error"`) {
		t.Fatalf("stderr = %s, want auth_error code", res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"message":"invalid API key"`) {
		t.Fatalf("stderr = %s, want invalid API key message", res.Stderr)
	}
}

// TestAuthStatus_AuthError_AddsHint verifies that a 401 from the API gets the
// actionable auth hint injected by auth_status.go.
func TestAuthStatus_AuthError_AddsHint(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 401,
			Body:       `{"error":{"message":"unauthorized"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "bad-key", "auth", "status")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d", res.ExitCode, clierrors.ExitAuth)
	}
	if !strings.Contains(res.Stderr, "HEYGEN_API_KEY") {
		t.Fatalf("stderr = %s, want HEYGEN_API_KEY in hint", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "heygen auth login") {
		t.Fatalf("stderr = %s, want heygen auth login in hint", res.Stderr)
	}
}

// TestAuthStatus_NonAuthError_NoHintMutation verifies that non-auth errors are
// not mutated by the auth hint injection in auth_status.go.
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
	if !strings.Contains(res.Stderr, "Three ways to provide your API key") {
		t.Fatalf("stderr = %s, want auth guidance", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "app.heygen.com/settings/api") {
		t.Fatalf("stderr = %s, want key URL in hint", res.Stderr)
	}
}
