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
	DefaultAuthorizeURL           = "https://app.heygen.com/oauth/authorize"
	DefaultDeviceAuthorizationURL = "https://api2.heygen.com/v1/oauth/device_authorization"
	DefaultTokenURL               = "https://api2.heygen.com/v1/oauth/token"
	DefaultRevokeURL              = "https://api2.heygen.com/v1/oauth/revoke"
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
	ClientID               string
	AuthorizeURL           string
	DeviceAuthorizationURL string
	TokenURL               string
	RevokeURL              string

	// HTTPClient is used for the token + revoke round trips. Defaults
	// to an http.Client with sensible timeouts.
	HTTPClient *http.Client
	// Now is the wall-clock source, overridable for deterministic tests.
	Now func() time.Time
	// Sleep waits between RFC 8628 polling attempts. Tests replace it
	// with a deterministic clock advance.
	Sleep func(context.Context, time.Duration) error
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

// WithDeviceAuthorizationURL overrides the RFC 8628 issuance endpoint.
func WithDeviceAuthorizationURL(u string) Option {
	return func(c *Client) { c.DeviceAuthorizationURL = u }
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

// WithSleep overrides the RFC 8628 polling wait.
func WithSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(c *Client) { c.Sleep = sleep }
}

// NewClient returns a Client with sensible defaults, optionally
// overridden by opts.
func NewClient(opts ...Option) *Client {
	c := &Client{
		ClientID:               DefaultClientID,
		AuthorizeURL:           DefaultAuthorizeURL,
		DeviceAuthorizationURL: DefaultDeviceAuthorizationURL,
		TokenURL:               DefaultTokenURL,
		RevokeURL:              DefaultRevokeURL,
		HTTPClient:             &http.Client{Timeout: DefaultExchangeTimeout},
		Now:                    time.Now,
		Sleep:                  sleepContext,
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
//
// state, codeChallenge, and redirectURI MUST be non-empty — an empty
// value would silently produce an unusable authorize URL (state mismatch
// on callback, PKCE rejection at token exchange, IdP returns the user
// to the wrong place). Returns an error rather than papering over the
// caller bug.
func (c *Client) BuildAuthorizationURL(state, codeChallenge, redirectURI, scope string) (string, error) {
	if state == "" {
		return "", errors.New("oauth: BuildAuthorizationURL: state must be non-empty")
	}
	if codeChallenge == "" {
		return "", errors.New("oauth: BuildAuthorizationURL: codeChallenge must be non-empty")
	}
	if redirectURI == "" {
		return "", errors.New("oauth: BuildAuthorizationURL: redirectURI must be non-empty")
	}
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
		return c.AuthorizeURL + "&" + q.Encode(), nil
	}
	return c.AuthorizeURL + "?" + q.Encode(), nil
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
		return nil, fmt.Errorf("%w (%s): %w", ErrTokenRequestFailed, grant, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Stamp IssuedAt at request-send time, not after the response is
	// parsed: the IdP's `expires_in` is relative to *its* clock when it
	// minted the token, which is closer to the moment the request hit
	// the wire than to whenever we finish JSON-decoding. Bias toward
	// "expired slightly sooner than ideal" rather than the opposite —
	// a stale-cached refresh costs a round trip; a stale-cached access
	// token costs an API failure.
	requestSent := c.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, classifyTransportErr(ctx, err, grant)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		// A truncated body is the same family as Do failing, and can equally be
		// caused by the caller cancelling, so it goes through the same classifier.
		return nil, classifyTransportErr(ctx, err, "read "+grant+" response")
	}

	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		// 400/401 is the "rejected" family, but it covers two situations that
		// need opposite responses: the supplied code/refresh token is unusable
		// (re-login fixes it) versus our own client registration or request shape
		// is wrong (re-login cannot). RFC 6749 §5.2 distinguishes them via the
		// `error` field, so read it rather than inferring from the status alone.
		return nil, &TokenError{
			Status:  resp.StatusCode,
			Grant:   grant,
			Body:    truncate(string(body), 500),
			Wrapped: sentinelForOAuthErrorCode(body),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Three classes, three actions: retry a 5xx, back off on a 429, and treat
		// anything else (403, a redirect) as neither.
		sentinel := ErrTokenUnexpectedStatus
		switch {
		case resp.StatusCode >= 500:
			sentinel = ErrTokenServerError
		case resp.StatusCode == http.StatusTooManyRequests:
			sentinel = ErrTokenRateLimited
		}
		return nil, fmt.Errorf("%w: %s endpoint returned HTTP %d: %s",
			sentinel, grant, resp.StatusCode, truncate(string(body), 500))
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode %s response: %w (body: %s)",
			ErrTokenResponseInvalid, grant, err, truncate(string(body), 500))
	}
	tok, err := parseTokenResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s response: %w", ErrTokenResponseInvalid, grant, err)
	}
	tok.IssuedAt = requestSent
	return tok, nil
}

// errRejected wraps 400/401 from the token endpoint. Exposed via
// errors.Is so the PR-2 caller can detect "user needs to log in again"
// without string-matching server bodies.
var errRejected = errors.New("oauth: credential rejected by token endpoint")

// TokenError is returned by ExchangeAuthorizationCode / RefreshAccessToken
// when the IdP returns 400 or 401. Wrapped carries the classification: either
// ErrTokenClientConfig (our request or registration is wrong, re-login will not
// help) or ErrRejected, optionally alongside ErrRejectionUnclassified when the
// body stated no reason. errors.Is(err, ErrRejected) remains the signal for a
// re-login UX.
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

// ErrRejected is the sentinel for a 400/401 from the token endpoint whose error
// code blames the supplied credential (or that carries no usable code).
var ErrRejected = errRejected

