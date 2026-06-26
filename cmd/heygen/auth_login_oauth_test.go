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
	"github.com/heygen-com/heygen-cli/internal/config"
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

// TestAuthLogin_APIKeyFlow_ClearsPreviousOAuth verifies the
// single-credential normalization invariant on the api-key path:
// when an OAuth block is already on disk (pre-this-change user, or
// a user who logged in via OAuth previously), running
// `heygen auth login --api-key <newkey>` must drop the OAuth block
// so the file holds at most one of api_key / oauth.
func TestAuthLogin_APIKeyFlow_ClearsPreviousOAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_API_KEY", "")

	// Seed an OAuth session from a prior login.
	if err := auth.SaveOAuthTokens(auth.OAuthTokens{
		AccessToken:  "at_old",
		RefreshToken: "rt_old",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scope:        "openid profile",
	}); err != nil {
		t.Fatalf("seed SaveOAuthTokens: %v", err)
	}

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("hg_replacement\n"),
		"auth", "login", "--api-key")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s\nstdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), `"hg_replacement"`) {
		t.Fatalf("api_key was not saved: %s", raw)
	}
	if strings.Contains(string(raw), `"oauth"`) {
		t.Fatalf("OAuth block survived api-key login (single-credential invariant violated):\n%s", raw)
	}
	// Stdout JSON should report cleared_oauth=true so callers see the
	// normalization happened.
	if !strings.Contains(res.Stdout, `"cleared_oauth":true`) {
		t.Errorf("stdout = %q, want cleared_oauth=true", res.Stdout)
	}
}

// TestAuthLogin_APIKeyFlow_NoPriorOAuth_NoMention: when there's no
// OAuth block to clear, the success message must NOT mention OAuth.
// Keeps the happy path quiet for users who only ever used api_key.
func TestAuthLogin_APIKeyFlow_NoPriorOAuth_NoMention(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("hg_first\n"),
		"auth", "login", "--api-key")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if strings.Contains(res.Stdout, "OAuth") || strings.Contains(res.Stdout, "oauth") {
		// The boolean field "cleared_oauth":false is fine; we want to
		// ensure the human-readable message doesn't ramble about
		// OAuth when there was nothing to clear.
		if !strings.Contains(res.Stdout, `"cleared_oauth":false`) ||
			strings.Contains(res.Stdout, "cleared previously-stored OAuth") {
			t.Errorf("stdout mentions OAuth for an OAuth-less first login: %s", res.Stdout)
		}
	}
}

