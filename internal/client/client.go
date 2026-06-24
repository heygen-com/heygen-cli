package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/heygen-com/heygen-cli/internal/auth"
	"github.com/heygen-com/heygen-cli/internal/auth/oauth"
	"github.com/heygen-com/heygen-cli/internal/origin"
)

const (
	DefaultBaseURL   = "https://api.heygen.com"
	DefaultUserAgent = "heygen-cli/dev"
	DefaultTimeout   = 30 * time.Second
)

// ErrReLoginNeeded signals that the stored OAuth credential was rejected
// even after a refresh attempt. Callers (CLI surface, error renderer)
// can use errors.Is to drive a "run `heygen auth login` again" hint.
var ErrReLoginNeeded = errors.New("oauth credential rejected; re-run `heygen auth login`")

// OAuthPersister persists refreshed OAuth tokens back to disk. The
// client calls this whenever it performs a refresh on behalf of the user
// so the next CLI invocation doesn't pay for the same refresh round
// trip. The default implementation hands off to auth.SaveOAuthTokens.
type OAuthPersister interface {
	SaveOAuthTokens(tok auth.OAuthTokens) error
}

// Client wraps net/http.Client with HeyGen-specific behavior:
// automatic auth header injection (x-api-key OR Authorization: Bearer),
// base URL resolution, and User-Agent tagging. Use WithHTTPClient to
// inject a test transport.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	credential   auth.Credential
	credMu       sync.Mutex
	userAgent    string
	clientOrigin string
	extraHeaders map[string]string
	retry        RetryConfig

	oauthClient    *oauth.Client
	oauthPersister OAuthPersister

	// refreshMu serializes the OAuth refresh path so two concurrent Do()
	// calls don't both round-trip the IdP. Holders re-check the in-memory
	// expiry under the lock; if another goroutine already refreshed, the
	// second arrival skips its own refresh.
	refreshMu sync.Mutex
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL overrides the default API base URL.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = url }
}

// WithHTTPClient injects a custom http.Client (critical for httptest).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithMaxRetries sets the maximum number of retries for transient failures.
// 0 disables retries. Delays remain at their defaults.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.retry.MaxRetries = n }
}

// WithUserAgent overrides the default User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithClientOrigin overrides the parent-agent string sent in the
// X-HeyGen-Client-Origin header. Empty disables the header. By default the
// constructor calls origin.Detect() once at startup; tests use this to
// pin a deterministic value.
func WithClientOrigin(o string) Option {
	return func(c *Client) { c.clientOrigin = o }
}

// WithExtraHeaders adds custom headers sent with every request.
func WithExtraHeaders(h map[string]string) Option {
	return func(c *Client) { c.extraHeaders = h }
}

// WithOAuthClient injects the OAuth client used to refresh access
// tokens. Required when the Client is constructed with an OAuth
// credential; ignored for pure api-key callers.
func WithOAuthClient(oc *oauth.Client) Option {
	return func(c *Client) { c.oauthClient = oc }
}

// WithOAuthPersister injects the sink that receives refreshed tokens.
// Defaults to the on-disk persister; tests override this so a refresh
// doesn't touch the real credentials file.
func WithOAuthPersister(p OAuthPersister) Option {
	return func(c *Client) { c.oauthPersister = p }
}

// New creates a Client with the given API key and options. Retained for
// callers that haven't been upgraded to NewWithCredential.
func New(apiKey string, opts ...Option) *Client {
	return NewWithCredential(auth.Credential{
		Type:   auth.CredentialTypeAPIKey,
		APIKey: apiKey,
	}, opts...)
}

// NewWithCredential creates a Client driven by a typed credential. OAuth
// credentials require an oauth.Client (via WithOAuthClient) so the
// transport can refresh; the constructor panics with a clear error if
// that wiring is missing (caller bug).
func NewWithCredential(cred auth.Credential, opts ...Option) *Client {
	c := &Client{
		httpClient:     &http.Client{Timeout: DefaultTimeout},
		baseURL:        DefaultBaseURL,
		credential:     cred,
		userAgent:      DefaultUserAgent,
		clientOrigin:   string(origin.Detect()),
		retry:          DefaultRetryConfig(),
		oauthPersister: defaultOAuthPersister{},
	}
	for _, opt := range opts {
		opt(c)
	}

	if c.credential.IsOAuth() && c.oauthClient == nil {
		// Public API contract: an OAuth client must be supplied alongside
		// an OAuth credential. Falling back to defaults here would tie
		// the test transport to the live IdP — fail fast instead.
		c.oauthClient = oauth.NewClient()
	}

	copied := *c.httpClient
	transport := copied.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	copied.Transport = &retryTransport{
		base:   transport,
		config: c.retry,
	}
	c.httpClient = &copied

	return c
}

