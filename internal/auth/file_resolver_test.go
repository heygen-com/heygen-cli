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

// PR 2: an OAuth-only file is now usable via ResolveCredential — the
// resolver surfaces a typed OAuth credential the transport sends as
// `Authorization: Bearer ...`. The legacy string Resolve() still refuses
// it so older string-only call sites never accidentally feed an
// access_token into the x-api-key header.
func TestFileCredentialResolver_JSON_OAuthOnly_TypedIsAccepted(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"oauth":{"access_token":"fresh_at","expires_at":"`+future+`","scope":"openid profile"}}`)

	r := &FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred.Type != CredentialTypeOAuth {
		t.Fatalf("Type = %v, want CredentialTypeOAuth", cred.Type)
	}
	if cred.AccessToken != "fresh_at" {
		t.Fatalf("AccessToken = %q, want fresh_at", cred.AccessToken)
	}
	if cred.Scope != "openid profile" {
		t.Fatalf("Scope = %q, want openid profile", cred.Scope)
	}
	if cred.Source != SourceFile {
		t.Fatalf("Source = %q, want file", cred.Source)
	}

	// String Resolve still refuses — backwards-compat guard.
	if _, err := r.Resolve(); err == nil {
		t.Fatal("expected legacy string Resolve to refuse an OAuth credential")
	}
}

// When both an OAuth session and an api_key are co-located, a fresh
// OAuth credential wins — OAuth is the new default. The api_key is left
// alone on disk and still selectable via the api-key code path.
func TestFileCredentialResolver_JSON_BothPrefersOAuth(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"api_key":"hg_legacy","oauth":{"access_token":"fresh_at","expires_at":"`+future+`"}}`)

	r := &FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred.Type != CredentialTypeOAuth {
		t.Fatalf("Type = %v, want CredentialTypeOAuth (OAuth wins over api_key)", cred.Type)
	}
	if cred.AccessToken != "fresh_at" {
		t.Fatalf("AccessToken = %q, want fresh_at", cred.AccessToken)
	}
}

// Expired access_token + a refresh_token should yield an
// OAuthExpired credential so the transport refreshes before the
// first request.
func TestFileCredentialResolver_JSON_OAuthExpired_HasRefresh(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"oauth":{"access_token":"stale","refresh_token":"rt_keep","expires_at":"`+past+`"}}`)

	r := &FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred.Type != CredentialTypeOAuthExpired {
		t.Fatalf("Type = %v, want CredentialTypeOAuthExpired", cred.Type)
	}
	if cred.RefreshToken != "rt_keep" {
		t.Fatalf("RefreshToken = %q, want rt_keep", cred.RefreshToken)
	}
}

// Expired access_token AND no refresh_token AND no api_key is unusable.
func TestFileCredentialResolver_JSON_OAuthExpired_NoRefresh_NoAPIKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"oauth":{"access_token":"stale","expires_at":"`+past+`"}}`)

	r := &FileCredentialResolver{}
	_, err := r.ResolveCredential()
	if err == nil {
		t.Fatal("expected error when access expired + no refresh + no api_key")
	}
}

// Expired access_token + no refresh_token but a co-located api_key
// should fall back to api_key.
func TestFileCredentialResolver_JSON_OAuthExpired_FallsToAPIKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"api_key":"hg_backup","oauth":{"access_token":"stale","expires_at":"`+past+`"}}`)

	r := &FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred.Type != CredentialTypeAPIKey {
		t.Fatalf("Type = %v, want CredentialTypeAPIKey", cred.Type)
	}
	if cred.APIKey != "hg_backup" {
		t.Fatalf("APIKey = %q, want hg_backup", cred.APIKey)
	}
}

// An OAuth block with no expiry information at all is treated as "no
// information" and the access_token is used optimistically — the
// transport will fall back to refresh-on-401.
func TestFileCredentialResolver_JSON_OAuthNoExpiry(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeCredentials(t, `{"oauth":{"access_token":"at_no_expiry"}}`)

	r := &FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred.Type != CredentialTypeOAuth {
		t.Fatalf("Type = %v, want CredentialTypeOAuth", cred.Type)
	}
	if cred.AccessToken != "at_no_expiry" {
		t.Fatalf("AccessToken = %q, want at_no_expiry", cred.AccessToken)
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
