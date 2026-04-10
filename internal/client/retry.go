package client

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"time"
)

// RetryConfig controls retry behavior for transient HTTP failures.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig returns the default retry policy, optionally overridden by
// the HEYGEN_MAX_RETRIES environment variable.
func DefaultRetryConfig() RetryConfig {
	cfg := RetryConfig{
		MaxRetries: 2,
		BaseDelay:  time.Second,
		MaxDelay:   30 * time.Second,
	}

	if raw, ok := os.LookupEnv("HEYGEN_MAX_RETRIES"); ok {
		if maxRetries, err := strconv.Atoi(raw); err == nil && maxRetries >= 0 {
			cfg.MaxRetries = maxRetries
		}
	}

	return cfg
}

// retryTransport wraps an http.RoundTripper with automatic retries for
// transient failures.
type retryTransport struct {
	base   http.RoundTripper
	config RetryConfig
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	currentReq := req
	for attempt := 0; ; attempt++ {
		resp, err := base.RoundTrip(currentReq)
		if attempt >= t.config.MaxRetries || !shouldRetry(currentReq, resp, err) {
			return resp, err
		}

		if !canReplayBody(currentReq) {
			return resp, err
		}

		delay := backoffDelay(attempt, t.config)
		if resp != nil {
			if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After")); retryAfter > delay {
				// Clamp Retry-After to MaxDelay — a server sending Retry-After: 86400
				// shouldn't block the CLI for 24 hours.
				if t.config.MaxDelay > 0 && retryAfter > t.config.MaxDelay {
					retryAfter = t.config.MaxDelay
				}
				delay = retryAfter
			}
			drainAndClose(resp.Body)
		}

		if err := waitForRetry(currentReq.Context(), delay); err != nil {
			return nil, err
		}

		nextReq, err := cloneRequestForRetry(currentReq)
		if err != nil {
			return nil, err
		}
		currentReq = nextReq
	}
}

func shouldRetry(req *http.Request, resp *http.Response, err error) bool {
	if err != nil {
		if req.Context().Err() != nil ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return isIdempotent(req.Method)
	}

	if resp == nil {
		return false
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	// 5xx server errors — only retry for idempotent methods.
	return isRetryableServerError(resp.StatusCode) && isIdempotent(req.Method)
}

// isRetryableServerError returns true for server-side errors worth retrying.
// 429 (rate limited) is handled separately in shouldRetry — it's always retried
// regardless of method because the server explicitly rejected without processing.
func isRetryableServerError(code int) bool {
	switch code {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodDelete,
		http.MethodOptions:
		return true
	default:
		return false
	}
}

func canReplayBody(req *http.Request) bool {
	return req.Body == nil || req.GetBody != nil
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(header); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}

	retryAt, err := http.ParseTime(header)
	if err != nil {
		return 0
	}
	if delay := time.Until(retryAt); delay > 0 {
		return delay
	}
	return 0
}

func backoffDelay(attempt int, config RetryConfig) time.Duration {
	if config.BaseDelay <= 0 {
		return 0
	}

	delay := float64(config.BaseDelay) * math.Pow(2, float64(attempt))
	if config.MaxDelay > 0 && time.Duration(delay) > config.MaxDelay {
		delay = float64(config.MaxDelay)
	}

	jitter := 0.75 + rand.Float64()*0.5 //nolint:gosec // G404: jitter does not need crypto randomness
	return time.Duration(delay * jitter)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cloneRequestForRetry(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	clone.GetBody = req.GetBody
	clone.ContentLength = req.ContentLength

	if req.Body == nil {
		clone.Body = nil
		return clone, nil
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone.Body = body
	return clone, nil
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	// Cap the drain to 4KB — response bodies on 429/5xx should be tiny.
	// A misbehaving server sending a large body won't block the retry loop.
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4096))
	_ = body.Close()
}
