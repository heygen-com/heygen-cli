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
	u, err := c.BuildAuthorizationURL("the-state", "the-challenge", "http://127.0.0.1:12345/cb", "")
	if err != nil {
		t.Fatalf("BuildAuthorizationURL: %v", err)
	}

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
	u, err := c.BuildAuthorizationURL("s", "c", "http://127.0.0.1/cb", "openid")
	if err != nil {
		t.Fatalf("BuildAuthorizationURL: %v", err)
	}
	if !strings.Contains(u, "foo=bar") {
		t.Errorf("expected pre-existing query to survive: %s", u)
	}
	if !strings.Contains(u, "&client_id=") {
		t.Errorf("expected & separator, got %s", u)
	}
}

func TestBuildAuthorizationURL_CustomScopeRespected(t *testing.T) {
	c := NewClient(WithAuthorizeURL("https://example.test/oauth/authorize"))
	u, err := c.BuildAuthorizationURL("s", "c", "http://127.0.0.1/cb", "custom scope here")
	if err != nil {
		t.Fatalf("BuildAuthorizationURL: %v", err)
	}
	parsed, _ := url.Parse(u)
	if got := parsed.Query().Get("scope"); got != "custom scope here" {
		t.Errorf("scope = %q, want %q", got, "custom scope here")
	}
}

func TestBuildAuthorizationURL_RejectsEmptyArgs(t *testing.T) {
	c := NewClient(WithAuthorizeURL("https://example.test/oauth/authorize"))
	tests := []struct {
		name, state, challenge, redirect string
	}{
		{"empty state", "", "c", "http://127.0.0.1/cb"},
		{"empty challenge", "s", "", "http://127.0.0.1/cb"},
		{"empty redirect", "s", "c", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.BuildAuthorizationURL(tc.state, tc.challenge, tc.redirect, ""); err == nil {
				t.Fatal("expected error for empty arg")
			}
		})
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

// Every token-endpoint failure must wrap a distinct sentinel. The CLI maps them
// to separate analytics reasons with errors.Is, so an unwrapped error collapses
// back into the catch-all bucket this split exists to eliminate.
func TestPostTokenForm_ErrorsWrapSentinels(t *testing.T) {
	tests := []struct {
		name    string
		handler func(w http.ResponseWriter, r *http.Request)
		want    error
		notWant error
	}{
		{
			name: "400 rejected code",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			},
			want:    ErrRejected,
			notWant: ErrTokenServerError,
		},
		{
			name: "401 invalid_client blames our config, not the credential",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			},
			want:    ErrTokenClientConfig,
			notWant: ErrRejected,
		},
		{
			name: "400 unsupported_grant_type is also a client error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
			},
			want:    ErrTokenClientConfig,
			notWant: ErrRejected,
		},
		{
			name: "400 invalid_grant is a verified rejection",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			},
			want:    ErrRejected,
			notWant: ErrRejectionUnclassified,
		},
		{
			name: "400 invalid_request is a client error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			},
			want:    ErrTokenClientConfig,
			notWant: ErrRejected,
		},
		{
			name: "401 unauthorized_client is a client error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"unauthorized_client"}`, http.StatusUnauthorized)
			},
			want:    ErrTokenClientConfig,
			notWant: ErrRejected,
		},
		{
			name: "400 invalid_scope is a client error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"invalid_scope"}`, http.StatusBadRequest)
			},
			want:    ErrTokenClientConfig,
			notWant: ErrRejected,
		},
		{
			name: "400 with an unparseable body is an unclassified rejection",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `<html>gateway</html>`, http.StatusBadRequest)
			},
			want:    ErrRejectionUnclassified,
			notWant: ErrTokenClientConfig,
		},
		{
			name: "400 with no error field is an unclassified rejection",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"detail":"nope"}`, http.StatusBadRequest)
			},
			want:    ErrRejectionUnclassified,
			notWant: ErrTokenClientConfig,
		},
		{
			name: "400 with an unrecognized code is an unclassified rejection",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"some_future_code"}`, http.StatusBadRequest)
			},
			want:    ErrRejectionUnclassified,
			notWant: ErrTokenClientConfig,
		},
		{
			name:    "500 is a server error, not a rejection",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
			want:    ErrTokenServerError,
			notWant: ErrRejected,
		},
		{
			name: "503 is a server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			},
			want:    ErrTokenServerError,
			notWant: ErrRejected,
		},
		{
			name:    "403 is neither rejection nor server error",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", http.StatusForbidden) },
			want:    ErrTokenUnexpectedStatus,
			notWant: ErrRejected,
		},
		{
			name:    "429 needs backoff, so it is its own class",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "slow down", http.StatusTooManyRequests) },
			want:    ErrTokenRateLimited,
			notWant: ErrTokenUnexpectedStatus,
		},
		{
			name: "unparseable body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `not json at all`)
			},
			want:    ErrTokenResponseInvalid,
			notWant: ErrRejected,
		},
		{
			name: "200 without access_token",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"token_type":"Bearer","expires_in":3600}`)
			},
			want:    ErrTokenResponseInvalid,
			notWant: ErrTokenNetwork,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idp := newFakeIdP(t)
			idp.tokenHandler = tt.handler
			c := idp.client(t)

			_, err := c.ExchangeAuthorizationCode(context.Background(), "the-code", "the-verifier", "http://127.0.0.1:8080/cb")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want wrapping %v", err, tt.want)
			}
			if errors.Is(err, tt.notWant) {
				t.Errorf("err = %v must NOT match %v — the two reasons drive different actions", err, tt.notWant)
			}
		})
	}
}

// A cancelled context is not an upstream fault and must not report as a network
// failure; the CLI reports them as different reasons.
func TestPostTokenForm_CanceledContextIsNotNetworkError(t *testing.T) {
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"AT"}`)
	}
	c := idp.client(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.ExchangeAuthorizationCode(ctx, "the-code", "the-verifier", "http://127.0.0.1:8080/cb")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrTokenCanceled) {
		t.Errorf("err = %v, want wrapping ErrTokenCanceled", err)
	}
	if errors.Is(err, ErrTokenNetwork) {
		t.Errorf("err = %v must not also match ErrTokenNetwork", err)
	}
}

