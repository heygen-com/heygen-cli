package main

import (
	"encoding/json"
	"errors"
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
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
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

	res := runAuthLogoutForTest(t, false, authLogoutConfig{RevokeURL: revoke.URL + "/v1/oauth/revoke"})
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

func TestAuthLogout_PreservesAPIKey(t *testing.T) {
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
	res := runAuthLogoutForTest(t, false, authLogoutConfig{RevokeURL: "http://127.0.0.1:1/revoke"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	// API key block preserved.
	raw, err := os.ReadFile(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), `"hg_keepme"`) {
		t.Errorf("api_key was wiped by logout (default): %s", raw)
	}
	if strings.Contains(string(raw), `"oauth"`) {
		t.Errorf("oauth block still present after logout: %s", raw)
	}
}

func TestAuthLogout_All_AlsoClearsAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	store := &auth.FileCredentialStore{}
	_ = store.Save("hg_clear")
	_ = auth.SaveOAuthTokens(auth.OAuthTokens{AccessToken: "at", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	res := runAuthLogoutForTest(t, true, authLogoutConfig{RevokeURL: "http://127.0.0.1:1/revoke"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	// File should be gone entirely — no api_key + no oauth.
	if _, err := os.Stat(filepath.Join(dir, "credentials")); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(filepath.Join(dir, "credentials"))
		t.Errorf("credentials file still exists after --all: %s", raw)
	}
}

func TestAuthLogout_NoSession_NoOp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	res := runAuthLogoutForTest(t, false, authLogoutConfig{RevokeURL: "http://127.0.0.1:1/revoke"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "No OAuth session") {
		t.Errorf("stderr = %q, want 'No OAuth session'", res.Stderr)
	}
}

func TestAuthRefresh_UsesStoredRefreshToken(t *testing.T) {
	dir := t.TempDir()
	seedOAuthCredentials(t, dir, "stale_at", "rt_seed", time.Now().Add(-time.Hour))

	var seenRefresh string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		seenRefresh = r.PostForm.Get("refresh_token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh_at","refresh_token":"rt_rotated","token_type":"Bearer","expires_in":3600,"scope":"openid"}`))
	}))
	defer idp.Close()

	res := runAuthRefreshForTest(t, authRefreshConfig{TokenURL: idp.URL + "/v1/oauth/token"})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if seenRefresh != "rt_seed" {
		t.Errorf("IdP saw refresh_token = %q, want rt_seed", seenRefresh)
	}

	tok, err := auth.LoadOAuthTokens()
	if err != nil {
		t.Fatalf("LoadOAuthTokens: %v", err)
	}
	if tok.AccessToken != "fresh_at" {
		t.Errorf("access_token = %q, want fresh_at", tok.AccessToken)
	}
	if tok.RefreshToken != "rt_rotated" {
		t.Errorf("refresh_token = %q, want rt_rotated", tok.RefreshToken)
	}

	if !strings.Contains(res.Stderr, "Refreshed") {
		t.Errorf("stderr = %q, want 'Refreshed'", res.Stderr)
	}

	// JSON payload should expose expires_at + scope so callers can pipe.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, res.Stdout)
	}
	if parsed["scope"] != "openid" {
		t.Errorf("stdout.scope = %v, want openid", parsed["scope"])
	}
}

func TestAuthRefresh_NotLoggedIn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	res := runAuthRefreshForTest(t, authRefreshConfig{TokenURL: "http://127.0.0.1:1"})
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3 (auth)", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "heygen auth login") {
		t.Errorf("stderr = %q, want 'heygen auth login' hint", res.Stderr)
	}
}

func TestAuthRefresh_RefreshRejected(t *testing.T) {
	dir := t.TempDir()
	seedOAuthCredentials(t, dir, "stale_at", "rt_bad", time.Now().Add(-time.Hour))

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer idp.Close()

	res := runAuthRefreshForTest(t, authRefreshConfig{TokenURL: idp.URL + "/v1/oauth/token"})
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3 (auth)", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "rejected") {
		t.Errorf("stderr = %q, want 'rejected'", res.Stderr)
	}
}

func runAuthLogoutForTest(t *testing.T, alsoAPIKey bool, cfg authLogoutConfig) cmdResult {
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
		return runAuthLogout(c, ctx, alsoAPIKey, cfg)
	}
	exitCode := 0
	if err := cmd.Execute(); err != nil {
		exitCode = 1
		_, _ = stderr.WriteString(err.Error())
	}
	return cmdResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}

func runAuthRefreshForTest(t *testing.T, cfg authRefreshConfig) cmdResult {
	t.Helper()
	var stdout, stderr strings.Builder
	formatter := formatterForArgs([]string{"auth", "refresh"}, &stdout, &stderr)
	t.Setenv("HEYGEN_NO_ANALYTICS", "1")
	ctx := &cmdContext{formatter: formatter, version: "test"}

	cmd := newAuthRefreshCmd(ctx)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runAuthRefresh(c, ctx, cfg)
	}
	exitCode := 0
	if err := cmd.Execute(); err != nil {
		// Mirror main.go's error rendering: if it's already a CLIError,
		// honor its ExitCode; otherwise classify.
		var cliErr *clierrors.CLIError
		if errors.As(err, &cliErr) {
			formatter.Error(cliErr)
			exitCode = cliErr.ExitCode
		} else {
			wrapped := classifyError(err)
			formatter.Error(wrapped)
			exitCode = wrapped.ExitCode
		}
	}
	return cmdResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}
