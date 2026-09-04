package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/auth/oauth"
	"github.com/heygen-com/heygen-cli/internal/client"
	"github.com/heygen-com/heygen-cli/internal/output"
)

// TestAuthStatus_APIKey_AddsCredentialMeta verifies that the api-key
// happy path still works AND now exposes the credential metadata block.
func TestAuthStatus_APIKey_AddsCredentialMeta(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"u@example.com","username":"demo"}}`,
		},
		"GET /v3/api_keys/self": successfulAPIKeySelfHandler(),
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
	if credMeta["key_name"] != "production automation" {
		t.Errorf("credential.key_name = %v, want production automation", credMeta["key_name"])
	}
	if credMeta["scope_mode"] != "custom" {
		t.Errorf("credential.scope_mode = %v, want custom", credMeta["scope_mode"])
	}
	if credMeta["key_id"] != "key-123" {
		t.Errorf("credential.key_id = %v, want key-123", credMeta["key_id"])
	}
	if _, present := credMeta["user_type"]; present {
		t.Errorf("credential.user_type should not be returned: %v", credMeta["user_type"])
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
	t.Setenv("HEYGEN_API_KEY", "")
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

func TestAuthStatus_OAuthRefreshReportsUpdatedMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_API_KEY", "")
	if err := auth.SaveOAuthTokens(auth.OAuthTokens{
		AccessToken:  "stale_access_token",
		RefreshToken: "old_refresh_token",
		ExpiresAt:    time.Now().Add(-time.Hour),
		Scope:        "openid old_scope",
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}

	var apiAuthorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"fresh_access_token","refresh_token":"new_refresh_token","token_type":"Bearer","expires_in":3600,"scope":"openid refreshed_scope"}`))
		case "/v3/users/me":
			apiAuthorization = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"email":"user@example.com"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cred, err := (&auth.FileCredentialResolver{}).ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	var stdout, stderr bytes.Buffer
	formatter := output.NewJSONFormatter(&stdout, &stderr)
	ctx := &cmdContext{
		client: client.NewWithCredential(
			*cred,
			client.WithBaseURL(srv.URL),
			client.WithOAuthClient(oauth.NewClient(
				oauth.WithTokenURL(srv.URL+"/v1/oauth/token"),
				oauth.WithHTTPClient(srv.Client()),
			)),
		),
		formatter: formatter,
	}

	cmd := newAuthStatusCmd(ctx)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("auth status: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if apiAuthorization != "Bearer fresh_access_token" {
		t.Fatalf("Authorization = %q, want refreshed access token", apiAuthorization)
	}

	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	credMeta := parsed["credential"].(map[string]any)
	if credMeta["scope"] != "openid refreshed_scope" {
		t.Fatalf("credential.scope = %v, want refreshed scope", credMeta["scope"])
	}
	if _, stale := credMeta["expired"]; stale {
		t.Fatalf("credential metadata still reports expired after successful refresh: %v", credMeta)
	}
	expiresAt, err := time.Parse(time.RFC3339, credMeta["expires_at"].(string))
	if err != nil {
		t.Fatalf("credential.expires_at = %v: %v", credMeta["expires_at"], err)
	}
	if time.Until(expiresAt) < 50*time.Minute {
		t.Fatalf("credential.expires_at = %s, want refreshed expiry", expiresAt)
	}
}

func TestAuthStatus_OAuthHumanOutputIncludesAccountAndCredential(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_API_KEY", "")
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
			Body:       `{"data":{"email":"oauth@example.com"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "", "auth", "status", "--human")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	for _, want := range []string{"Data:", "oauth@example.com", "Credential:", "Type", "oauth", "Scope", "openid profile email"} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, res.Stdout)
		}
	}
}

// W3: a /v3/users/me response body of literal `null` decodes to a nil
// map. The merge step must not panic on the credential assignment;
// instead it returns an envelope with just the credential block.
func TestMergeStatusEnvelope_NullBody_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("mergeStatusEnvelope panicked on null body: %v (W3 regression)", r)
		}
	}()
	credMeta := map[string]any{"type": "api_key", "source": "env"}
	out, err := mergeStatusEnvelope([]byte("null"), credMeta)
	if err != nil {
		t.Fatalf("mergeStatusEnvelope: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("merged output not JSON: %v\n%s", err, string(out))
	}
	got, ok := parsed["credential"].(map[string]any)
	if !ok {
		t.Fatalf("expected `credential` block in merged output, got %v", parsed)
	}
	if got["type"] != "api_key" {
		t.Errorf("credential.type = %v, want api_key", got["type"])
	}
}
