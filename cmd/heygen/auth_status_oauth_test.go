package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/auth"
)

// TestAuthStatus_APIKey_AddsCredentialMeta verifies that the api-key
// happy path still works AND now exposes the credential metadata block.
func TestAuthStatus_APIKey_AddsCredentialMeta(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"u@example.com","username":"demo"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, res.Stdout)
	}
	credMeta, ok := parsed["credential"].(map[string]any)
	if !ok {
		t.Fatalf("expected `credential` block, got %v", parsed)
	}
	if credMeta["type"] != "api_key" {
		t.Errorf("credential.type = %v, want api_key", credMeta["type"])
	}
	if credMeta["source"] != "env" {
		t.Errorf("credential.source = %v, want env", credMeta["source"])
	}
	// Data field still present + unchanged.
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatalf("data block missing/wrong shape: %v", parsed)
	}
	if data["email"] != "u@example.com" {
		t.Errorf("data.email = %v, want u@example.com", data["email"])
	}
}

// TestAuthStatus_OAuth_ReportsExpiryAndScope verifies the OAuth path:
// a credential on disk with an OAuth block produces a credential block
// containing type:oauth + expiry/scope.
func TestAuthStatus_OAuth_ReportsExpiryAndScope(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	// runCommand sets HEYGEN_API_KEY when non-empty — leave it blank so
	// the file path wins.
	if err := auth.SaveOAuthTokens(auth.OAuthTokens{
		AccessToken:  "at_fresh",
		RefreshToken: "rt_for_refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scope:        "openid profile email",
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}

	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"username":"demo"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "", "auth", "status")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s\nstdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, res.Stdout)
	}
	credMeta, ok := parsed["credential"].(map[string]any)
	if !ok {
		t.Fatalf("expected `credential` block, got %v", parsed)
	}
	if credMeta["type"] != "oauth" {
		t.Errorf("credential.type = %v, want oauth", credMeta["type"])
	}
	if credMeta["source"] != "file" {
		t.Errorf("credential.source = %v, want file", credMeta["source"])
	}
	if credMeta["refreshable"] != true {
		t.Errorf("credential.refreshable = %v, want true", credMeta["refreshable"])
	}
	if !strings.Contains(credMeta["scope"].(string), "openid") {
		t.Errorf("credential.scope = %v, want includes openid", credMeta["scope"])
	}
	if _, ok := credMeta["expires_at"].(string); !ok {
		t.Errorf("credential.expires_at missing or wrong type: %v", credMeta)
	}
}
