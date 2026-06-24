package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/auth/oauth"
	"github.com/spf13/cobra"
)

// fakeIdP serves a minimal subset of the HeyGen OAuth endpoints for the
// PR 2 login integration test. It accepts a single authorization_code
// exchange and returns a fixed token response.
type fakeIdP struct {
	server         *httptest.Server
	expectedCode   string
	expectedClient string
	tokenResponse  string
	tokenStatus    int
	gotForm        url.Values
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	idp := &fakeIdP{
		expectedCode:   "test_code_abc",
		expectedClient: oauth.DefaultClientID,
		tokenStatus:    http.StatusOK,
		tokenResponse:  `{"access_token":"at_minted","refresh_token":"rt_minted","token_type":"Bearer","expires_in":3600,"scope":"openid profile"}`,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		idp.gotForm = r.PostForm
		if r.PostForm.Get("grant_type") != "authorization_code" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("code") != idp.expectedCode {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("client_id") != idp.expectedClient {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(idp.tokenStatus)
		_, _ = w.Write([]byte(idp.tokenResponse))
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// fakeUsersMeServer returns the /v3/users/me payload used to populate
// the "Logged in as ..." line.
func fakeUsersMeServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/users/me" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// hitBrowserCallback is the "browser" — we simulate the IdP redirecting
// the user back to 127.0.0.1:<port>/oauth/callback with the captured
// state + a canned code. The test inspects authURL to extract the state
// + redirect_uri so the callback round-trips correctly.
func hitBrowserCallback(t *testing.T, code, authURL string) {
	t.Helper()
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authURL: %v", err)
	}
	q := parsed.Query()
	redirectURI := q.Get("redirect_uri")
	state := q.Get("state")
	if redirectURI == "" || state == "" {
		t.Fatalf("authURL missing redirect_uri/state: %s", authURL)
	}
	callbackURL := redirectURI + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	req, err := http.NewRequest("GET", callbackURL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	// Use a fresh client so we don't share idle keep-alive sockets with
	// the test's other servers.
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("hit callback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback returned %d", resp.StatusCode)
	}
}

// TestAuthLogin_OAuthFlow_PersistsTokens is the end-to-end integration
// test for the loopback ← callback ← exchange chain. It uses an
// httptest fake IdP for token exchange and an httptest /v3/users/me for
// the identity probe.
func TestAuthLogin_OAuthFlow_PersistsTokens(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)
	t.Setenv("HEYGEN_API_KEY", "")

	idp := newFakeIdP(t)
	usersMe := fakeUsersMeServer(t, `{"data":{"username":"demo","email":"demo@example.com"}}`)

	// Stub the browser: instead of spawning the OS browser, we synthesize
	// the redirect ourselves. The login command will block on the
	// loopback callback channel, so we kick off the synthesized callback
	// in a goroutine.
	cfg := oauthLoginConfig{
		AuthorizeURL: "http://127.0.0.1:1/oauth/authorize", // unused — we never visit it
		TokenURL:     idp.server.URL + "/v1/oauth/token",
		OpenBrowser: func(authURL string) error {
			go func() {
				// Tiny delay so the loopback server's Accept() loop is
				// up before we connect.
				time.Sleep(50 * time.Millisecond)
				hitBrowserCallback(t, idp.expectedCode, authURL)
			}()
			return nil
		},
		UsersMeBaseURL: usersMe.URL,
		Now:            time.Now,
	}

	res := runOAuthLoginForTest(t, cfg)
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s\nstdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}

	// Token block must be persisted with the IdP's response.
	tok, err := auth.LoadOAuthTokens()
	if err != nil {
		t.Fatalf("LoadOAuthTokens: %v", err)
	}
	if tok.AccessToken != "at_minted" {
		t.Errorf("access_token = %q, want at_minted", tok.AccessToken)
	}
	if tok.RefreshToken != "rt_minted" {
		t.Errorf("refresh_token = %q, want rt_minted", tok.RefreshToken)
	}
	if tok.Scope != "openid profile" {
		t.Errorf("scope = %q, want %q", tok.Scope, "openid profile")
	}
	if time.Until(tok.ExpiresAt) <= 0 {
		t.Errorf("expires_at = %v, want future", tok.ExpiresAt)
	}

	// File permissions must be 0600 on Unix (same as api-key path).
	// Windows NTFS does not honor POSIX permission bits, so the assert
	// only applies elsewhere.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(configDir, "credentials"))
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("perms = %o, want 0600", perm)
		}
	}

	// Stderr should mention "Logged in as demo" (from /v3/users/me).
	if !strings.Contains(res.Stderr, "Logged in as demo") {
		t.Errorf("stderr = %q, want 'Logged in as demo'", res.Stderr)
	}

	// Stdout JSON should expose the credential metadata so callers can
	// pipe to jq.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, res.Stdout)
	}
	if parsed["scope"] != "openid profile" {
		t.Errorf("stdout.scope = %v, want %q", parsed["scope"], "openid profile")
	}
}

