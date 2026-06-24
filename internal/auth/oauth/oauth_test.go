package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeIdP wraps an httptest.Server with helpers for the three OAuth
// endpoints the driver hits. Tests configure the response inline via
// the HandlerFunc fields.
type fakeIdP struct {
	server *httptest.Server

	tokenHandler  http.HandlerFunc
	revokeHandler http.HandlerFunc

	// captured for assertions
	lastTokenBody  url.Values
	lastRevokeBody url.Values
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	idp := &fakeIdP{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		idp.lastTokenBody, _ = url.ParseQuery(string(body))
		if idp.tokenHandler != nil {
			idp.tokenHandler(w, r)
			return
		}
		http.Error(w, "no token handler configured", http.StatusInternalServerError)
	})
	mux.HandleFunc("/v1/oauth/revoke", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		idp.lastRevokeBody, _ = url.ParseQuery(string(body))
		if idp.revokeHandler != nil {
			idp.revokeHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *fakeIdP) client(t *testing.T) *Client {
	t.Helper()
	return NewClient(
		WithClientID("test-client"),
		WithAuthorizeURL("https://example.test/oauth/authorize"),
		WithTokenURL(f.server.URL+"/v1/oauth/token"),
		WithRevokeURL(f.server.URL+"/v1/oauth/revoke"),
		WithHTTPClient(f.server.Client()),
		WithNow(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }),
	)
}

func TestBuildAuthorizationURL_IncludesAllParams(t *testing.T) {
	c := NewClient(
		WithClientID("test-client"),
		WithAuthorizeURL("https://example.test/oauth/authorize"),
	)
	u := c.BuildAuthorizationURL("the-state", "the-challenge", "http://127.0.0.1:12345/cb", "")

	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := parsed.Query()
	tests := []struct {
		key, want string
	}{
		{"response_type", "code"},
		{"client_id", "test-client"},
		{"redirect_uri", "http://127.0.0.1:12345/cb"},
		{"scope", DefaultScopes},
		{"state", "the-state"},
		{"code_challenge", "the-challenge"},
		{"code_challenge_method", "S256"},
	}
	for _, tc := range tests {
		if got := q.Get(tc.key); got != tc.want {
			t.Errorf("query[%s] = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestBuildAuthorizationURL_HonorsExistingQuery(t *testing.T) {
	c := NewClient(WithAuthorizeURL("https://example.test/oauth/authorize?foo=bar"))
	u := c.BuildAuthorizationURL("s", "c", "http://127.0.0.1/cb", "openid")
	if !strings.Contains(u, "foo=bar") {
		t.Errorf("expected pre-existing query to survive: %s", u)
	}
	if !strings.Contains(u, "&client_id=") {
		t.Errorf("expected & separator, got %s", u)
	}
}

func TestBuildAuthorizationURL_CustomScopeRespected(t *testing.T) {
	c := NewClient(WithAuthorizeURL("https://example.test/oauth/authorize"))
	u := c.BuildAuthorizationURL("s", "c", "http://127.0.0.1/cb", "custom scope here")
	parsed, _ := url.Parse(u)
	if got := parsed.Query().Get("scope"); got != "custom scope here" {
		t.Errorf("scope = %q, want %q", got, "custom scope here")
	}
}

func TestExchangeAuthorizationCode_Success(t *testing.T) {
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"access_token": "AT-123",
			"refresh_token": "RT-456",
			"token_type": "Bearer",
			"expires_in": 3600,
			"scope": "openid profile email"
		}`)
	}
	c := idp.client(t)

	tok, err := c.ExchangeAuthorizationCode(context.Background(), "the-code", "the-verifier", "http://127.0.0.1:8080/cb")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode: %v", err)
	}
	if tok.AccessToken != "AT-123" {
		t.Errorf("access_token = %q", tok.AccessToken)
	}
	if tok.RefreshToken != "RT-456" {
		t.Errorf("refresh_token = %q", tok.RefreshToken)
	}
	if tok.ExpiresIn != 3600 {
		t.Errorf("expires_in = %d", tok.ExpiresIn)
	}
	if tok.IssuedAt.IsZero() {
		t.Error("IssuedAt should be set by driver")
	}

	// Verify the form fields the driver POSTed.
	want := map[string]string{
		"grant_type":    "authorization_code",
		"code":          "the-code",
		"redirect_uri":  "http://127.0.0.1:8080/cb",
		"client_id":     "test-client",
		"code_verifier": "the-verifier",
	}
	for k, v := range want {
		if got := idp.lastTokenBody.Get(k); got != v {
			t.Errorf("form[%s] = %q, want %q", k, got, v)
		}
	}
}

func TestExchangeAuthorizationCode_RejectedReturnsTokenError(t *testing.T) {
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}
	c := idp.client(t)

	_, err := c.ExchangeAuthorizationCode(context.Background(), "bad-code", "verifier", "http://127.0.0.1/cb")
	if err == nil {
		t.Fatal("expected error")
	}
	var te *TokenError
	if !errors.As(err, &te) {
		t.Fatalf("expected *TokenError, got %T: %v", err, err)
	}
	if te.Status != http.StatusBadRequest {
		t.Errorf("Status = %d", te.Status)
	}
	if !errors.Is(err, ErrRejected) {
		t.Errorf("errors.Is(err, ErrRejected) = false, want true")
	}
}

func TestRefreshAccessToken_PreservesPriorTokenWhenServerOmits(t *testing.T) {
	// RFC 6749 §6 allows the server to skip refresh_token on a refresh
	// response (no rotation). The driver returns the response as-is;
	// caller-side persistence handles the preserve. This test verifies
	// the wire-level behavior — the driver does not invent fields.
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"new-AT","token_type":"Bearer","expires_in":1800}`)
	}
	c := idp.client(t)

	tok, err := c.RefreshAccessToken(context.Background(), "old-RT")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}
	if tok.AccessToken != "new-AT" {
		t.Errorf("access_token = %q", tok.AccessToken)
	}
	if tok.RefreshToken != "" {
		t.Errorf("driver should not invent refresh_token, got %q", tok.RefreshToken)
	}
	if got := idp.lastTokenBody.Get("grant_type"); got != "refresh_token" {
		t.Errorf("grant_type = %q", got)
	}
	if got := idp.lastTokenBody.Get("refresh_token"); got != "old-RT" {
		t.Errorf("refresh_token = %q", got)
	}
}

