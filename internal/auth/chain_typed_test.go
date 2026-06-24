package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveTypedCredential_EnvWinsOverFileOAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_API_KEY", "env-key-xyz")

	// Seed a fresh OAuth credential on disk.
	if err := SaveOAuthTokens(OAuthTokens{
		AccessToken: "at_unused",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}

	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&EnvCredentialResolver{},
			&FileCredentialResolver{},
		},
	}
	cred, err := r.ResolveTypedCredential()
	if err != nil {
		t.Fatalf("ResolveTypedCredential: %v", err)
	}
	if cred.Type != CredentialTypeAPIKey {
		t.Fatalf("Type = %v, want CredentialTypeAPIKey (env must beat file)", cred.Type)
	}
	if cred.APIKey != "env-key-xyz" {
		t.Errorf("APIKey = %q, want env-key-xyz", cred.APIKey)
	}
	if cred.Source != SourceEnv {
		t.Errorf("Source = %q, want env", cred.Source)
	}
}

func TestResolveTypedCredential_FileOAuthWhenEnvUnset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_API_KEY", "")

	if err := SaveOAuthTokens(OAuthTokens{
		AccessToken: "at_use_me",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "openid",
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}

	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&EnvCredentialResolver{},
			&FileCredentialResolver{},
		},
	}
	cred, err := r.ResolveTypedCredential()
	if err != nil {
		t.Fatalf("ResolveTypedCredential: %v", err)
	}
	if cred.Type != CredentialTypeOAuth {
		t.Fatalf("Type = %v, want CredentialTypeOAuth", cred.Type)
	}
	if cred.AccessToken != "at_use_me" {
		t.Errorf("AccessToken = %q, want at_use_me", cred.AccessToken)
	}
	if cred.Source != SourceFile {
		t.Errorf("Source = %q, want file", cred.Source)
	}
}

func TestResolveTypedCredential_FileExpiredFlowsAsOAuthExpired(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_API_KEY", "")

	// Seed a past expiry + refresh_token.
	past := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	if err := os.WriteFile(
		filepath.Join(dir, "credentials"),
		[]byte(`{"oauth":{"access_token":"old","refresh_token":"rt","expires_at":"`+past+`"}}`),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&EnvCredentialResolver{},
			&FileCredentialResolver{},
		},
	}
	cred, err := r.ResolveTypedCredential()
	if err != nil {
		t.Fatalf("ResolveTypedCredential: %v", err)
	}
	if cred.Type != CredentialTypeOAuthExpired {
		t.Fatalf("Type = %v, want CredentialTypeOAuthExpired", cred.Type)
	}
	if cred.RefreshToken != "rt" {
		t.Errorf("RefreshToken = %q, want rt", cred.RefreshToken)
	}
}

func TestResolveTypedCredential_NoCredentialsReturnsAuthError(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv("HEYGEN_API_KEY", "")

	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&EnvCredentialResolver{},
			&FileCredentialResolver{},
		},
	}
	if _, err := r.ResolveTypedCredential(); err == nil {
		t.Fatal("expected error when no credentials configured")
	}
}

// SaveOAuthTokens persists + LoadOAuthTokens reads back the same shape.
func TestOAuthStore_RoundTrip(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	in := OAuthTokens{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second).UTC(),
		Scope:        "openid email",
		TokenType:    "Bearer",
	}
	if err := SaveOAuthTokens(in); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}

	out, err := LoadOAuthTokens()
	if err != nil {
		t.Fatalf("LoadOAuthTokens: %v", err)
	}
	if out.AccessToken != in.AccessToken {
		t.Errorf("AccessToken = %q, want %q", out.AccessToken, in.AccessToken)
	}
	if out.RefreshToken != in.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", out.RefreshToken, in.RefreshToken)
	}
	if !out.ExpiresAt.Equal(in.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", out.ExpiresAt, in.ExpiresAt)
	}
	if out.Scope != in.Scope {
		t.Errorf("Scope = %q, want %q", out.Scope, in.Scope)
	}
}

// ClearOAuthTokens without --all preserves api_key.
func TestOAuthStore_ClearPreservesAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	store := &FileCredentialStore{}
	if err := store.Save("hg_keep"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := SaveOAuthTokens(OAuthTokens{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}
	if err := ClearOAuthTokens(false); err != nil {
		t.Fatalf("ClearOAuthTokens: %v", err)
	}

	// api_key still present, oauth gone.
	r := &FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred.Type != CredentialTypeAPIKey || cred.APIKey != "hg_keep" {
		t.Errorf("after clear: cred = %+v, want APIKey hg_keep", cred)
	}
}

// ClearOAuthTokens with --all removes file when api_key also wiped.
func TestOAuthStore_ClearAllRemovesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	if err := SaveOAuthTokens(OAuthTokens{AccessToken: "at", RefreshToken: "rt"}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}
	if err := ClearOAuthTokens(true); err != nil {
		t.Fatalf("ClearOAuthTokens: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials")); !os.IsNotExist(err) {
		t.Errorf("credentials file still exists after Clear(true): err=%v", err)
	}
}
