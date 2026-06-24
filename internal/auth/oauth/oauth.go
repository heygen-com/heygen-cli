package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Shared OAuth client_id with hyperframes-CLI. Both CLIs are public
// clients on the same HeyGen IdP and use the same baked-in id; the
// HyperFrames CLI's value is the authoritative one and must not drift.
//
// Source: hyperframes-oss packages/cli/src/auth/oauth.ts:DEFAULT_CLIENT_ID.
const DefaultClientID = "q2A2QRSke2LrFTPJhoDbHtXh"

// Default scopes requested on the authorize URL. Matches the
// hyperframes-CLI default; the heygen-cli surface needs the same
// minimal openid/profile/email set for now.
const DefaultScopes = "openid profile email"

// Default endpoints. The authorize endpoint lives on the consumer
// origin (app.heygen.com — same origin as the user's web session
// cookies); token + revoke live on the api2.heygen.com server-to-server
// API. See hyperframes-CLI's oauth.ts for the live verification notes.
const (
	DefaultAuthorizeURL = "https://app.heygen.com/oauth/authorize"
	DefaultTokenURL     = "https://api2.heygen.com/v1/oauth/token"
	DefaultRevokeURL    = "https://api2.heygen.com/v1/oauth/revoke"
)

// DefaultExchangeTimeout caps each token-endpoint round trip.
const DefaultExchangeTimeout = 30 * time.Second

// DefaultRevokeTimeout caps the best-effort revoke call. RFC 7009
// allows servers to be slow; we don't want logout to hang on it.
const DefaultRevokeTimeout = 5 * time.Second

// Client drives the OAuth 2.0 + PKCE flow against the HeyGen IdP.
//
// All four endpoints (Authorize/Token/Revoke) and the client_id are
// overridable so tests can swap in an httptest.Server. Production
// callers should call NewClient() with no options.
type Client struct {
	ClientID     string
	AuthorizeURL string
	TokenURL     string
	RevokeURL    string

	// HTTPClient is used for the token + revoke round trips. Defaults
	// to an http.Client with sensible timeouts.
	HTTPClient *http.Client
	// Now is the wall-clock source, overridable for deterministic tests.
	Now func() time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithClientID overrides the OAuth client_id.
func WithClientID(id string) Option {
	return func(c *Client) { c.ClientID = id }
}

// WithAuthorizeURL overrides the authorize endpoint URL.
func WithAuthorizeURL(u string) Option {
	return func(c *Client) { c.AuthorizeURL = u }
}

// WithTokenURL overrides the token endpoint URL.
func WithTokenURL(u string) Option {
	return func(c *Client) { c.TokenURL = u }
}

// WithRevokeURL overrides the revoke endpoint URL.
func WithRevokeURL(u string) Option {
	return func(c *Client) { c.RevokeURL = u }
}

// WithHTTPClient injects a custom *http.Client (required for httptest).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.HTTPClient = hc }
}

// WithNow overrides the wall-clock source. Tests use this so
// TokenResponse.IssuedAt is deterministic.
func WithNow(now func() time.Time) Option {
	return func(c *Client) { c.Now = now }
}

// NewClient returns a Client with sensible defaults, optionally
// overridden by opts.
func NewClient(opts ...Option) *Client {
	c := &Client{
		ClientID:     DefaultClientID,
		AuthorizeURL: DefaultAuthorizeURL,
		TokenURL:     DefaultTokenURL,
		RevokeURL:    DefaultRevokeURL,
		HTTPClient:   &http.Client{Timeout: DefaultExchangeTimeout},
		Now:          time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BuildAuthorizationURL composes the consent-screen URL with the given
// state, PKCE code challenge, redirect URI, and scope.
//
// The challenge must be the S256-hashed verifier (see GeneratePKCEPair).
func (c *Client) BuildAuthorizationURL(state, codeChallenge, redirectURI, scope string) string {
	if scope == "" {
		scope = DefaultScopes
	}
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {scope},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	if strings.Contains(c.AuthorizeURL, "?") {
		return c.AuthorizeURL + "&" + q.Encode()
	}
	return c.AuthorizeURL + "?" + q.Encode()
}

// ExchangeAuthorizationCode POSTs the authorization code + PKCE verifier
// to the token endpoint, returning the parsed TokenResponse.
//
// RFC 6749 §4.1.3 requires the redirect_uri here be byte-identical to
// the value used on the authorize hop, so callers should pass the URI
// the loopback server bound to (see LoopbackResult.RedirectURI), not
// reconstruct it from the local socket later.
func (c *Client) ExchangeAuthorizationCode(
	ctx context.Context,
	code, codeVerifier, redirectURI string,
) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {c.ClientID},
		"code_verifier": {codeVerifier},
	}
	return c.postTokenForm(ctx, form, "authorization_code")
}

