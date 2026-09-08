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
		Body: `{"data":{"key_id":"key-123","key_name":"production automation","status":"active","scope_mode":"custom","scopes":["videos:read"],` +
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

// A 5xx on the enrichment call is transient and must not brick the command:
// `auth status` still knows which credential is in use and that it authenticates.
// This previously hard-failed; degrading is the deliberate change.
func TestAuthStatus_APIKeySelfServerErrorDegrades(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/api_keys/self": {
			StatusCode: 500,
			Body:       `{"error":{"message":"failed to load API key metadata"}}`,
		},
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"user@example.com"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (5xx should degrade, not fail)\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "/v3/api_keys/self") || !strings.Contains(res.Stderr, "transient") {
		t.Fatalf("stderr = %s, want a transient-server warning naming the endpoint", res.Stderr)
	}
	credMeta := credentialBlock(t, res.Stdout)
	reason, ok := credMeta["permissions_unavailable"].(string)
	if !ok || !strings.Contains(reason, "500") {
		t.Fatalf("credential.permissions_unavailable = %v, want an inline reason naming HTTP 500", credMeta["permissions_unavailable"])
	}
	if _, leaked := credMeta["scope_mode"]; leaked {
		t.Fatalf("scope_mode must be absent when the metadata call failed: %v", credMeta)
	}
}

// 401 is the one class that must stay fatal. Degrading it would render
// "credential present, from local file" for a revoked key — false reassurance
// about the single question `auth status` exists to answer.
func TestAuthStatus_APIKeySelfUnauthorizedStaysFatal(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/api_keys/self": {
			StatusCode: 401,
			Body:       `{"error":{"code":"auth_error","message":"api key is revoked"}}`,
		},
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"user@example.com"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode == 0 {
		t.Fatalf("ExitCode = 0, want non-zero: a rejected credential must fail\nstdout: %s", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "api key is revoked") {
		t.Fatalf("stderr = %s, want the upstream rejection surfaced", res.Stderr)
	}
}

// A 403 degrades like the rest, but must NOT read like the routine
// not-deployed-yet state: /v3/api_keys/self is scope-exempt, so a 403 there is
// an anomaly (exemption regression, or an upstream proxy intercepting).
func TestAuthStatus_APIKeySelfForbiddenDegradesButReadsAsAnomalous(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/api_keys/self": {
			StatusCode: 403,
			Body:       `{"error":{"code":"forbidden","message":"forbidden"}}`,
		},
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"user@example.com"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (403 degrades)\nstderr: %s", res.ExitCode, res.Stderr)
	}
	reason, ok := credentialBlock(t, res.Stdout)["permissions_unavailable"].(string)
	if !ok {
		t.Fatalf("want an inline permissions_unavailable reason, stdout: %s", res.Stdout)
	}
	if !strings.Contains(reason, "unexpected") || !strings.Contains(reason, "no scope grant") {
		t.Fatalf("reason = %q, want it flagged as unexpected for a scope-exempt endpoint", reason)
	}
	// The distinguishing property: it must not read as the 404 case.
	if strings.Contains(reason, "does not support") {
		t.Fatalf("reason = %q, must not render as the routine pre-deploy state", reason)
	}
}

func credentialBlock(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}
	credMeta, ok := parsed["credential"].(map[string]any)
	if !ok {
		t.Fatalf("credential block missing: %v", parsed)
	}
	return credMeta
}

func TestAuthStatus_APIKeySelfNotFoundFallsBack(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/api_keys/self": {
			StatusCode: 404,
			Body:       `{"error":{"code":"not_found","message":"route not found"}}`,
		},
		"GET /v3/users/me": {
			StatusCode: 200,
			Body:       `{"data":{"email":"user@example.com"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"warning"`) || !strings.Contains(res.Stderr, "/v3/api_keys/self") {
		t.Fatalf("stderr = %s, want structured endpoint-unavailable warning", res.Stderr)
	}
	// The calm end of the taxonomy: 404 is the expected pre-deploy state, and
	// must stay distinguishable from the 403 anomaly wording.
	if reason, ok := credentialBlock(t, res.Stdout)["permissions_unavailable"].(string); !ok || !strings.Contains(reason, "does not support") {
		t.Fatalf("credential.permissions_unavailable = %v, want the calm not-deployed-yet reason", reason)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if parsed["data"].(map[string]any)["email"] != "user@example.com" {
		t.Fatalf("legacy account data missing: %v", parsed)
	}
	credMeta, ok := parsed["credential"].(map[string]any)
	if !ok || credMeta["type"] != "api_key" || credMeta["source"] != "env" {
		t.Fatalf("local credential metadata missing: %v", parsed)
	}
}

func TestAuthStatus_CustomAPIKeyWithoutAccountReadStillSucceeds(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/api_keys/self": successfulAPIKeySelfHandler(),
		"GET /v3/users/me": {
			StatusCode: 403,
			Body:       `{"error":{"code":"insufficient_api_key_scope","message":"This API key does not have permission to perform this action. Required scope: account:read."}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	data, ok := parsed["data"].(map[string]any)
	if !ok || len(data) != 0 {
		t.Errorf("data should be an empty object when account.read is unavailable: %v", parsed["data"])
	}
	credMeta, ok := parsed["credential"].(map[string]any)
	if !ok || credMeta["key_name"] != "production automation" {
		t.Fatalf("credential metadata missing: %v", parsed)
	}
}

func TestAuthStatus_APIKeyOtherForbiddenDoesNotDegrade(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/api_keys/self": successfulAPIKeySelfHandler(),
		"GET /v3/users/me": {
			StatusCode: 403,
			Body:       `{"error":{"code":"resource_access_denied","message":"workspace policy denied account access"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "auth", "status")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"code":"resource_access_denied"`) {
		t.Fatalf("stderr = %s, want non-scope 403 to propagate", res.Stderr)
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
	for _, want := range []string{"Data:", "Email", "user@example.com", "Credential:", "Key Name", "production automation", "Scope Mode", "custom", "videos:read", "Expires At"} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, res.Stdout)
		}
	}
}

func TestMergeAPIKeyMetadata_PreservesLocallyOwnedFields(t *testing.T) {
	credMeta := map[string]any{
		"source": "env",
		"type":   "api_key",
		"user":   map[string]any{"email": "local@example.com"},
	}
	raw := json.RawMessage(`{"data":{"source":"server","type":"future_type","user":{"email":"server@example.com"},"key_id":"key-123"}}`)

	if err := mergeAPIKeyMetadata(raw, credMeta); err != nil {
		t.Fatalf("mergeAPIKeyMetadata() error = %v", err)
	}
	if credMeta["source"] != "env" || credMeta["type"] != "api_key" {
		t.Fatalf("locally owned credential fields were overwritten: %v", credMeta)
	}
	user := credMeta["user"].(map[string]any)
	if user["email"] != "local@example.com" {
		t.Fatalf("local user metadata was overwritten: %v", credMeta)
	}
	if credMeta["key_id"] != "key-123" {
		t.Fatalf("unknown backend metadata was not copied through: %v", credMeta)
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
		"GET /v3/api_keys/self": {
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
		"GET /v3/api_keys/self": {
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
		"GET /v3/api_keys/self": successfulAPIKeySelfHandler(),
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
	t.Setenv("HEYGEN_API_KEY", "")

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
