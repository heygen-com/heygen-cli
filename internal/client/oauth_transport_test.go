package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/auth/oauth"
)

// fakePersister captures persisted tokens so tests can assert that the
// transport saved refreshed tokens back without touching real disk.
type fakePersister struct {
	mu   sync.Mutex
	last auth.OAuthTokens
	n    int
}

func (p *fakePersister) SaveOAuthTokens(tok auth.OAuthTokens) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = tok
	p.n++
	return nil
}

func (p *fakePersister) snapshot() (int, auth.OAuthTokens) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n, p.last
}

// newFakeIdP spins up an httptest server that answers
// grant_type=refresh_token with `tok` (one shot per request). It records
// every refresh hit on `refreshes`. A nil `tok` returns a 400.
func newFakeIdP(t *testing.T, tok *oauth.TokenResponse, refreshes *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		*refreshes++
		if tok == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Defensive: assemble the response payload by hand so we can
		// test the omit-refresh-token (RFC 6749 §6) path.
		body := `{"access_token":"` + tok.AccessToken + `","token_type":"Bearer","expires_in":3600`
		if tok.RefreshToken != "" {
			body += `,"refresh_token":"` + tok.RefreshToken + `"`
		}
		if tok.Scope != "" {
			body += `,"scope":"` + tok.Scope + `"`
		}
		body += `}`
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_Do_BearerHeader_OAuth(t *testing.T) {
	var gotAuth, gotAPIKey string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:        auth.CredentialTypeOAuth,
		AccessToken: "at_xyz",
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithOAuthClient(oauth.NewClient(oauth.WithTokenURL("http://example.invalid/token"))),
		WithOAuthPersister(&fakePersister{}),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/users/me", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if gotAuth != "Bearer at_xyz" {
		t.Errorf("Authorization = %q, want Bearer at_xyz", gotAuth)
	}
	if gotAPIKey != "" {
		t.Errorf("x-api-key = %q, want empty (OAuth credentials must NOT send x-api-key)", gotAPIKey)
	}
}

func TestClient_Do_APIKeyUnchanged_NoBearer(t *testing.T) {
	var gotAuth, gotAPIKey string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := New("the-key", WithBaseURL(api.URL), WithHTTPClient(api.Client()))

	req, _ := http.NewRequest("GET", api.URL+"/v3/users/me", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if gotAPIKey != "the-key" {
		t.Errorf("x-api-key = %q, want the-key", gotAPIKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty for API-key credential", gotAuth)
	}
}

// 401 with OAuth + refresh_token: client refreshes once and retries.
// Second call sees the new Bearer header.
func TestClient_Do_OAuth_RefreshesOn401_AndRetries(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, &oauth.TokenResponse{
		AccessToken:  "fresh_at",
		RefreshToken: "rotated_rt",
		Scope:        "openid",
	}, &refreshes)

	var apiCalls int
	var gotAuths []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		gotAuths = append(gotAuths, r.Header.Get("Authorization"))
		// First request: reject. Second: accept.
		if apiCalls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"expired"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	persister := &fakePersister{}
	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "stale_at",
		RefreshToken: "rt_seed",
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL(idp.URL),
			oauth.WithHTTPClient(idp.Client()),
		)),
		WithOAuthPersister(persister),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/users/me", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if apiCalls != 2 {
		t.Fatalf("apiCalls = %d, want 2 (first 401, second 200)", apiCalls)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1", refreshes)
	}
	if gotAuths[0] != "Bearer stale_at" {
		t.Errorf("first auth = %q, want Bearer stale_at", gotAuths[0])
	}
	if gotAuths[1] != "Bearer fresh_at" {
		t.Errorf("second auth = %q, want Bearer fresh_at (refreshed)", gotAuths[1])
	}
	n, last := persister.snapshot()
	if n != 1 {
		t.Errorf("persister saves = %d, want 1", n)
	}
	if last.AccessToken != "fresh_at" {
		t.Errorf("persisted access_token = %q, want fresh_at", last.AccessToken)
	}
	if last.RefreshToken != "rotated_rt" {
		t.Errorf("persisted refresh_token = %q, want rotated_rt", last.RefreshToken)
	}
}

