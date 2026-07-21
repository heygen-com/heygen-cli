package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Do_InjectsHeaders(t *testing.T) {
	var gotAPIKey, gotUserAgent, gotSource string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotUserAgent = r.Header.Get("User-Agent")
		gotSource = r.Header.Get("X-HeyGen-Source")
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
	if gotSource != "cli" {
		t.Errorf("X-HeyGen-Source = %q, want %q", gotSource, "cli")
	}
}

func TestClient_Do_SetsClientOrigin(t *testing.T) {
	var gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("X-HeyGen-Client-Origin")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithClientOrigin("claude_code"),
	)

	req, _ := http.NewRequest("GET", srv.URL+"/v3/videos", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if gotOrigin != "claude_code" {
		t.Errorf("X-HeyGen-Client-Origin = %q, want %q", gotOrigin, "claude_code")
	}
}

// Empty origin must produce NO header at all — an empty header would still
// land on the wire and downstream attribution would treat empty string as
// "explicitly unknown" rather than "no signal". They're not the same thing
// for funnel queries.
func TestClient_Do_OmitsClientOriginWhenEmpty(t *testing.T) {
	hadHeader := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadHeader = r.Header["X-Heygen-Client-Origin"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithClientOrigin(""),
	)

	req, _ := http.NewRequest("GET", srv.URL+"/v3/videos", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if hadHeader {
		t.Error("X-HeyGen-Client-Origin header was set despite empty origin")
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

func TestClient_WithMaxRetries(t *testing.T) {
	c := New("key", WithMaxRetries(5))
	if c.retry.MaxRetries != 5 {
		t.Errorf("retry.MaxRetries = %d, want %d", c.retry.MaxRetries, 5)
	}
	// Delays stay at defaults
	if c.retry.BaseDelay != time.Second {
		t.Errorf("retry.BaseDelay = %v, want %v", c.retry.BaseDelay, time.Second)
	}
}

func TestClient_WithMaxRetries_Zero(t *testing.T) {
	c := New("key", WithMaxRetries(0))
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

func TestClient_Do_SetsExtraHeaders(t *testing.T) {
	var gotSource string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSource = r.Header.Get("X-HeyGen-Client-Source")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("key",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithExtraHeaders(map[string]string{"X-HeyGen-Client-Source": "media-use"}),
	)

	req, _ := http.NewRequest("GET", srv.URL+"/v3/test", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if gotSource != "media-use" {
		t.Errorf("X-HeyGen-Client-Source = %q, want %q", gotSource, "media-use")
	}
}

func TestClient_Do_ExtraHeadersCannotOverrideReserved(t *testing.T) {
	var gotKey, gotUA, gotSource string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotUA = r.Header.Get("User-Agent")
		gotSource = r.Header.Get("X-HeyGen-Source")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("real-key",
		WithBaseURL(srv.URL),
		WithHTTPClient(srv.Client()),
		WithExtraHeaders(map[string]string{
			"x-api-key":      "evil",
			"User-Agent":     "evil-agent",
			"X-HeyGen-Source": "evil",
		}),
	)

	req, _ := http.NewRequest("GET", srv.URL+"/v3/test", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if gotKey != "real-key" {
		t.Errorf("x-api-key = %q, want %q (reserved must not be overridden)", gotKey, "real-key")
	}
	if gotUA == "evil-agent" {
		t.Error("User-Agent was overridden by extraHeaders")
	}
	if gotSource != "cli" {
		t.Errorf("X-HeyGen-Source = %q, want %q", gotSource, "cli")
	}
}

func TestNewWithCredential_DefaultTimeout(t *testing.T) {
	c := New("key")
	if c.httpClient.Timeout != DefaultTimeout {
		t.Errorf("timeout = %v, want default %v", c.httpClient.Timeout, DefaultTimeout)
	}
}

func TestSetTimeout(t *testing.T) {
	c := New("key")
	c.SetTimeout(2 * time.Minute)
	if c.httpClient.Timeout != 2*time.Minute {
		t.Errorf("timeout = %v, want 2m", c.httpClient.Timeout)
	}
	// Zero is an explicit "no timeout".
	c.SetTimeout(0)
	if c.httpClient.Timeout != 0 {
		t.Errorf("timeout = %v, want 0 (disabled)", c.httpClient.Timeout)
	}
}