// TestAuthLogin_OAuthFlow_TokenExchangeRejected: the IdP returns 400
// on the exchange step. The CLI surfaces it as a clear error and does
// NOT persist anything.
func TestAuthLogin_OAuthFlow_TokenExchangeRejected(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)
	t.Setenv("HEYGEN_API_KEY", "")

	idp := newFakeIdP(t)
	idp.tokenStatus = http.StatusBadRequest
	idp.tokenResponse = `{"error":"invalid_grant"}`

	cfg := oauthLoginConfig{
		TokenURL: idp.server.URL + "/v1/oauth/token",
		OpenBrowser: func(authURL string) error {
			go func() {
				time.Sleep(50 * time.Millisecond)
				hitBrowserCallback(t, idp.expectedCode, authURL)
			}()
			return nil
		},
	}
	res := runOAuthLoginForTest(t, cfg)
	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit, got 0\nstderr: %s\nstdout: %s", res.Stderr, res.Stdout)
	}
	if _, err := auth.LoadOAuthTokens(); err == nil {
		t.Error("tokens were persisted despite exchange failure")
	}
}

// TestAuthLogin_APIKeyFlag_StillSavesKey is a regression test for the
// --api-key flag path now that browser OAuth is the default.
func TestAuthLogin_APIKeyFlag_StillSavesKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("hg_xyz\n"),
		"auth", "login", "--api-key")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	data, err := os.ReadFile(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"hg_xyz"`) {
		t.Fatalf("creds file = %s, want hg_xyz", string(data))
	}
}

// TestAuthLogin_DeviceCode_NotYetSupported guards the placeholder flag.
func TestAuthLogin_DeviceCode_NotYetSupported(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader(""),
		"auth", "login", "--device-code")
	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2", res.ExitCode)
	}
	if !strings.Contains(res.Stderr, "not yet supported") {
		t.Fatalf("stderr = %q, want 'not yet supported'", res.Stderr)
	}
}

// runOAuthLoginForTest builds the auth login command directly with a
// pinned oauthLoginConfig — runCommand can't pass per-command config
// hooks, so we drop down a level here.
func runOAuthLoginForTest(t *testing.T, cfg oauthLoginConfig) cmdResult {
	t.Helper()

	var stdout, stderr strings.Builder
	formatter := formatterForArgs([]string{"auth", "login"}, &stdout, &stderr)

	if os.Getenv("HEYGEN_CONFIG_DIR") == "" {
		t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	}
	t.Setenv("HEYGEN_NO_ANALYTICS", "1")
	t.Setenv("HEYGEN_NO_BROWSER", "1") // belt + braces; OpenBrowser is stubbed via cfg anyway

	ctx := &cmdContext{
		formatter:      formatter,
		version:        "test",
		configProvider: nil,
		client:         nil,
	}
	_ = analytics.New("test", false)

	cmd := newAuthLoginCmd(ctx)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	// Inject our test config by calling runOAuthLogin directly through
	// a tweaked default. This keeps the production command unchanged
	// while letting the test pin endpoints.
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runOAuthLogin(c, ctx, cfg)
	}

	exitCode := 0
	if err := cmd.Execute(); err != nil {
		exitCode = 1
		_, _ = stderr.WriteString(err.Error())
	}
	return cmdResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}