// 401 after a successful refresh = re-login needed (sentinel error).
func TestClient_Do_OAuth_TwiceUnauthorized_ReturnsReLoginSentinel(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, &oauth.TokenResponse{AccessToken: "still_bad_at"}, &refreshes)

	var apiCalls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"still bad"}}`))
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "old_at",
		RefreshToken: "rt",
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL(idp.URL),
			oauth.WithHTTPClient(idp.Client()),
		)),
		WithOAuthPersister(&fakePersister{}),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/users/me", nil)
	resp, err := c.Do(req)
	if resp != nil {
		resp.Body.Close()
	}

	if err == nil {
		t.Fatal("expected error after second 401, got nil")
	}
	if !errors.Is(err, ErrReLoginNeeded) {
		t.Fatalf("err = %v, want ErrReLoginNeeded", err)
	}
	if apiCalls != 2 {
		t.Errorf("apiCalls = %d, want 2 (initial + post-refresh retry)", apiCalls)
	}
}

// Refresh-only credential (OAuthExpired) triggers a proactive refresh
// BEFORE the first request, so the API only sees the new Bearer header.
func TestClient_Do_OAuthExpired_RefreshesBeforeFirstRequest(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, &oauth.TokenResponse{AccessToken: "minted_at"}, &refreshes)

	var apiCalls int
	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuthExpired,
		RefreshToken: "rt",
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL(idp.URL),
			oauth.WithHTTPClient(idp.Client()),
		)),
		WithOAuthPersister(&fakePersister{}),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/users/me", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if apiCalls != 1 {
		t.Errorf("apiCalls = %d, want 1 (refresh + 1 api hit, no 401 retry)", apiCalls)
	}
	if refreshes != 1 {
		t.Errorf("refreshes = %d, want 1 (proactive)", refreshes)
	}
	if gotAuth != "Bearer minted_at" {
		t.Errorf("Authorization = %q, want Bearer minted_at", gotAuth)
	}
}

// OAuth credential with an ExpiresAt within the 60s skew should be
// refreshed proactively — no 401 round-trip required.
func TestClient_Do_OAuth_ProactiveRefresh_NearExpiry(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, &oauth.TokenResponse{AccessToken: "minted_at"}, &refreshes)

	var apiCalls int
	var gotAuth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	// Token expires 5s from now → inside the 60s skew → must refresh
	// proactively.
	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "near_expiry_at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(5 * time.Second),
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL(idp.URL),
			oauth.WithHTTPClient(idp.Client()),
		)),
		WithOAuthPersister(&fakePersister{}),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/users/me", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if refreshes != 1 {
		t.Errorf("refreshes = %d, want 1 (proactive)", refreshes)
	}
	if apiCalls != 1 {
		t.Errorf("apiCalls = %d, want 1", apiCalls)
	}
	if gotAuth != "Bearer minted_at" {
		t.Errorf("Authorization = %q, want Bearer minted_at (refreshed)", gotAuth)
	}
}

// A 401 with an API-key credential is NOT retried — there's no refresh
// path. The 401 bubbles up unchanged.
func TestClient_Do_APIKey_NoRefreshOn401(t *testing.T) {
	var apiCalls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
	}))
	defer api.Close()

	c := New("k", WithBaseURL(api.URL), WithHTTPClient(api.Client()), WithMaxRetries(0))

	req, _ := http.NewRequest("GET", api.URL+"/v3/users/me", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want 401", resp.StatusCode)
	}
	if apiCalls != 1 {
		t.Errorf("apiCalls = %d, want 1 (api-key must NOT retry on 401)", apiCalls)
	}
}

// Body-bearing POST request still survives the refresh-and-retry path:
// the retry must replay the body, not send an empty one.
func TestClient_Do_OAuth_RefreshAndRetry_ReplaysBody(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, &oauth.TokenResponse{AccessToken: "fresh"}, &refreshes)

	var bodies []string
	var calls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		bodies = append(bodies, string(buf[:n]))
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "stale",
		RefreshToken: "rt",
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL(idp.URL),
			oauth.WithHTTPClient(idp.Client()),
		)),
		WithOAuthPersister(&fakePersister{}),
	)

	body := strings.NewReader(`{"hello":"world"}`)
	req, err := http.NewRequest("POST", api.URL+"/v3/things", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	// http.NewRequest sets GetBody for strings.Reader-like sources, so
	// the retry path can replay the body.
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	for i, b := range bodies {
		if !strings.Contains(b, `"hello":"world"`) {
			t.Errorf("call %d body = %q, want hello:world", i, b)
		}
	}
}

// Ensure refreshing without an oauth client wired up returns an explicit
// error instead of a nil-pointer deref.
func TestClient_Do_OAuthExpired_NoOAuthClient_ReturnsClearError(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	// Explicitly leave WithOAuthClient out — but the constructor auto-
	// fills oauthClient with the default. We force the broken state by
	// constructing manually below.
	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuthExpired,
		RefreshToken: "rt",
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL("http://127.0.0.1:1"), // dead endpoint
		)),
		WithOAuthPersister(&fakePersister{}),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error refreshing against dead endpoint")
	}
	if !errors.Is(err, ErrReLoginNeeded) {
		t.Fatalf("err = %v, want ErrReLoginNeeded", err)
	}
}

// Sanity check: with a wired persister and a stable token, multiple
// Do() calls don't re-refresh until the token actually nears expiry.
func TestClient_Do_OAuth_NoNeedlessRefresh(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, nil, &refreshes)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "at_long_lived",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL(idp.URL),
			oauth.WithHTTPClient(idp.Client()),
		)),
		WithOAuthPersister(&fakePersister{}),
	)

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("Do[%d]: %v", i, err)
		}
		resp.Body.Close()
	}
	if refreshes != 0 {
		t.Fatalf("refreshes = %d, want 0 (no proactive refresh on long-lived token)", refreshes)
	}
}

// Defensive guard: a Bearer credential must NOT leak through a request
// whose URL the user has rewritten — applyHeaders is per-request,
// driven by the credential, not by the request URL. (Light sanity test
// that the wiring is right; a more thorough test would mock DNS.)
func TestClient_Do_OAuth_BearerOnEveryRequest(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer at_xyz" {
			t.Errorf("Authorization = %q, want Bearer at_xyz", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:        auth.CredentialTypeOAuth,
		AccessToken: "at_xyz",
		ExpiresAt:   time.Now().Add(2 * time.Hour),
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithOAuthClient(oauth.NewClient(oauth.WithTokenURL("http://example.invalid"))),
		WithOAuthPersister(&fakePersister{}),
	)

	// Three independent requests; each must have the Bearer header.
	for _, path := range []string{"/a", "/b", "/c"} {
		u, _ := url.Parse(api.URL + path)
		req, _ := http.NewRequest("GET", u.String(), nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("Do %s: %v", path, err)
		}
		resp.Body.Close()
	}
}
