package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

func writeCredentials(t *testing.T, contents string) string {
	t.Helper()
	dir := paths.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "credentials")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// --- legacy plaintext ------------------------------------------------------

func TestFileCredentialResolver_ReadsKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, "test-key-123\n")

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "test-key-123" {
		t.Fatalf("key = %q, want %q", key, "test-key-123")
	}
}

func TestFileCredentialResolver_MissingFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var notConfigured *ErrNotConfigured
	if !errors.As(err, &notConfigured) {
		t.Fatalf("expected *ErrNotConfigured, got %T", err)
	}
}

func TestFileCredentialResolver_EmptyFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, " \n")

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFileCredentialResolver_WhitespaceHandling(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, "test-key-123  \n")

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "test-key-123" {
		t.Fatalf("key = %q, want %q", key, "test-key-123")
	}
}

// --- new JSON format -------------------------------------------------------

func TestFileCredentialResolver_JSON_APIKeyOnly(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, `{"api_key":"hg_json_abc"}`)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "hg_json_abc" {
		t.Fatalf("key = %q, want hg_json_abc", key)
	}
}

func TestFileCredentialResolver_JSON_OAuthOnly_Fresh(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"oauth":{"access_token":"fresh_at","expires_at":"`+future+`"}}`)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "fresh_at" {
		t.Fatalf("key = %q, want fresh_at", key)
	}
}

func TestFileCredentialResolver_JSON_BothFreshOAuthWins(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"api_key":"hg_fallback","oauth":{"access_token":"fresh_at","expires_at":"`+future+`"}}`)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "fresh_at" {
		t.Fatalf("key = %q, want fresh_at (oauth should win)", key)
	}
}

func TestFileCredentialResolver_JSON_ExpiredOAuthFallsThroughToAPIKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"api_key":"hg_fallback","oauth":{"access_token":"stale_at","expires_at":"`+past+`"}}`)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "hg_fallback" {
		t.Fatalf("key = %q, want hg_fallback (expired oauth should not be used)", key)
	}
}

func TestFileCredentialResolver_JSON_ExpiredOAuthNoAPIKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"oauth":{"access_token":"stale_at","expires_at":"`+past+`"}}`)

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error when only expired oauth is present")
	}
}

func TestFileCredentialResolver_JSON_OAuthWithoutExpiresAtIsAccepted(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	// No expires_at field — be lenient and trust the token. The server
	// will reject if it's actually dead.
	writeCredentials(t, `{"oauth":{"access_token":"at_no_expiry"}}`)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "at_no_expiry" {
		t.Fatalf("key = %q, want at_no_expiry", key)
	}
}

func TestFileCredentialResolver_JSON_InvalidJSON(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, `{this is not valid JSON`)

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}

	// Must NOT be ErrNotConfigured — a broken file is a hard error.
	var notConfigured *ErrNotConfigured
	if errors.As(err, &notConfigured) {
		t.Fatalf("invalid JSON should not surface as ErrNotConfigured, got %T", err)
	}
}

func TestFileCredentialResolver_JSON_EmptyObject(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, `{}`)

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error when JSON has no credential fields")
	}
}

func TestFileCredentialResolver_MultiLineNonJSONIsRejected(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, "line1\nline2\nline3")

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error on multi-line non-JSON garbage")
	}
}

func TestFileCredentialResolver_JSON_UnparseableExpiresAt(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, `{"oauth":{"access_token":"at","expires_at":"not-a-date"}}`)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Unparseable expiry is treated as 'fresh' rather than failing —
	// the server is the source of truth for token validity.
	if key != "at" {
		t.Fatalf("key = %q, want at", key)
	}
}

func TestFileCredentialResolver_JSON_DropsUnknownFields(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, `{"api_key":"hg_x","future_field":{"nested":true}}`)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "hg_x" {
		t.Fatalf("key = %q, want hg_x", key)
	}
}
