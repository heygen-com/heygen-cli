package client

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestExecute_GETWithQueryParams(t *testing.T) {
	var gotPath, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/videos", Method: "GET"}
	inv := &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: url.Values{"limit": {"10"}, "folder_id": {"abc123"}},
	}

	result, err := c.Execute(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/v3/videos" {
		t.Errorf("path = %q, want %q", gotPath, "/v3/videos")
	}
	if gotQuery == "" {
		t.Fatal("expected query params, got empty")
	}

	var parsed map[string]any
	if jsonErr := json.Unmarshal(result, &parsed); jsonErr != nil {
		t.Errorf("response is not valid JSON: %v", jsonErr)
	}
}

func TestExecute_RepeatedQueryParams(t *testing.T) {
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/videos", Method: "GET"}
	inv := &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: url.Values{"status": {"completed", "failed"}},
	}

	_, err := c.Execute(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !(strings.Contains(gotQuery, "status=completed") && strings.Contains(gotQuery, "status=failed")) {
		t.Errorf("query = %q, want both status=completed and status=failed", gotQuery)
	}
}

func TestExecute_PathParamSubstitution(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc123"}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/videos/{video_id}", Method: "GET"}
	inv := &command.Invocation{
		PathParams:  map[string]string{"video_id": "abc123"},
		QueryParams: make(url.Values),
	}

	_, err := c.Execute(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/v3/videos/abc123" {
		t.Errorf("path = %q, want %q", gotPath, "/v3/videos/abc123")
	}
}

func TestExecute_POSTWithBody(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"new123"}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/videos", Method: "POST", BodyEncoding: "json"}
	inv := &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
		Body:        map[string]any{"title": "My Video", "draft": true},
	}

	_, err := c.Execute(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["title"] != "My Video" {
		t.Errorf("body.title = %v, want %q", gotBody["title"], "My Video")
	}
	if gotBody["draft"] != true {
		t.Errorf("body.draft = %v, want true", gotBody["draft"])
	}
}

func TestExecute_NilBodySendsNoContent(t *testing.T) {
	var gotContentLength int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentLength = r.ContentLength
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/avatars", Method: "POST"}
	inv := &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
		// Body is nil — no body should be sent
	}

	_, err := c.Execute(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContentLength > 0 {
		t.Errorf("ContentLength = %d, want 0 (no body)", gotContentLength)
	}
}

func TestExecute_ErrorEnvelope_400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_456")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_parameter","message":"limit must be positive"}}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/videos", Method: "GET"}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.Execute(spec, inv)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.ExitCode != clierrors.ExitGeneral {
		t.Errorf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitGeneral)
	}
	if cliErr.Code != "invalid_parameter" {
		t.Errorf("Code = %q, want %q", cliErr.Code, "invalid_parameter")
	}
	if cliErr.RequestID != "req_456" {
		t.Errorf("RequestID = %q, want %q", cliErr.RequestID, "req_456")
	}
}

func TestExecute_ErrorEnvelope_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"invalid API key"}}`))
	}))
	defer srv.Close()

	c := New("bad-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/videos", Method: "GET"}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.Execute(spec, inv)

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.ExitCode != clierrors.ExitAuth {
		t.Errorf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitAuth)
	}
}

func TestExecute_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/videos", Method: "GET"}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.Execute(spec, inv)

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != "network_error" {
		t.Errorf("Code = %q, want %q", cliErr.Code, "network_error")
	}
}

func TestExecute_MultipartNotImplemented(t *testing.T) {
	c := New("key")

	spec := &command.Spec{Endpoint: "/v3/assets", Method: "POST", BodyEncoding: "multipart"}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.Execute(spec, inv)

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.ExitCode != clierrors.ExitUsage {
		t.Errorf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitUsage)
	}
}

func TestExecute_MethodRequired(t *testing.T) {
	c := New("key")

	spec := &command.Spec{Endpoint: "/v3/videos"}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.Execute(spec, inv)
	if err == nil {
		t.Fatal("expected error for empty Method, got nil")
	}
}
