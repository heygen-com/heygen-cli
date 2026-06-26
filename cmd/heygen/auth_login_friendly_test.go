package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/auth"
)

// usersMeServerForKey serves /v3/users/me, asserting the api-key probe
// sent the supplied key on x-api-key (and NOT Authorization: Bearer).
// The body fields control what the probe returns.
func usersMeServerForKey(t *testing.T, wantKey, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/users/me" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("x-api-key"); got != wantKey {
			t.Errorf("x-api-key = %q, want %q", got, wantKey)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header set on api-key probe: %q", got)
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

// TestAuthLogin_APIKey_PersistsFriendlyUserInfo is the end-to-end happy
// path for the friendly-display change on the api-key login route:
// post-login, the CLI probes /v3/users/me with x-api-key, persists the
// returned email/name/username to the credentials file, and surfaces
// "Logged in as ..." on stderr.
func TestAuthLogin_APIKey_PersistsFriendlyUserInfo(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	srv, hits := usersMeServerForKey(t, "hg_test_key",
		`{"data":{"username":"jdoe","email":"jane@example.com","first_name":"Jane","last_name":"Doe"}}`)

	res := runCommandWithInput(t, srv.URL, "", strings.NewReader("hg_test_key\n"),
		"auth", "login", "--api-key")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s\nstdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("/v3/users/me hits = %d, want 1", got)
	}

	// Persisted user block on disk.
	ui, err := auth.LoadUserInfo()
	if err != nil {
		t.Fatalf("LoadUserInfo: %v", err)
	}
	want := auth.UserInfo{
		Username:  "jdoe",
		Email:     "jane@example.com",
		FirstName: "Jane",
		LastName:  "Doe",
	}
	if ui != want {
		t.Fatalf("user block = %+v, want %+v", ui, want)
	}

	// Friendly-display priority: email wins. Stderr must surface the
	// email rather than the username so the line shows recognizable
	// identity instead of an opaque handle.
	if !strings.Contains(res.Stderr, "Logged in as jane@example.com") {
		t.Errorf("stderr = %q, want 'Logged in as jane@example.com'", res.Stderr)
	}

	// JSON payload exposes the friendly fields so jq pipelines can read
	// them without a second /v3/users/me call.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, res.Stdout)
	}
	if parsed["email"] != "jane@example.com" {
		t.Errorf("stdout.email = %v, want jane@example.com", parsed["email"])
	}
	if parsed["first_name"] != "Jane" {
		t.Errorf("stdout.first_name = %v, want Jane", parsed["first_name"])
	}
}

// TestAuthLogin_APIKey_FallsBackOnProbeFailure verifies the
// non-blocking-on-failure invariant: when /v3/users/me is unreachable
// (or returns a non-200), the api-key login still succeeds and the key
// is on disk. Friendly fields are simply absent.
func TestAuthLogin_APIKey_FallsBackOnProbeFailure(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	// Server returns 500 on the probe — non-fatal per the contract.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer srv.Close()

	res := runCommandWithInput(t, srv.URL, "", strings.NewReader("hg_key\n"),
		"auth", "login", "--api-key")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (login must not block on probe failure)\nstderr: %s",
			res.ExitCode, res.Stderr)
	}

	// api_key was persisted.
	creds, err := os.ReadFile(filepath.Join(configDir, "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(creds), `"hg_key"`) {
		t.Fatalf("api_key missing from creds:\n%s", creds)
	}

	// No friendly fields persisted.
	ui, err := auth.LoadUserInfo()
	if err != nil {
		t.Fatalf("LoadUserInfo: %v", err)
	}
	if !ui.IsZero() {
		t.Fatalf("expected zero UserInfo on probe failure, got %+v", ui)
	}

	// Friendly-display line absent (probe failed → no display).
	if strings.Contains(res.Stderr, "Logged in as") {
		t.Errorf("stderr should NOT contain 'Logged in as' on probe failure:\n%s", res.Stderr)
	}
}

// TestAuthLogin_APIKey_ClearsStaleUserInfo: re-login with a key for a
// DIFFERENT account where the probe FAILS must not leave the prior
// account's friendly fields on disk to mislead `auth status`.
func TestAuthLogin_APIKey_ClearsStaleUserInfo(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)

	// Seed prior account state directly via the store: api_key + user.
	if err := (&auth.FileCredentialStore{}).Save("hg_old"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := auth.SaveUserInfo(auth.UserInfo{Email: "old@example.com"}); err != nil {
		t.Fatalf("SaveUserInfo: %v", err)
	}

	// New login: probe fails (500).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := runCommandWithInput(t, srv.URL, "", strings.NewReader("hg_new\n"),
		"auth", "login", "--api-key")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	// New key on disk.
	creds, err := os.ReadFile(filepath.Join(configDir, "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(creds), `"hg_new"`) {
		t.Fatalf("new api_key missing:\n%s", creds)
	}
	// Old user info must be gone — leaving it would surface the wrong
	// account in `auth status` until the next successful probe.
	if strings.Contains(string(creds), "old@example.com") {
		t.Fatalf("stale user block survived re-login:\n%s", creds)
	}
}

// TestAuthLogin_OAuth_PersistsFullUserInfo extends the existing OAuth
// integration test to assert the new full schema (first_name, last_name)
// is persisted, not just username/email.
func TestAuthLogin_OAuth_PersistsFullUserInfo(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", configDir)
	t.Setenv("HEYGEN_API_KEY", "")

	idp := newFakeIdP(t)
	usersMe := fakeUsersMeServer(t,
		`{"data":{"username":"jdoe","email":"jane@example.com","first_name":"Jane","last_name":"Doe"}}`)

	cfg := oauthLoginConfig{
		TokenURL: idp.server.URL + "/v1/oauth/token",
		OpenBrowser: func(authURL string) error {
			go hitBrowserCallbackAfter(t, idp.expectedCode, authURL)
			return nil
		},
		UsersMeBaseURL: usersMe.URL,
	}
	res := runOAuthLoginForTest(t, cfg)
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s\nstdout: %s", res.ExitCode, res.Stderr, res.Stdout)
	}

	ui, err := auth.LoadUserInfo()
	if err != nil {
		t.Fatalf("LoadUserInfo: %v", err)
	}
	want := auth.UserInfo{
		Username:  "jdoe",
		Email:     "jane@example.com",
		FirstName: "Jane",
		LastName:  "Doe",
	}
	if ui != want {
		t.Fatalf("persisted user block = %+v, want %+v", ui, want)
	}
}

// hitBrowserCallbackAfter mirrors the goroutine pattern the other OAuth
// tests use: a tiny delay so the loopback server's Accept() loop is up
// before we connect, then invoke the shared hitBrowserCallback helper
// defined in auth_login_oauth_test.go.
func hitBrowserCallbackAfter(t *testing.T, code, authURL string) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	hitBrowserCallback(t, code, authURL)
}