// RefreshAccessToken POSTs grant_type=refresh_token to the token
// endpoint. Returns the new TokenResponse; per RFC 6749 §6 the server
// may omit refresh_token (no rotation), so callers should preserve the
// prior refresh_token in that case.
func (c *Client) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if refreshToken == "" {
		return nil, errors.New("oauth: refresh token must be non-empty")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.ClientID},
	}
	return c.postTokenForm(ctx, form, "refresh_token")
}

// RevokeToken POSTs a best-effort revoke (RFC 7009) for the supplied
// token. A hung or unreachable IdP MUST NOT block local logout, so this
// method swallows network/transport failures and returns nil for them.
// Only programmer-error inputs (empty token, malformed URL) return a
// non-nil error.
func (c *Client) RevokeToken(ctx context.Context, token string) error {
	if token == "" {
		return errors.New("oauth: token must be non-empty")
	}
	form := url.Values{
		"token":     {token},
		"client_id": {c.ClientID},
	}

	revokeCtx, cancel := context.WithTimeout(ctx, DefaultRevokeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(revokeCtx, http.MethodPost, c.RevokeURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("oauth: build revoke request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		// Best-effort: network/timeout/cancel — swallow.
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	// Even on non-2xx we consider the local logout authoritative; the
	// caller will wipe the on-disk credentials regardless. We drain the
	// body to allow connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// postTokenForm is the shared POST + decode for the token endpoint.
// `grant` is used only for error context.
func (c *Client) postTokenForm(
	ctx context.Context,
	form url.Values,
	grant string,
) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: build %s request: %w", grant, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: %s request: %w", grant, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth: read %s response: %w", grant, err)
	}

	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		// 400/401 here is the "the code/refresh was rejected" path. The
		// PR-2 CLI surface will map this to a "log in again" UX; for now
		// we surface a typed error with the server detail attached.
		return nil, &TokenError{
			Status:  resp.StatusCode,
			Grant:   grant,
			Body:    truncate(string(body), 500),
			Wrapped: errRejected,
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth: %s endpoint returned HTTP %d: %s",
			grant, resp.StatusCode, truncate(string(body), 500))
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("oauth: decode %s response: %w (body: %s)",
			grant, err, truncate(string(body), 500))
	}
	tok, err := parseTokenResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("oauth: %s response invalid: %w", grant, err)
	}
	tok.IssuedAt = c.Now()
	return tok, nil
}

// errRejected wraps 400/401 from the token endpoint. Exposed via
// errors.Is so the PR-2 caller can detect "user needs to log in again"
// without string-matching server bodies.
var errRejected = errors.New("oauth: credential rejected by token endpoint")

// TokenError is returned by ExchangeAuthorizationCode / RefreshAccessToken
// when the IdP returns 400 or 401. Callers can use errors.Is(err, ErrRejected)
// to drive a re-login UX.
type TokenError struct {
	Status  int
	Grant   string
	Body    string
	Wrapped error
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("oauth: %s rejected (HTTP %d): %s", e.Grant, e.Status, e.Body)
}

func (e *TokenError) Unwrap() error { return e.Wrapped }

// ErrRejected is the sentinel for a 400/401 from the token endpoint.
var ErrRejected = errRejected

func parseTokenResponse(obj map[string]any) (*TokenResponse, error) {
	accessToken, ok := stringField(obj, "access_token")
	if !ok || accessToken == "" {
		return nil, errors.New("missing access_token")
	}
	if !isHeaderSafe(accessToken) {
		return nil, errors.New("access_token contains control characters")
	}
	tok := &TokenResponse{AccessToken: accessToken}
	if refresh, ok := stringField(obj, "refresh_token"); ok && refresh != "" {
		if !isHeaderSafe(refresh) {
			return nil, errors.New("refresh_token contains control characters")
		}
		tok.RefreshToken = refresh
	}
	if tt, ok := stringField(obj, "token_type"); ok {
		tok.TokenType = tt
	}
	if scope, ok := stringField(obj, "scope"); ok {
		tok.Scope = scope
	}
	if exp, ok := numericField(obj, "expires_in"); ok {
		tok.ExpiresIn = int(exp)
	}
	return tok, nil
}

func stringField(obj map[string]any, key string) (string, bool) {
	v, ok := obj[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func numericField(obj map[string]any, key string) (float64, bool) {
	v, ok := obj[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		// Spec says expires_in is numeric, but defend against IdPs that
		// send it as a quoted string.
		var f float64
		if _, err := fmt.Sscanf(n, "%f", &f); err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// isHeaderSafe returns false if s contains any byte that would corrupt
// an HTTP header (\r, \n, NUL). Protects against credential payloads
// that the user wouldn't want propagated to an Authorization header.
func isHeaderSafe(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case 0, '\r', '\n':
			return false
		}
	}
	return true
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
