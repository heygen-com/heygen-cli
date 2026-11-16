package auth

import (
	"errors"
	"os"
	"testing"
)

func TestNoConfigDirLeavesTheWorkingDirectoryAlone(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HEYGEN_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if err := (&FileCredentialStore{}).Save("sk_test"); err == nil {
		t.Fatal("saving an API key with no config directory succeeded")
	}
	if err := SaveOAuthTokens(OAuthTokens{AccessToken: "a"}); err == nil {
		t.Fatal("saving OAuth tokens with no config directory succeeded")
	}
	if err := SaveUserInfo(UserInfo{Email: "a@b.test"}); err == nil {
		t.Fatal("saving user info with no config directory succeeded")
	}

	var notConfigured *ErrNotConfigured
	if _, err := (&FileCredentialResolver{}).ResolveCredential(); !errors.As(err, &notConfigured) {
		t.Fatalf("ResolveCredential: got %v, want not configured", err)
	}
	if _, err := LoadOAuthTokens(); !errors.As(err, &notConfigured) {
		t.Fatalf("LoadOAuthTokens: got %v, want not configured", err)
	}
	if key, err := LoadAPIKeyFromFile(); key != "" || err != nil {
		t.Fatalf("LoadAPIKeyFromFile: got %q, %v", key, err)
	}
	if _, err := LoadUserInfo(); err != nil {
		t.Fatalf("LoadUserInfo: %v", err)
	}
	if err := ClearOAuthTokens(true); err != nil {
		t.Fatalf("ClearOAuthTokens: %v", err)
	}
	if _, err := ClearAPIKey(); err != nil {
		t.Fatalf("ClearAPIKey: %v", err)
	}
	if err := ClearUserInfo(); err != nil {
		t.Fatalf("ClearUserInfo: %v", err)
	}

	entries, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the working directory gained %d entries, first %q", len(entries), entries[0].Name())
	}
}
