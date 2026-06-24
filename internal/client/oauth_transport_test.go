package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

// A refresh against an unreachable IdP is a TRANSIENT failure, not a
// credential-rejected case. Per W1, the transport must surface the
// underlying network error (so the executor can render something
// accurate) instead of misclassifying it as ErrReLoginNeeded.
func TestClient_Do_OAuthExpired_TransientRefreshFailure_NotReLoginNeeded(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			oauth.WithTokenURL("http://127.0.0.1:1"), // dead endpoint
		)),
		WithOAuthPersister(&fakePersister{}),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error refreshing against dead endpoint")
	}
	if errors.Is(err, ErrReLoginNeeded) {
		t.Fatalf("err = %v, must NOT be ErrReLoginNeeded for a transient failure (W1)", err)
	}
	if !errors.Is(err, oauth.ErrRejected) {
		// Sanity: not ErrRejected either — it's a plain transport error.
		// We don't pin the exact message because dial errors vary by OS.
		if !strings.Contains(err.Error(), "oauth: refresh failed") {
			t.Errorf("err = %v, want %q prefix so callers can recognize transient refresh failures", err, "oauth: refresh failed")
		}
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

// W1: a 401 followed by an IdP-rejected refresh (HTTP 400 with
// oauth.ErrRejected wrapped inside) must surface as ErrReLoginNeeded.
// This is the legitimate "your refresh token is dead" path.
func TestClient_Do_OAuth_RefreshRejected_ReturnsReLoginNeeded(t *testing.T) {
	var refreshes int32
	// nil tok = the IdP returns 400, which oauth.postTokenForm wraps as
	// a TokenError with errRejected.
	idp := newFakeIdP(t, nil, &refreshes)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "stale_at",
		RefreshToken: "rt_dead",
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

	req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error on rejected refresh")
	}
	if !errors.Is(err, ErrReLoginNeeded) {
		t.Fatalf("err = %v, want ErrReLoginNeeded for an IdP-rejected refresh", err)
	}
}

// W1: a 401 followed by a TRANSIENT refresh failure (IdP unreachable)
// must NOT be classified as ErrReLoginNeeded — the user can still log
// in. The error surfaces with the transient cause so the executor can
// render an accurate message ("network unreachable") instead of
// "OAuth session expired".
func TestClient_Do_OAuth_TransientRefreshFailure_NotReLoginNeeded(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "stale_at",
		RefreshToken: "rt",
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL("http://127.0.0.1:1"), // dead IdP
		)),
		WithOAuthPersister(&fakePersister{}),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error on transient refresh failure")
	}
	if errors.Is(err, ErrReLoginNeeded) {
		t.Fatalf("err = %v, must NOT be ErrReLoginNeeded for a transient failure (W1)", err)
	}
	if !strings.Contains(err.Error(), "oauth: refresh failed") {
		t.Errorf("err = %v, want %q prefix", err, "oauth: refresh failed")
	}
}

// W2: a 401 on a request whose body cannot be replayed (Body set but
// GetBody nil) must NOT call cloneRequestForRetry — that would
// nil-panic on req.GetBody. Instead the 401 is returned to the caller
// unchanged.
func TestClient_Do_OAuth_401WithNonReplayableBody_ReturnsResponseUnchanged(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, &oauth.TokenResponse{AccessToken: "fresh"}, &refreshes)

	var apiCalls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		w.WriteHeader(http.StatusUnauthorized)
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

	// Build a POST request with Body set but GetBody nil. http.NewRequest
	// sets GetBody for strings.Reader-shaped readers, so we use a
	// plain io.NopCloser around bytes.Buffer (no GetBody auto-population).
	body := io.NopCloser(bytes.NewBufferString(`{"x":1}`))
	req, err := http.NewRequest("POST", api.URL+"/v3/things", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.GetBody = nil // belt + braces; NopCloser shouldn't trigger it but be explicit
	if canReplayBody(req) {
		t.Fatal("test setup: canReplayBody must be false for this case")
	}

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v (must return the 401 unchanged, NOT panic on cloneRequestForRetry)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401 returned unchanged", resp.StatusCode)
	}
	if refreshes != 0 {
		t.Errorf("refreshes = %d, want 0 — we must NOT refresh when we can't replay", refreshes)
	}
	if apiCalls != 1 {
		t.Errorf("apiCalls = %d, want 1 — no retry on non-replayable body", apiCalls)
	}
}

