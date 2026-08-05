package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const (
	deviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	maxDevicePollDelay  = 60 * time.Second
	maxDeviceLifetime   = 30 * time.Minute
)

// DeviceAuthorization is the safe-to-display subset of an RFC 8628
// authorization response. DeviceCode is a bearer secret and must never be
// logged or included in errors.
type DeviceAuthorization struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// DeviceAuthorizationError is a stable, redacted OAuth error from the device
// flow. It deliberately excludes error_description because a buggy upstream
// may echo the device bearer credential there.
type DeviceAuthorizationError struct {
	Code string
}

func (e *DeviceAuthorizationError) Error() string {
	return "oauth device authorization failed: " + e.Code
}

// RequestDeviceAuthorization starts an RFC 8628 flow. The returned
// verification URI is restricted to HTTPS, or loopback HTTP for local tests.
func (c *Client) RequestDeviceAuthorization(
	ctx context.Context,
	scope string,
	resource string,
) (*DeviceAuthorization, error) {
	form := url.Values{"client_id": {c.ClientID}}
	if scope != "" {
		form.Set("scope", scope)
	}
	if resource != "" {
		form.Set("resource", strings.TrimRight(resource, "/"))
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.DeviceAuthorizationURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("oauth: build device authorization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: device authorization request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("oauth: read device authorization response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeRedactedDeviceError(body, resp.StatusCode)
	}
	var result DeviceAuthorization
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.New("oauth: invalid device authorization response")
	}
	if result.DeviceCode == "" || result.UserCode == "" ||
		result.VerificationURI == "" || result.ExpiresIn <= 0 {
		return nil, errors.New("oauth: incomplete device authorization response")
	}
	if !safeUserCode(result.UserCode) {
		return nil, errors.New("oauth: unsafe device user code")
	}
	if result.Interval < 5 {
		result.Interval = 5
	}
	if !safeVerificationURI(result.VerificationURI) {
		return nil, errors.New("oauth: unsafe device verification URI")
	}
	return &result, nil
}

// PollDeviceToken waits according to RFC 8628 until the user approves, denies,
// the code expires, or the caller cancels. The first request is delayed by the
// server-provided interval; slow_down increases subsequent waits by five
// seconds.
func (c *Client) PollDeviceToken(
	ctx context.Context,
	deviceCode string,
	intervalSeconds int,
	expiresInSeconds int,
) (*TokenResponse, error) {
	if deviceCode == "" {
		return nil, errors.New("oauth: device code must be non-empty")
	}
	if intervalSeconds < 5 {
		intervalSeconds = 5
	}
	if expiresInSeconds <= 0 {
		return nil, errors.New("oauth: device authorization lifetime must be positive")
	}
	lifetime := time.Duration(expiresInSeconds) * time.Second
	if lifetime > maxDeviceLifetime {
		lifetime = maxDeviceLifetime
	}
	deadline := c.Now().Add(lifetime)
	delay := time.Duration(intervalSeconds) * time.Second

	for {
		if delay > maxDevicePollDelay {
			delay = maxDevicePollDelay
		}
		if !c.Now().Add(delay).Before(deadline) {
			return nil, &DeviceAuthorizationError{Code: "expired_token"}
		}
		if err := c.Sleep(ctx, delay); err != nil {
			return nil, fmt.Errorf("oauth: device polling canceled: %w", err)
		}

		tok, code, err := c.pollDeviceTokenOnce(ctx, deviceCode)
		if err != nil {
			return nil, err
		}
		switch code {
		case "":
			return tok, nil
		case "authorization_pending":
			continue
		case "slow_down":
			delay += 5 * time.Second
			continue
		default:
			return nil, &DeviceAuthorizationError{Code: code}
		}
	}
}

func (c *Client) pollDeviceTokenOnce(
	ctx context.Context,
	deviceCode string,
) (*TokenResponse, string, error) {
	form := url.Values{
		"grant_type":  {deviceCodeGrantType},
		"device_code": {deviceCode},
		"client_id":   {c.ClientID},
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.TokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, "", fmt.Errorf("oauth: build device token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	requestSent := c.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("oauth: device token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, "", fmt.Errorf("oauth: read device token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
			return nil, payload.Error, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			// Some gateways return a bare 429 instead of the RFC 8628
			// slow_down payload. Preserve the protocol's backoff behavior.
			return nil, "slow_down", nil
		}
		return nil, "", decodeRedactedDeviceError(body, resp.StatusCode)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, "", errors.New("oauth: invalid device token response")
	}
	tok, err := parseTokenResponse(raw)
	if err != nil {
		return nil, "", fmt.Errorf("oauth: invalid device token response: %w", err)
	}
	tok.IssuedAt = requestSent
	return tok, "", nil
}

func decodeRedactedDeviceError(body []byte, status int) error {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		return &DeviceAuthorizationError{Code: payload.Error}
	}
	return fmt.Errorf("oauth: device endpoint returned HTTP %d", status)
}

func safeUserCode(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func safeVerificationURI(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
