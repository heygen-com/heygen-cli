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

func TestAuthStatus_NoKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	res := runCommand(t, "http://example.invalid", "", "auth", "status")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"message":"no API key found"`) {
		t.Fatalf("stderr = %s, want missing API key message", res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"hint":"Set HEYGEN_API_KEY env var or run: heygen auth login"`) {
		t.Fatalf("stderr = %s, want auth hint", res.Stderr)
	}
}
