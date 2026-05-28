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

func TestFileCredentialResolver_ReadDoesNotRewriteLegacyFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	const legacy = "legacy-plain-key\n"
	path := writeCredentials(t, legacy)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "legacy-plain-key" {
		t.Fatalf("key = %q, want legacy-plain-key", key)
	}

	// Reads must be side-effect-free: a legacy plaintext file is left
	// untouched (so an older pinned binary run afterward can still read
	// it). Upgrading to JSON happens only on an explicit write (Save).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) != legacy {
		t.Fatalf("read rewrote the file: got %q, want %q", string(raw), legacy)
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
	// An empty (but present) file is broken state, not "not configured".
	// It must surface as a non-ErrNotConfigured error so the chain
	// resolver reports it instead of silently skipping to "no API key".
	var notConfigured *ErrNotConfigured
	if errors.As(err, &notConfigured) {
		t.Fatalf("empty file should not be ErrNotConfigured, got %T", err)
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

// heygen-cli can only transmit an api_key (x-api-key header), so an
// OAuth-only file is unusable and must surface a clear error rather than
// returning the access_token to be mis-sent as an API key.
func TestFileCredentialResolver_JSON_OAuthOnlyIsRejected(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"oauth":{"access_token":"fresh_at","expires_at":"`+future+`"}}`)

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error for an OAuth-only file, got nil")
	}
	var notConfigured *ErrNotConfigured
	if errors.As(err, &notConfigured) {
		t.Fatalf("OAuth-only file should surface a usable error, not ErrNotConfigured: %T", err)
	}
}

// When both are present, api_key wins — it's the only credential
// heygen-cli can actually send.
func TestFileCredentialResolver_JSON_BothPrefersAPIKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"api_key":"hg_use_me","oauth":{"access_token":"fresh_at","expires_at":"`+future+`"}}`)

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "hg_use_me" {
		t.Fatalf("key = %q, want hg_use_me (api_key must win over oauth)", key)
	}
}

func TestFileCredentialResolver_JSON_OAuthOnlyNoExpiryIsRejected(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, `{"oauth":{"access_token":"at_no_expiry"}}`)

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error for an OAuth-only file, got nil")
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
