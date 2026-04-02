package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetry_429ThenSuccess_GET(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 1}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls = %d, want %d", got, 2)
	}
}

func TestRetry_429ThenSuccess_POST(t *testing.T) {
	var calls int32
	bodies := make([]string, 0, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 1}))
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"title":"demo"}`))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls = %d, want %d", got, 2)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Fatalf("bodies = %#v, want two identical request bodies", bodies)
	}
}

func TestRetry_500ThenSuccess_GET(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 1}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls = %d, want %d", got, 2)
	}
}

func TestRetry_500NoRetry_POST(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 2}))
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"title":"demo"}`))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want %d", got, 1)
	}
}

func TestRetry_400NoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 2}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want %d", got, 1)
	}
}

func TestRetry_401NoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 2}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want %d", got, 1)
	}
}

func TestRetry_ExhaustedRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 2}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls = %d, want %d", got, 3)
	}
}

func TestRetry_NetworkErrorThenSuccess_GET(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key",
		WithHTTPClient(&http.Client{
			Transport: &failOnceTransport{
				err:  errors.New("temporary network error"),
				base: srv.Client().Transport,
			},
		}),
		WithRetry(RetryConfig{MaxRetries: 1}),
	)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("server calls = %d, want %d", got, 1)
	}
}

func TestRetry_NetworkErrorNoRetry_POST(t *testing.T) {
	c := New("key",
		WithHTTPClient(&http.Client{
			Transport: &failOnceTransport{err: errors.New("temporary network error")},
		}),
		WithRetry(RetryConfig{MaxRetries: 2}),
	)

	req, _ := http.NewRequest(http.MethodPost, "http://example.invalid", strings.NewReader(`{"title":"demo"}`))
	_, err := c.Do(req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	ft, ok := c.httpClient.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("Transport = %T, want *retryTransport", c.httpClient.Transport)
	}
	failOnce, ok := ft.base.(*failOnceTransport)
	if !ok {
		t.Fatalf("retryTransport.base = %T, want *failOnceTransport", ft.base)
	}
	if failOnce.calls != 1 {
		t.Fatalf("transport calls = %d, want %d", failOnce.calls, 1)
	}
}

func TestRetry_RetryAfterSeconds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 1}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)

	start := time.Now()
	resp, err := c.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if elapsed < time.Second {
		t.Fatalf("elapsed = %v, want >= %v", elapsed, time.Second)
	}
}

func TestRetry_RetryAfterHTTPDate(t *testing.T) {
	var calls int32
	delay := 2 * time.Second
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			w.Header().Set("Retry-After", time.Now().Add(delay).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 1}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)

	start := time.Now()
	resp, err := c.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if elapsed < time.Second {
		t.Fatalf("elapsed = %v, want >= %v", elapsed, time.Second)
	}
}

func TestRetry_MaxRetriesZero(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 0}))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want %d", got, 1)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{
		MaxRetries: 1,
		BaseDelay:  2 * time.Second,
		MaxDelay:   2 * time.Second,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	start := time.Now()
	_, err := c.Do(req)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want %v", err, context.Canceled)
	}
	if elapsed >= time.Second {
		t.Fatalf("elapsed = %v, want cancellation before full backoff", elapsed)
	}
}

func TestRetry_BackoffIncreases(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 100 * time.Millisecond, MaxDelay: 30 * time.Second}

	first := backoffDelay(0, cfg)
	second := backoffDelay(1, cfg)
	third := backoffDelay(2, cfg)

	if !(first < second && second < third) {
		t.Fatalf("delays = %v, %v, %v, want strictly increasing", first, second, third)
	}
}

func TestRetry_NoGetBody_SkipsRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New("key", WithHTTPClient(srv.Client()), WithRetry(RetryConfig{MaxRetries: 2}))
	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Body = io.NopCloser(strings.NewReader(`{"title":"demo"}`))
	req.ContentLength = int64(len(`{"title":"demo"}`))
	req.GetBody = nil

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want %d", got, 1)
	}
}

func TestParseRetryAfter_Invalid(t *testing.T) {
	if got := parseRetryAfter("not-a-date"); got != 0 {
		t.Fatalf("parseRetryAfter() = %v, want 0", got)
	}
}

type failOnceTransport struct {
	base  http.RoundTripper
	err   error
	calls int
}

func (t *failOnceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	if t.calls == 1 {
		return nil, t.err
	}
	return t.base.RoundTrip(req)
}

func TestCloneRequestForRetry_PreservesURL(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/v1/videos?limit=1", nil)
	cloned, err := cloneRequestForRetry(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := url.Parse(cloned.URL.String())
	if got.String() != "https://example.com/v1/videos?limit=1" {
		t.Fatalf("cloned URL = %q, want exact URL preserved", got.String())
	}
}
