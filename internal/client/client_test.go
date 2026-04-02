package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Do_InjectsHeaders(t *testing.T) {
	var gotAPIKey, gotUserAgent string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("test-key-abc",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithUserAgent("heygen-cli/test"),
	)

	req, _ := http.NewRequest("GET", srv.URL+"/v3/videos", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if gotAPIKey != "test-key-abc" {
		t.Errorf("x-api-key = %q, want %q", gotAPIKey, "test-key-abc")
	}
	if gotUserAgent != "heygen-cli/test" {
		t.Errorf("User-Agent = %q, want %q", gotUserAgent, "heygen-cli/test")
	}
}

func TestClient_Do_SetsContentType(t *testing.T) {
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	// GET without body — no Content-Type
	req, _ := http.NewRequest("GET", srv.URL+"/test", nil)
	resp, _ := c.Do(req)
	resp.Body.Close()
	if gotContentType != "" {
		t.Errorf("GET Content-Type = %q, want empty", gotContentType)
	}
}

func TestClient_Defaults(t *testing.T) {
	c := New("key")
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.userAgent != DefaultUserAgent {
		t.Errorf("userAgent = %q, want %q", c.userAgent, DefaultUserAgent)
	}
	if c.retry.MaxRetries != 2 {
		t.Errorf("retry.MaxRetries = %d, want %d", c.retry.MaxRetries, 2)
	}
	if c.retry.BaseDelay != time.Second {
		t.Errorf("retry.BaseDelay = %v, want %v", c.retry.BaseDelay, time.Second)
	}
	if c.retry.MaxDelay != 30*time.Second {
		t.Errorf("retry.MaxDelay = %v, want %v", c.retry.MaxDelay, 30*time.Second)
	}
	if _, ok := c.httpClient.Transport.(*retryTransport); !ok {
		t.Fatalf("httpClient.Transport = %T, want *retryTransport", c.httpClient.Transport)
	}
}

func TestClient_WithNoRetry(t *testing.T) {
	c := New("key", WithNoRetry())
	if c.retry.MaxRetries != 0 {
		t.Errorf("retry.MaxRetries = %d, want %d", c.retry.MaxRetries, 0)
	}
}

func TestClient_WithHTTPClientPreservesTimeout(t *testing.T) {
	custom := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &stubTransport{},
	}

	c := New("key", WithHTTPClient(custom))
	if c.httpClient.Timeout != 5*time.Second {
		t.Errorf("httpClient.Timeout = %v, want %v", c.httpClient.Timeout, 5*time.Second)
	}
	rt, ok := c.httpClient.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("httpClient.Transport = %T, want *retryTransport", c.httpClient.Transport)
	}
	if _, ok := rt.base.(*stubTransport); !ok {
		t.Fatalf("retryTransport.base = %T, want *stubTransport", rt.base)
	}
}

func TestClient_WithHTTPClientNoMutation(t *testing.T) {
	base := &stubTransport{}
	custom := &http.Client{
		Timeout:   5 * time.Second,
		Transport: base,
	}

	c1 := New("key", WithHTTPClient(custom))
	c2 := New("key", WithHTTPClient(custom))

	if custom.Transport != base {
		t.Fatalf("custom.Transport mutated to %T, want original transport", custom.Transport)
	}

	rt1, ok := c1.httpClient.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("c1.httpClient.Transport = %T, want *retryTransport", c1.httpClient.Transport)
	}
	rt2, ok := c2.httpClient.Transport.(*retryTransport)
	if !ok {
		t.Fatalf("c2.httpClient.Transport = %T, want *retryTransport", c2.httpClient.Transport)
	}
	if rt1.base != base {
		t.Fatalf("c1 retry base = %T, want original transport", rt1.base)
	}
	if rt2.base != base {
		t.Fatalf("c2 retry base = %T, want original transport", rt2.base)
	}
}

func TestDefaultRetryConfig_FromEnv(t *testing.T) {
	t.Setenv("HEYGEN_MAX_RETRIES", "5")

	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 5 {
		t.Errorf("cfg.MaxRetries = %d, want %d", cfg.MaxRetries, 5)
	}
}

type stubTransport struct{}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