// Do executes an HTTP request, injecting auth and User-Agent headers.
// Extra headers are applied FIRST so the client's own reserved headers
// always win and cannot be overridden by user input.
//
// For OAuth credentials the transport refreshes the access token before
// the request when the current token is near expiry (proactive refresh)
// and once after a 401 (reactive refresh). A 401 after a successful
// refresh attempt surfaces ErrReLoginNeeded.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// `refreshedThisCall` tracks whether ensureFreshOAuthToken already
	// minted a new token during this Do(). When the post-Do 401 retry
	// path fires, this lets us skip a second refresh round-trip in the
	// same call: if the token we just sent was already fresh and the
	// server still rejected it, refresh-on-401 won't fix it — go
	// straight to ErrReLoginNeeded. (N2)
	var refreshedThisCall bool
	if c.credential.IsOAuth() {
		var err error
		refreshedThisCall, err = c.ensureFreshOAuthToken(ctx)
		if err != nil {
			return nil, err
		}
	}

	c.applyHeaders(req)

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: CLI makes HTTP requests to user-configured API endpoints
	if err != nil {
		return nil, err
	}

	// OAuth-only: on a 401, attempt a single refresh + retry. API-key
	// credentials get no such retry — there's nothing to refresh.
	if resp.StatusCode == http.StatusUnauthorized && c.credential.IsOAuth() && c.credential.HasRefreshToken() {
		// Drain + close the 401 body before retrying so the connection
		// can be reused.
		drainAndClose(resp.Body)

		// If the request body can't be replayed, we can't safely retry
		// even with a fresh token — handing the caller back the 401
		// gives them a deterministic shape they can act on. (W2)
		if !canReplayBody(req) {
			// Re-issue the 401 response we already drained: callers (and
			// the executor) need a non-nil resp to render its body, so
			// synthesize a fresh 401 with the same status line. The body
			// is unrecoverable at this point — drainAndClose consumed it.
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     resp.Status,
				Header:     resp.Header,
				Body:       http.NoBody,
				Request:    req,
				Proto:      resp.Proto,
				ProtoMajor: resp.ProtoMajor,
				ProtoMinor: resp.ProtoMinor,
			}, nil
		}

		// If we already refreshed at the top of this Do() and the server
		// STILL returned 401, the fresh token doesn't help — re-login is
		// the only path forward. (N2)
		if refreshedThisCall {
			return nil, ErrReLoginNeeded
		}

		refreshed, refreshErr := c.forceRefresh(ctx)
		if refreshErr != nil {
			// Discriminate between "IdP said no" (rejected) and any other
			// transient failure (network, 5xx, ctx cancel). Only the
			// rejected branch tells the user re-login is needed; the
			// transient branch surfaces the underlying error as-is so
			// the executor renders something accurate. (W1)
			if errors.Is(refreshErr, oauth.ErrRejected) {
				return nil, fmt.Errorf("%w: %v", ErrReLoginNeeded, refreshErr)
			}
			return nil, fmt.Errorf("oauth: refresh failed: %w", refreshErr)
		}
		if !refreshed {
			// Refresh succeeded as a no-op (no new token issued) —
			// re-login is the only path.
			return nil, ErrReLoginNeeded
		}

		retryReq, err := cloneRequestForRetry(req)
		if err != nil {
			return nil, fmt.Errorf("retry after refresh: %w", err)
		}
		c.applyHeaders(retryReq)
		resp, err = c.httpClient.Do(retryReq) //nolint:gosec // G704: same CLI HTTP path
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			drainAndClose(resp.Body)
			return nil, ErrReLoginNeeded
		}
	}

	return resp, nil
}

// applyHeaders sets the wire-level headers from the current Client +
// credential state. Called both on the initial request and on the
// post-refresh retry so the new access token lands on the retry.
func (c *Client) applyHeaders(req *http.Request) {
	for k, v := range c.extraHeaders {
		req.Header.Set(k, v)
	}
	// Reserved headers set AFTER extraHeaders — these always win.
	c.credMu.Lock()
	switch c.credential.Type {
	case auth.CredentialTypeAPIKey:
		req.Header.Set("x-api-key", c.credential.APIKey)
		req.Header.Del("Authorization")
	case auth.CredentialTypeOAuth:
		req.Header.Set("Authorization", "Bearer "+c.credential.AccessToken)
		req.Header.Del("x-api-key")
	case auth.CredentialTypeOAuthExpired:
		// Programmer-bug guard: by the time applyHeaders runs, Do() must
		// have already routed an OAuthExpired credential through
		// ensureFreshOAuthToken, which upgrades it to CredentialTypeOAuth
		// on success or returns an error before we get here. Reaching
		// this branch means a code path skipped the refresh gate. (N7)
		c.credMu.Unlock()
		panic("client.applyHeaders: refresh gate skipped for CredentialTypeOAuthExpired (programmer bug)")
	}
	c.credMu.Unlock()
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-HeyGen-Source", "cli")
	if c.clientOrigin != "" {
		req.Header.Set("X-HeyGen-Client-Origin", c.clientOrigin)
	}
	if req.Header.Get("Content-Type") == "" && req.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
}