func TestRefreshAccessToken_RejectsEmpty(t *testing.T) {
	idp := newFakeIdP(t)
	c := idp.client(t)
	if _, err := c.RefreshAccessToken(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty refresh token")
	}
}

func TestRefreshAccessToken_401Returns_ErrRejected(t *testing.T) {
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusUnauthorized)
	}
	c := idp.client(t)
	_, err := c.RefreshAccessToken(context.Background(), "stale-rt")
	if !errors.Is(err, ErrRejected) {
		t.Errorf("expected ErrRejected, got %v", err)
	}
}

func TestRevokeToken_Success(t *testing.T) {
	idp := newFakeIdP(t)
	idp.revokeHandler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
	c := idp.client(t)

	if err := c.RevokeToken(context.Background(), "tok-abc"); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if got := idp.lastRevokeBody.Get("token"); got != "tok-abc" {
		t.Errorf("body[token] = %q", got)
	}
	if got := idp.lastRevokeBody.Get("client_id"); got != "test-client" {
		t.Errorf("body[client_id] = %q", got)
	}
}

func TestRevokeToken_SwallowsNetworkErrors(t *testing.T) {
	// Best-effort: a hung/unreachable IdP MUST NOT block local logout.
	c := NewClient(
		WithClientID("test-client"),
		WithRevokeURL("http://127.0.0.1:1/v1/oauth/revoke"), // unroutable
		WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}),
	)
	if err := c.RevokeToken(context.Background(), "tok"); err != nil {
		t.Errorf("expected nil error on network failure, got %v", err)
	}
}

func TestRevokeToken_RejectsEmpty(t *testing.T) {
	c := NewClient()
	if err := c.RevokeToken(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestExchangeAuthorizationCode_BadShapeReturnsError(t *testing.T) {
	tests := []struct {
		name, body string
	}{
		{"missing access_token", `{"token_type":"Bearer"}`},
		{"non-object body", `[]`},
		{"access_token with newline", "{\"access_token\":\"AT\\nleak\"}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			idp := newFakeIdP(t)
			idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}
			c := idp.client(t)
			_, err := c.ExchangeAuthorizationCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb")
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestExchangeAuthorizationCode_5xxReturnsGenericError(t *testing.T) {
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}
	c := idp.client(t)
	_, err := c.ExchangeAuthorizationCode(context.Background(), "code", "verifier", "http://127.0.0.1/cb")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrRejected) {
		t.Errorf("5xx should not be ErrRejected: %v", err)
	}
}

func TestExchangeAuthorizationCode_ContextCancellation(t *testing.T) {
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"AT"}`)
	}
	c := idp.client(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.ExchangeAuthorizationCode(ctx, "code", "verifier", "http://127.0.0.1/cb")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
