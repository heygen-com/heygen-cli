package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/spf13/cobra"
)

// seedOAuthCredentials writes a credentials file with an OAuth block
// for tests that need a pre-existing logged-in state.
func seedOAuthCredentials(t *testing.T, dir, accessToken, refreshToken string, expiresAt time.Time) {
	t.Helper()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	if err := auth.SaveOAuthTokens(auth.OAuthTokens{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		Scope:        "openid profile",
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}
}

func TestAuthLogout_ClearsOAuthBlock(t *testing.T) {
	dir := t.TempDir()
	seedOAuthCredentials(t, dir, "at", "rt", time.Now().Add(time.Hour))

	// Best-effort revoke endpoint records hits but always returns 200.
	var hits int32
	revoke := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer revoke.Close()

	res := runAuthLogoutForTest(t, authLogoutConfig{RevokeURL: revoke.URL + "/v1/oauth/revoke"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("revoke hits = %d, want 1 (best-effort revoke)", hits)
	}
	if _, err := auth.LoadOAuthTokens(); err == nil {
		t.Errorf("OAuth tokens still present after logout")
	}
	if !strings.Contains(res.Stderr, "Logged out") {
		t.Errorf("stderr = %q, want 'Logged out'", res.Stderr)
	}
}

// TestAuthLogout_ClearsBothBlocks: with the single-credential
// normalization invariant a fresh post-this-change file holds only one
// of api_key / oauth, but logout still has to be safe against
// pre-this-change files that hold both. Both blocks must be cleared and
// the file removed.
func TestAuthLogout_ClearsBothBlocks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	store := &auth.FileCredentialStore{}
	if err := store.Save("hg_keepme"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := auth.SaveOAuthTokens(auth.OAuthTokens{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}

	// Pin revoke to a dead endpoint — best-effort means it still
	// succeeds locally.
	res := runAuthLogoutForTest(t, authLogoutConfig{RevokeURL: "http://127.0.0.1:1/revoke"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	// File should be gone entirely — no api_key + no oauth.
	if _, err := os.Stat(filepath.Join(dir, "credentials")); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(filepath.Join(dir, "credentials"))
		t.Errorf("credentials file still exists after logout: %s", raw)
	}

	// JSON envelope reports cleared_credential=both for pre-existing
	// dual-block files.
	if !strings.Contains(res.Stdout, `"cleared_credential":"both"`) {
		t.Errorf("stdout = %q, want cleared_credential=both", res.Stdout)
	}
}

func TestAuthLogout_ClearsAPIKeyOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	store := &auth.FileCredentialStore{}
	if err := store.Save("hg_clear"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	res := runAuthLogoutForTest(t, authLogoutConfig{RevokeURL: "http://127.0.0.1:1/revoke"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials")); !os.IsNotExist(err) {
		t.Errorf("credentials file still exists after api-key-only logout")
	}
	if !strings.Contains(res.Stderr, "Cleared stored API key") {
		t.Errorf("stderr = %q, want 'Cleared stored API key'", res.Stderr)
	}
	if !strings.Contains(res.Stdout, `"cleared_credential":"api_key"`) {
		t.Errorf("stdout = %q, want cleared_credential=api_key", res.Stdout)
	}
}

func TestAuthLogout_NoSession_NoOp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	res := runAuthLogoutForTest(t, authLogoutConfig{RevokeURL: "http://127.0.0.1:1/revoke"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "No stored credentials") {
		t.Errorf("stderr = %q, want 'No stored credentials'", res.Stderr)
	}
}

func runAuthLogoutForTest(t *testing.T, cfg authLogoutConfig) cmdResult {
	t.Helper()
	var stdout, stderr strings.Builder
	formatter := formatterForArgs([]string{"auth", "logout"}, &stdout, &stderr)
	t.Setenv("HEYGEN_NO_ANALYTICS", "1")
	ctx := &cmdContext{formatter: formatter, version: "test"}
	_ = analytics.New("test", false)

	cmd := newAuthLogoutCmd(ctx)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runAuthLogout(c, ctx, cfg)
	}
	exitCode := 0
	if err := cmd.Execute(); err != nil {
		exitCode = 1
		_, _ = stderr.WriteString(err.Error())
	}
	return cmdResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}