// N1: two concurrent Do() calls against an OAuth credential at near
// expiry must coalesce to a single refresh round-trip. Tested with
// race detector enabled (go test -race) to also catch the credential
// mutation hazard.
func TestClient_Do_OAuth_ConcurrentRefresh_Coalesces(t *testing.T) {
	var refreshes int32
	var mu sync.Mutex
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		refreshes++
		mu.Unlock()
		// Slight delay so both goroutines have a real chance to race
		// into the refresh path before either completes.
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer idp.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "stale_at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(5 * time.Second), // inside the skew
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

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
			resp, err := c.Do(req)
			if err != nil {
				t.Errorf("Do: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()

	mu.Lock()
	got := refreshes
	mu.Unlock()
	if got != 1 {
		t.Errorf("refreshes = %d, want 1 (concurrent Do() must coalesce, N1)", got)
	}
}

// N2: a near-expiry credential that triggers a proactive refresh + the
// fresh token still gets 401'd must NOT refresh a SECOND time. The
// 401 retry path detects the in-Do refresh and goes straight to
// ErrReLoginNeeded.
func TestClient_Do_OAuth_NoDoubleRefreshAfterProactive(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, &oauth.TokenResponse{AccessToken: "fresh_at"}, &refreshes)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API always 401s, even with the freshly-minted token.
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer api.Close()

	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "stale_at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(5 * time.Second), // inside the skew → proactive refresh
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

	req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected ErrReLoginNeeded after fresh-token 401")
	}
	if !errors.Is(err, ErrReLoginNeeded) {
		t.Fatalf("err = %v, want ErrReLoginNeeded", err)
	}
	if refreshes != 1 {
		t.Errorf("refreshes = %d, want 1 (proactive refresh only; N2 forbids a second refresh after still-401)", refreshes)
	}
}

// S1: two concurrent Do() calls against a still-valid (proactive
// refresh NOT triggered) OAuth credential that both get 401'd must
// coalesce the REACTIVE refresh to a single IdP round-trip. Without
// the refreshMu + sent-token re-check, both goroutines call
// forceRefresh and the second consumes a refresh_token that the IdP
// has already rotated → spurious ErrReLoginNeeded. Run with -race
// to also exercise the credMu-guarded snapshot helper. (S1)
func TestClient_Do_OAuth_ConcurrentReactiveRefresh_Coalesces(t *testing.T) {
	var refreshes int32
	var refreshMu sync.Mutex
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshMu.Lock()
		refreshes++
		current := refreshes
		refreshMu.Unlock()
		// Slow down so both Do() goroutines have a real chance to
		// observe their 401 and race into the reactive refresh path
		// before either completes.
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		// First IdP call rotates the refresh_token. Second call would
		// receive the already-consumed token → invalid_grant in real
		// IdP behaviour. The test asserts that second call never
		// happens.
		if current == 1 {
			_, _ = w.Write([]byte(`{"access_token":"fresh_at","refresh_token":"rotated_rt","token_type":"Bearer","expires_in":3600}`))
			return
		}
		// If we reach here, the coalescing failed.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer idp.Close()

	var apiCalls int32
	var apiMu sync.Mutex
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiMu.Lock()
		apiCalls++
		apiMu.Unlock()
		// Reject the stale Bearer; accept the fresh one. The fresh
		// access_token is "fresh_at" from the IdP above.
		if r.Header.Get("Authorization") == "Bearer fresh_at" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer api.Close()

	// Long-lived ExpiresAt means proactive refresh is NOT triggered;
	// the only refresh path is reactive-after-401.
	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "stale_at",
		RefreshToken: "rt_seed",
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

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
			resp, err := c.Do(req)
			if err != nil {
				errs[i] = err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs[i] = fmt.Errorf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Do[%d] err = %v (concurrent reactive refresh must not surface invalid_grant)", i, err)
		}
	}

	refreshMu.Lock()
	got := refreshes
	refreshMu.Unlock()
	if got != 1 {
		t.Errorf("IdP refreshes = %d, want 1 (concurrent reactive Do() must coalesce, S1)", got)
	}
}

