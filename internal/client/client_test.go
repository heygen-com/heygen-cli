package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
}
