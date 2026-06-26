package auth

import (
	"encoding/json"
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

// readRawJSON decodes the on-disk credentials file into a generic map so
// tests can assert the presence of keys this CLI doesn't model (which the
// typed jsonCredentials view deliberately hides on its `extra` field).
func readRawJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("on-disk file is not valid JSON: %v\ncontents: %s", err, data)
	}
	return out
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

// TestResolveCredential_FileAPIKeyBeatsFileOAuth: when both an OAuth
// session and an api_key are co-located, api_key wins. heygen-cli is
// agent-first; the api_key path is the dominant use case. Going forward
// the login runner clears the other block on login, so this scenario
// mostly matters for pre-this-change users who still have a stale
// OAuth block co-located with the api_key they re-saved.
func TestResolveCredential_FileAPIKeyBeatsFileOAuth(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	writeCredentials(t, `{"api_key":"hg_winner","oauth":{"access_token":"fresh_at","expires_at":"`+future+`"}}`)

	r := &FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred.Type != CredentialTypeAPIKey {
		t.Fatalf("Type = %v, want CredentialTypeAPIKey (api_key wins over OAuth in file)", cred.Type)
	}
	if cred.APIKey != "hg_winner" {
		t.Fatalf("APIKey = %q, want hg_winner", cred.APIKey)
	}

	// Legacy string Resolve() also returns the api_key — no more
	// "OAuth-aware transport required" refusal when api_key is the
	// selected credential.
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "hg_winner" {
		t.Fatalf("Resolve key = %q, want hg_winner", key)
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
// resolves to api_key. (Under the api-key-first precedence the api_key
// is selected directly before the OAuth-expiry branch ever runs, but
// we keep this test so the "expired oauth + api_key" combination has
// explicit coverage either way.)
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

// Cross-CLI forward compatibility: the credentials file is SHARED with
// the Node hyperframes CLI. heygen-cli must NOT strip top-level / nested
// keys it doesn't model when it rewrites the file, or it silently
// destroys data the other CLI wrote (and vice versa). The reader stashes
// unrecognized keys and the writer re-emits them verbatim. This mirrors
// the Node-side hardening in hyperframes#1741.
func TestFileCredentialResolver_JSON_PreservesUnknownFields(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := writeCredentials(t, `{"api_key":"hg_x","future_field":{"nested":true}}`)

	// Reading still resolves the known credential cleanly.
	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "hg_x" {
		t.Fatalf("key = %q, want hg_x", key)
	}

	// And the unknown key is captured on the parsed value so the next
	// write round-trips it instead of dropping it.
	parsed, _, err := loadCredentialsFile(path)
	if err != nil {
		t.Fatalf("loadCredentialsFile: %v", err)
	}
	if _, ok := parsed.extra["future_field"]; !ok {
		t.Fatalf("future_field not captured for round-trip, extra = %v", parsed.extra)
	}

	// Rewrite the file (here: re-save the same parsed value). The
	// unknown key must survive on disk.
	if err := writeCredentialsFile(path, parsed); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}
	onDisk := readRawJSON(t, path)
	if onDisk["api_key"] != "hg_x" {
		t.Fatalf("api_key = %v, want hg_x", onDisk["api_key"])
	}
	ff, ok := onDisk["future_field"].(map[string]any)
	if !ok || ff["nested"] != true {
		t.Fatalf("future_field not preserved on round-trip: %v", onDisk["future_field"])
	}
}

// An unknown key INSIDE the oauth sub-object (e.g. a future id_token the
// hyperframes CLI starts writing) must survive a heygen-cli round-trip.
func TestFileCredentialResolver_JSON_PreservesUnknownOAuthSubKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := writeCredentials(t, `{"oauth":{"access_token":"at_1","id_token":"future_id_token_value"}}`)

	parsed, _, err := loadCredentialsFile(path)
	if err != nil {
		t.Fatalf("loadCredentialsFile: %v", err)
	}
	if err := writeCredentialsFile(path, parsed); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	onDisk := readRawJSON(t, path)
	oauth, ok := onDisk["oauth"].(map[string]any)
	if !ok {
		t.Fatalf("oauth block missing after round-trip: %v", onDisk)
	}
	if oauth["access_token"] != "at_1" {
		t.Fatalf("oauth.access_token = %v, want at_1", oauth["access_token"])
	}
	if oauth["id_token"] != "future_id_token_value" {
		t.Fatalf("oauth.id_token not preserved: %v", oauth["id_token"])
	}
}

// An unknown key INSIDE the user sub-object (e.g. a future avatar_url)
// must survive a heygen-cli round-trip.
func TestFileCredentialResolver_JSON_PreservesUnknownUserSubKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := writeCredentials(t, `{"api_key":"hg_x","user":{"email":"u@example.com","avatar_url":"https://cdn/x.png"}}`)

	parsed, _, err := loadCredentialsFile(path)
	if err != nil {
		t.Fatalf("loadCredentialsFile: %v", err)
	}
	if err := writeCredentialsFile(path, parsed); err != nil {
		t.Fatalf("writeCredentialsFile: %v", err)
	}

	onDisk := readRawJSON(t, path)
	user, ok := onDisk["user"].(map[string]any)
	if !ok {
		t.Fatalf("user block missing after round-trip: %v", onDisk)
	}
	if user["email"] != "u@example.com" {
		t.Fatalf("user.email = %v, want u@example.com", user["email"])
	}
	if user["avatar_url"] != "https://cdn/x.png" {
		t.Fatalf("user.avatar_url not preserved: %v", user["avatar_url"])
	}
}