// clientConfigErrorCodes are the RFC 6749 §5.2 codes that indicate the request
// or the client itself is at fault, not the credential being presented.
// invalid_grant is deliberately absent: that one IS the credential.
var clientConfigErrorCodes = map[string]bool{
	"invalid_request":        true,
	"invalid_client":         true,
	"unauthorized_client":    true,
	"unsupported_grant_type": true,
	"invalid_scope":          true,
}

// ErrRejectionUnclassified marks a 400/401 that could not be attributed to a
// specific RFC 6749 §5.2 code — an unparseable body, a missing `error` field, or
// a well-formed code outside the six core ones — so "the credential was
// refused" is inferred from the status rather than stated by the IdP.
//
// It is wrapped ALONGSIDE ErrRejected, not instead of it. internal/client keys
// its re-login prompt off ErrRejected, and a 400/401 from a token endpoint
// overwhelmingly does mean a refused credential — gateway and infrastructure
// failures arrive as 5xx, which is classified separately. Demoting this case
// out of ErrRejected would leave a user holding a dead refresh token with no
// prompt to re-authenticate, which is a worse failure than occasionally
// suggesting a re-login that turns out to be unnecessary. The marker exists so
// telemetry can separate a verified invalid_grant from an inferred rejection
// without changing that behavior.
var ErrRejectionUnclassified = errRejectionUnclassified

var errRejectionUnclassified = errors.New("oauth: rejection reason not stated by the IdP")

// sentinelForOAuthErrorCode picks the 400/401 sentinel from the response body's
// RFC 6749 §5.2 `error` field.
func sentinelForOAuthErrorCode(body []byte) error {
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == "" {
		return fmt.Errorf("%w: %w", errRejected, errRejectionUnclassified)
	}
	if clientConfigErrorCodes[parsed.Error] {
		return ErrTokenClientConfig
	}
	if parsed.Error == "invalid_grant" {
		return errRejected
	}
	// A well-formed but unrecognized code: still a rejection for behavior
	// purposes, still not one we can attribute.
	return fmt.Errorf("%w: %w", errRejected, errRejectionUnclassified)
}

// classifyTransportErr distinguishes the caller giving up from the transport
// failing. It reads ctx.Err() rather than inspecting err, because an
// http.Client.Timeout error also satisfies errors.Is(err,
// context.DeadlineExceeded) while the caller's context is still live — matching
// on the chain would report a slow IdP as a user cancellation.
func classifyTransportErr(ctx context.Context, err error, what string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w (%s): %w", ErrTokenCanceled, what, err)
	}
	return fmt.Errorf("%w (%s): %w", ErrTokenNetwork, what, err)
}

// Sentinels for the token-endpoint failures that are NOT a credential
// rejection. ErrRejected already covers 400/401; these cover the rest, so a
// caller can tell "retry this" from "log in again" from "the IdP is broken"
// instead of collapsing all of them into one bucket.
//
// Three mutually exclusive behavioral classes: transient (network, canceled,
// server error, rate limited), terminal-for-the-client (client config, response
// invalid, request failed), and credential-rejected (ErrRejected). Every
// non-nil error out of postTokenForm wraps exactly one of them — with one
// deliberate overlap: an unattributable rejection wraps ErrRejected AND
// ErrRejectionUnclassified, so behavior stays put while telemetry can tell them
// apart. See ErrRejectionUnclassified.
var (
	// ErrTokenRequestFailed means the request could not even be built
	// (malformed token URL). Programmer error, not a runtime condition.
	ErrTokenRequestFailed = errors.New("oauth: could not build token request")

	// ErrTokenNetwork means the request never completed: DNS, TLS, connection
	// reset, or a truncated response body. Retryable.
	ErrTokenNetwork = errors.New("oauth: token request did not complete")

	// ErrTokenCanceled means the CALLER's context ended mid-exchange (Ctrl-C or
	// an agent-imposed deadline). Nothing is wrong upstream, which is why it is
	// separate from ErrTokenNetwork. Determined from ctx.Err(), never from the
	// returned error: an http.Client.Timeout error also satisfies
	// errors.Is(err, context.DeadlineExceeded) even when the caller's context is
	// perfectly healthy, so matching on the chain would mislabel it.
	ErrTokenCanceled = errors.New("oauth: token request canceled by caller")

	// ErrTokenServerError is a 5xx from the token endpoint. Retryable.
	ErrTokenServerError = errors.New("oauth: token endpoint server error")

	// ErrTokenRateLimited is a 429. Retryable, but only with backoff — which is
	// why it is not folded into ErrTokenUnexpectedStatus.
	ErrTokenRateLimited = errors.New("oauth: token endpoint rate limited")

	// ErrTokenUnexpectedStatus is any other non-2xx that is not 400/401, 429, or
	// 5xx — e.g. 403 or a redirect.
	ErrTokenUnexpectedStatus = errors.New("oauth: token endpoint returned an unexpected status")

	// ErrTokenClientConfig is a 400/401 whose RFC 6749 §5.2 error code blames the
	// client or the request rather than the supplied credential (invalid_client,
	// unauthorized_client, invalid_request, unsupported_grant_type,
	// invalid_scope). Split from ErrRejected because re-authenticating cannot fix
	// it — the CLI's own registration or request shape is wrong.
	ErrTokenClientConfig = errors.New("oauth: token request rejected as malformed or unauthorized client")

	// ErrTokenResponseInvalid means a 2xx body that could not be decoded or was
	// missing required fields. The IdP is misbehaving; retrying will not help.
	ErrTokenResponseInvalid = errors.New("oauth: token response invalid")
)

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
	// We decode with the default json package settings (no
	// Decoder.UseNumber), so every JSON number arrives as float64 — no
	// json.Number branch needed.
	switch n := v.(type) {
	case float64:
		return n, true
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