// An unreachable endpoint is a transport failure, distinct from every
// server-response case above.
func TestPostTokenForm_UnreachableEndpointIsNetworkError(t *testing.T) {
	c := NewClient(WithTokenURL("http://127.0.0.1:1/v1/oauth/token"))

	_, err := c.ExchangeAuthorizationCode(context.Background(), "the-code", "the-verifier", "http://127.0.0.1:8080/cb")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrTokenNetwork) {
		t.Errorf("err = %v, want wrapping ErrTokenNetwork", err)
	}
	if errors.Is(err, ErrRejected) || errors.Is(err, ErrTokenServerError) {
		t.Errorf("err = %v must not match a server-response sentinel", err)
	}
}

// An http.Client.Timeout error satisfies errors.Is(err,
// context.DeadlineExceeded) even when the caller's context is untouched, so a
// classifier that inspected the error chain would report a slow IdP as a user
// cancellation. ctx.Err() is the only signal that actually means "the caller
// gave up".
func TestPostTokenForm_ClientTimeoutIsNetworkNotCanceled(t *testing.T) {
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}
	c := idp.client(t)
	c.HTTPClient = &http.Client{Timeout: 50 * time.Millisecond}

	// Background context: never cancelled, so only Client.Timeout fires.
	_, err := c.ExchangeAuthorizationCode(context.Background(), "the-code", "the-verifier", "http://127.0.0.1:8080/cb")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("precondition failed: Client.Timeout error should match context.DeadlineExceeded, got %v", err)
	}
	if !errors.Is(err, ErrTokenNetwork) {
		t.Errorf("err = %v, want wrapping ErrTokenNetwork", err)
	}
	if errors.Is(err, ErrTokenCanceled) {
		t.Errorf("err = %v must NOT be ErrTokenCanceled — the caller never cancelled", err)
	}
}

// A URL the request builder cannot parse is a programmer error, and the only
// path that produces ErrTokenRequestFailed.
func TestPostTokenForm_UnbuildableRequest(t *testing.T) {
	c := NewClient(WithTokenURL("http://\x7f invalid/v1/oauth/token"))

	_, err := c.ExchangeAuthorizationCode(context.Background(), "the-code", "the-verifier", "http://127.0.0.1:8080/cb")
	if err == nil {
		t.Fatal("expected a request-build error")
	}
	if !errors.Is(err, ErrTokenRequestFailed) {
		t.Errorf("err = %v, want wrapping ErrTokenRequestFailed", err)
	}
	if errors.Is(err, ErrTokenNetwork) {
		t.Errorf("err = %v must not look like a transport failure — nothing was sent", err)
	}
}

// internal/client keys its re-login prompt off ErrRejected, so an unclassified
// rejection MUST keep matching it. Demoting that case would leave a user with a
// dead refresh token and no prompt to re-authenticate — a worse outcome than
// occasionally suggesting an unnecessary re-login.
func TestUnclassifiedRejectionStillMatchesErrRejected(t *testing.T) {
	for _, body := range []string{`<html>gateway</html>`, `{"detail":"nope"}`, `{"error":"some_future_code"}`} {
		t.Run(body, func(t *testing.T) {
			idp := newFakeIdP(t)
			idp.tokenHandler = func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, body, http.StatusBadRequest)
			}
			c := idp.client(t)

			_, err := c.ExchangeAuthorizationCode(context.Background(), "the-code", "the-verifier", "http://127.0.0.1:8080/cb")
			if !errors.Is(err, ErrRejected) {
				t.Errorf("err = %v must still match ErrRejected — internal/client drives re-login off it", err)
			}
			if !errors.Is(err, ErrRejectionUnclassified) {
				t.Errorf("err = %v should also carry ErrRejectionUnclassified for telemetry", err)
			}
		})
	}
}

// The body-read path goes through the same classifier as Do, so cancelling
// mid-read must report as cancellation rather than a transport fault. Serves a
// Content-Length larger than the bytes actually written so ReadAll blocks.
func TestPostTokenForm_CanceledDuringBodyRead(t *testing.T) {
	idp := newFakeIdP(t)
	idp.tokenHandler = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			_, _ = io.WriteString(w, `{"access_token":"AT"`)
			f.Flush()
		}
		time.Sleep(2 * time.Second)
	}
	c := idp.client(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := c.ExchangeAuthorizationCode(ctx, "the-code", "the-verifier", "http://127.0.0.1:8080/cb")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrTokenCanceled) {
		t.Errorf("err = %v, want wrapping ErrTokenCanceled", err)
	}
	if errors.Is(err, ErrTokenNetwork) {
		t.Errorf("err = %v must not also match ErrTokenNetwork", err)
	}
}