// S2: forceRefresh's call to OAuthPersister.SaveOAuthTokens must NOT
// silently swallow a disk-write error. The in-memory token is fine for
// the current Do(), but the next CLI invocation will reload the old
// (now-rotated, now-dead) refresh_token from disk and force re-login.
// We surface a warning to the configured warn writer so the user can
// investigate. (S2)
func TestClient_Do_OAuth_PersistFailure_WarnsToStderr(t *testing.T) {
	var refreshes int32
	idp := newFakeIdP(t, &oauth.TokenResponse{AccessToken: "fresh", RefreshToken: "rotated"}, &refreshes)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	var warn strings.Builder
	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuthExpired,
		RefreshToken: "rt_seed",
	},
		WithBaseURL(api.URL),
		WithHTTPClient(api.Client()),
		WithMaxRetries(0),
		WithOAuthClient(oauth.NewClient(
			oauth.WithTokenURL(idp.URL),
			oauth.WithHTTPClient(idp.Client()),
		)),
		WithOAuthPersister(failingPersister{}),
		WithWarnOutput(&warn),
	)

	req, _ := http.NewRequest("GET", api.URL+"/v3/things", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v (refresh-success + persist-fail must not fail the request)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}

	got := warn.String()
	if !strings.Contains(got, "failed to persist") {
		t.Errorf("warn output = %q, want a 'failed to persist' warning (S2)", got)
	}
	if !strings.Contains(got, "boom-persist") {
		t.Errorf("warn output = %q, want underlying error %q to surface (S2)", got, "boom-persist")
	}
}

// failingPersister always returns a known error from SaveOAuthTokens
// so the warn-to-stderr behaviour can be asserted deterministically.
type failingPersister struct{}

func (failingPersister) SaveOAuthTokens(_ auth.OAuthTokens) error {
	return errors.New("boom-persist")
}

// S3: NewWithCredential must panic when an OAuth credential is supplied
// without WithOAuthClient. Silent fallback to oauth.NewClient() would
// tie a test transport to the live IdP, defeating the whole point of
// the test-injected oauth.Client. (S3)
func TestClient_NewWithCredential_OAuthWithoutOAuthClient_Panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected NewWithCredential to panic on OAuth credential without WithOAuthClient (S3)")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T(%v), want string", r, r)
		}
		if !strings.Contains(msg, "WithOAuthClient") {
			t.Errorf("panic message = %q, want a hint about WithOAuthClient (S3)", msg)
		}
	}()

	// No WithOAuthClient — must panic.
	_ = NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "at",
		RefreshToken: "rt",
	})
}

// S3 (positive case): API-key credentials must NOT trigger the fail-fast
// — they don't need an oauth.Client.
func TestClient_NewWithCredential_APIKey_NoOAuthClientNeeded(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("API-key NewWithCredential panicked: %v (S3: only OAuth needs WithOAuthClient)", r)
		}
	}()
	c := NewWithCredential(auth.Credential{
		Type:   auth.CredentialTypeAPIKey,
		APIKey: "k",
	})
	if c == nil {
		t.Fatal("NewWithCredential returned nil")
	}
}

// N2: needsRefresh must honour a test-injected nowFn so refresh
// decisions are deterministic. Pin "now" to a moment just inside the
// skew window and assert refresh fires.
func TestClient_NeedsRefresh_UsesInjectedNowFn(t *testing.T) {
	pinned := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	c := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "at",
		RefreshToken: "rt",
		// ExpiresAt set so (pinned + skew) is AFTER expiry → refresh.
		ExpiresAt: pinned.Add(10 * time.Second),
	},
		WithOAuthClient(oauth.NewClient(oauth.WithTokenURL("http://example.invalid"))),
		WithNow(func() time.Time { return pinned }),
	)
	if !c.needsRefresh() {
		t.Errorf("needsRefresh = false with pinned now inside skew window; want true (N2)")
	}

	// Now pin "now" well before expiry — refresh must NOT fire.
	c2 := NewWithCredential(auth.Credential{
		Type:         auth.CredentialTypeOAuth,
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    pinned.Add(2 * time.Hour),
	},
		WithOAuthClient(oauth.NewClient(oauth.WithTokenURL("http://example.invalid"))),
		WithNow(func() time.Time { return pinned }),
	)
	if c2.needsRefresh() {
		t.Errorf("needsRefresh = true with pinned now well before expiry; want false (N2)")
	}
}