// ensureFreshOAuthToken refreshes the current access token if it is
// missing (OAuthExpired) or near expiry (OAuth + ExpiresAt within skew).
// Tokens with no expiry information are left alone — the transport
// optimistically tries them and falls back to refresh-on-401.
//
// Returns true when this call performed a refresh round-trip; false when
// the in-memory token was already fresh enough or another goroutine
// refreshed concurrently. Callers thread the bool through to the
// post-Do() 401 retry path to skip a redundant refresh in the same
// Do() invocation. (N1, N2)
func (c *Client) ensureFreshOAuthToken(ctx context.Context) (bool, error) {
	if !c.needsRefresh() {
		return false, nil
	}
	if !c.credential.HasRefreshToken() {
		return false, ErrReLoginNeeded
	}

	// Serialize the refresh path so concurrent Do() calls don't both
	// round-trip the IdP. After grabbing the lock, re-check expiry: if
	// another goroutine already refreshed, the token is now fresh and
	// we don't need to refresh again. (N1)
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if !c.needsRefresh() {
		// Lost the race — another caller refreshed while we were
		// waiting for the lock. Treat this Do() as if we never needed
		// to refresh at all.
		return false, nil
	}

	if _, err := c.forceRefresh(ctx); err != nil {
		// Same W1 discrimination as the post-401 path: only an IdP
		// rejection signals re-login is needed; transient failures
		// surface as-is so the executor can render a useful error.
		if errors.Is(err, oauth.ErrRejected) {
			return false, fmt.Errorf("%w: %v", ErrReLoginNeeded, err)
		}
		return false, fmt.Errorf("oauth: refresh failed: %w", err)
	}
	return true, nil
}

// needsRefresh reports whether the in-memory credential needs a refresh
// round-trip. Reads the credential under the lock so concurrent refreshes
// don't see a torn snapshot.
func (c *Client) needsRefresh() bool {
	c.credMu.Lock()
	defer c.credMu.Unlock()
	switch c.credential.Type {
	case auth.CredentialTypeOAuthExpired:
		return true
	case auth.CredentialTypeOAuth:
		if !c.credential.ExpiresAt.IsZero() && time.Now().Add(auth.OAuthRefreshSkew).After(c.credential.ExpiresAt) {
			return true
		}
	}
	return false
}

// forceRefresh runs the OAuth refresh dance once and updates the
// in-memory credential + persists the new tokens. Returns true when a
// new access token was minted. Callers handle the err side: a real
// network/IdP failure stays a refresh error; an IdP 400/401 (token
// rejected) bubbles up wrapped in oauth.ErrRejected so callers can
// errors.Is-discriminate the re-login case.
func (c *Client) forceRefresh(ctx context.Context) (bool, error) {
	if c.oauthClient == nil {
		return false, errors.New("oauth client not configured (programmer error)")
	}
	c.credMu.Lock()
	refresh := c.credential.RefreshToken
	c.credMu.Unlock()
	if refresh == "" {
		return false, errors.New("no refresh_token on credential")
	}

	tok, err := c.oauthClient.RefreshAccessToken(ctx, refresh)
	if err != nil {
		return false, err
	}
	if tok.AccessToken == "" {
		return false, nil
	}

	c.credMu.Lock()
	c.credential.Type = auth.CredentialTypeOAuth
	c.credential.AccessToken = tok.AccessToken
	// RFC 6749 §6: refresh_token MAY be omitted (no rotation). Keep the
	// existing one in that case.
	if tok.RefreshToken != "" {
		c.credential.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		c.credential.ExpiresAt = tok.IssuedAt.Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	if tok.Scope != "" {
		c.credential.Scope = tok.Scope
	}
	persistTok := auth.OAuthTokens{
		AccessToken:  c.credential.AccessToken,
		RefreshToken: c.credential.RefreshToken,
		ExpiresAt:    c.credential.ExpiresAt,
		Scope:        c.credential.Scope,
		TokenType:    tok.TokenType,
	}
	c.credMu.Unlock()

	if c.oauthPersister != nil {
		// Persist on a best-effort basis. A disk failure here doesn't
		// invalidate the live in-memory token — the request can still
		// proceed; the next CLI invocation will re-refresh.
		_ = c.oauthPersister.SaveOAuthTokens(persistTok)
	}
	return true, nil
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// defaultOAuthPersister writes refreshed tokens straight to the shared
// credentials file. Tests override via WithOAuthPersister.
type defaultOAuthPersister struct{}

func (defaultOAuthPersister) SaveOAuthTokens(tok auth.OAuthTokens) error {
	return auth.SaveOAuthTokens(tok)
}