// TestAuthLogin_OAuthFlow_ClearsPreviousAPIKey verifies the same
// invariant on the OAuth path: a successful browser login drops any
// co-located api_key block. Pre-this-change users with both blocks
// self-heal on their next login.
func TestAuthLogin_OAuthFlow_ClearsPreviousAPIKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)
	t.Setenv("HEYGEN_API_KEY", "")

	// Seed an api_key from a prior login.
	store := &auth.FileCredentialStore{}
	if err := store.Save("hg_legacy"); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	idp := newFakeIdP(t)
	usersMe := fakeUsersMeServer(t, `{"data":{"username":"demo","email":"demo@example.com"}}`)
	cfg := oauthLoginConfig{
		TokenURL: idp.server.URL + "/v1/oauth/token",
		OpenBrowser: func(authURL string) error {
			go func() {
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

	raw, err := os.ReadFile(filepath.Join(configDir, "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), `"hg_legacy"`) {
		t.Fatalf("api_key survived OAuth login (single-credential invariant violated):\n%s", raw)
	}
	if !strings.Contains(string(raw), `"access_token"`) {
		t.Fatalf("oauth block missing after OAuth login:\n%s", raw)
	}
	if !strings.Contains(res.Stdout, `"cleared_api_key":true`) {
		t.Errorf("stdout = %q, want cleared_api_key=true", res.Stdout)
	}
}

// TestAuthLogin_CredentialConflict_SelfHeals walks the full
// "pre-this-change user with both blocks" scenario end-to-end:
//
//  1. seed a file with both api_key + oauth (legacy state from a
//     prior CLI version),
//  2. run `auth status` — must report api_key (per C2 precedence),
//  3. run `auth login --api-key <newkey>` — must save the new key
//     AND drop the OAuth block (per C3 normalization),
//  4. final file must hold only an api_key, no oauth section.
func TestAuthLogin_CredentialConflict_SelfHeals(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_API_KEY", "")

	// Seed both blocks via the legacy round-trip (Save then SaveOAuth
	// — Save preserves a co-located oauth block, so this is the same
	// file shape an old version would produce).
	store := &auth.FileCredentialStore{}
	if err := store.Save("hg_legacy"); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	if err := auth.SaveOAuthTokens(auth.OAuthTokens{
		AccessToken:  "at_legacy",
		RefreshToken: "rt_legacy",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed SaveOAuthTokens: %v", err)
	}

	// (2) Resolver must select api_key under the new precedence.
	r := &auth.FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred.APIKey != "hg_legacy" {
		t.Fatalf("legacy file: resolved cred = %+v, want APIKey hg_legacy", cred)
	}

	// (3) Next api-key login must drop the OAuth block.
	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("hg_new\n"),
		"auth", "login", "--api-key")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	// (4) Final file holds only api_key, no oauth.
	raw, err := os.ReadFile(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), `"hg_new"`) {
		t.Fatalf("new api_key missing: %s", raw)
	}
	if strings.Contains(string(raw), `"oauth"`) {
		t.Fatalf("oauth block survived conflict self-heal:\n%s", raw)
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

// W4: when cfg.UsersMeBaseURL is empty (production path), the login
// identity probe must read HEYGEN_API_BASE from the config provider
// instead of hardcoding https://api.heygen.com. Without this, pointing
// the CLI at a dev sandbox silently fans the probe out to prod.
func TestAuthLogin_OAuthFlow_HonorsHEYGEN_API_BASEForUsersMeProbe(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)
	t.Setenv("HEYGEN_API_KEY", "")

	idp := newFakeIdP(t)
	usersMe := fakeUsersMeServer(t, `{"data":{"username":"sandbox-user","email":"u@example.com"}}`)

	// Pin the CLI's base URL to the test users/me server. This is the
	// production code path (no cfg.UsersMeBaseURL override) but the
	// probe should still hit our test server because HEYGEN_API_BASE
	// directs the config provider there.
	t.Setenv("HEYGEN_API_BASE", usersMe.URL)

	cfg := oauthLoginConfig{
		TokenURL: idp.server.URL + "/v1/oauth/token",
		OpenBrowser: func(authURL string) error {
			go func() {
				time.Sleep(50 * time.Millisecond)
				hitBrowserCallback(t, idp.expectedCode, authURL)
			}()
			return nil
		},
		// Deliberately leave UsersMeBaseURL empty — we want to verify
		// the production fallback (ctx.configProvider.BaseURL()).
	}
	res := runOAuthLoginForTest(t, cfg)
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s\nstdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}
	if !strings.Contains(res.Stderr, "Logged in as sandbox-user") {
		t.Errorf("stderr = %q, want 'Logged in as sandbox-user' (probe should have hit %s, not api.heygen.com)", res.Stderr, usersMe.URL)
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

	// Mirror what initContext sets up so runOAuthLogin can fall back to
	// ctx.configProvider.BaseURL() when cfg.UsersMeBaseURL is empty (W4).
	ctx := &cmdContext{
		formatter: formatter,
		version:   "test",
		configProvider: &config.LayeredProvider{
			Env:  &config.EnvProvider{},
			File: &config.FileProvider{},
		},
		client: nil,
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

// N1: explicit `heygen auth login --oauth` in a headless shell (no
// stdin TTY + HEYGEN_NO_BROWSER=1) must fail fast with a usage error
// instead of blocking on the loopback callback for ~5min. Test by
// calling runOAuthLogin directly with cfg.OpenBrowser nil — that's
// the "production" signal that the test path also lacks injection.
func TestRunOAuthLogin_HeadlessShell_FailsFast(t *testing.T) {
	t.Setenv("HEYGEN_NO_BROWSER", "1")
	t.Setenv("BROWSER", "")
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// Force stdinIsTerminalFunc to report false — `go test` may attach
	// a tty even when the harness is non-interactive.
	orig := stdinIsTerminalFunc
	stdinIsTerminalFunc = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminalFunc = orig })

	var stdout, stderr strings.Builder
	formatter := formatterForArgs([]string{"auth", "login"}, &stdout, &stderr)
	ctx := &cmdContext{
		formatter: formatter,
		version:   "test",
		configProvider: &config.LayeredProvider{
			Env:  &config.EnvProvider{},
			File: &config.FileProvider{},
		},
	}
	_ = analytics.New("test", false)
	cmd := newAuthLoginCmd(ctx)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	// Production-shape config: NO OpenBrowser injected. That's the
	// signal the guard uses to fast-fail.
	cfg := oauthLoginConfig{}

	// Call runOAuthLogin directly so the test doesn't depend on the
	// flag dispatcher (which also has its own non-interactive fallback
	// to the API-key path). The N1 guard sits inside runOAuthLogin.
	start := time.Now()
	err := runOAuthLogin(cmd, ctx, cfg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected headless --oauth to fail fast, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("runOAuthLogin took %v in a headless shell; must fast-fail (N1)", elapsed)
	}
	msg := err.Error()
	if !strings.Contains(msg, "headless") {
		t.Errorf("error = %q, want a 'headless' hint (N1)", msg)
	}
	if !strings.Contains(msg, "--api-key") {
		t.Errorf("error = %q, want a pointer to --api-key (N1)", msg)
	}
}
