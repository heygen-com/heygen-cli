package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

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

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

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

func TestExecute_RetryOn429(t *testing.T) {
	var calls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"too many requests"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), withFastRetries(1))

	spec := &command.Spec{Endpoint: "/v3/videos", Method: "GET"}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	result, err := c.Execute(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want %d", calls, 2)
	}
	if string(result) != `{"data":[]}` {
		t.Fatalf("result = %s, want %s", result, `{"data":[]}`)
	}
}

func TestExecute_RetryPreservesBody(t *testing.T) {
	var calls int
	var bodies []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"too many requests"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"new123"}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), withFastRetries(1))

	spec := &command.Spec{Endpoint: "/v3/videos", Method: "POST", BodyEncoding: "json"}
	inv := &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
		Body:        map[string]any{"title": "My Video", "draft": true},
	}

	result, err := c.Execute(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want %d", calls, 2)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Fatalf("bodies = %#v, want two identical request bodies", bodies)
	}
	if string(result) != `{"id":"new123"}` {
		t.Fatalf("result = %s, want %s", result, `{"id":"new123"}`)
	}
}

func TestExecute_MultipartUpload(t *testing.T) {
	var gotContentType string
	var gotFileContent string
	var gotFileName string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("failed to get form file: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		gotFileName = header.Filename
		data, _ := io.ReadAll(file)
		gotFileContent = string(data)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"asset_id":"asset_123"}}`))
	}))
	defer srv.Close()

	// Create a temp file to upload
	tmpFile, err := os.CreateTemp("", "test-upload-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	_, _ = tmpFile.WriteString("test file content")
	tmpFile.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	spec := &command.Spec{Endpoint: "/v3/assets", Method: "POST", BodyEncoding: "multipart"}
	inv := &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
		FilePath:    tmpFile.Name(),
	}

	result, err := c.Execute(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify multipart content type
	if !strings.Contains(gotContentType, "multipart/form-data") {
		t.Errorf("Content-Type = %q, want multipart/form-data", gotContentType)
	}

	// Verify file was received
	if gotFileContent != "test file content" {
		t.Errorf("file content = %q, want %q", gotFileContent, "test file content")
	}

	// Verify filename is the base name
	if !strings.HasPrefix(gotFileName, "test-upload-") {
		t.Errorf("filename = %q, expected to start with test-upload-", gotFileName)
	}

	// Verify response
	var parsed map[string]any
	if jsonErr := json.Unmarshal(result, &parsed); jsonErr != nil {
		t.Errorf("response is not valid JSON: %v", jsonErr)
	}
}

func TestExecute_MultipartMissingFilePath(t *testing.T) {
	c := New("key")

	spec := &command.Spec{Endpoint: "/v3/assets", Method: "POST", BodyEncoding: "multipart"}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.Execute(spec, inv)
	if err == nil {
		t.Fatal("expected error for missing file path, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
}

func TestExecute_MultipartNonExistentFile(t *testing.T) {
	c := New("key")

	spec := &command.Spec{Endpoint: "/v3/assets", Method: "POST", BodyEncoding: "multipart"}
	inv := &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
		FilePath:    "/tmp/nonexistent-file-abc123.txt",
	}

	_, err := c.Execute(spec, inv)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
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

func TestExecuteAndPoll_ImmediateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/videos":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123"}}`))
		case "/v3/videos/vid_123":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"completed"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	result, err := c.ExecuteAndPoll(context.Background(), pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != `{"data":{"video_id":"vid_123","status":"completed"}}` {
		t.Fatalf("result = %s", result)
	}
}

func TestExecuteAndPoll_PollsUntilComplete(t *testing.T) {
	var statusCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/videos":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123"}}`))
		case "/v3/videos/vid_123":
			statusCalls++
			w.WriteHeader(http.StatusOK)
			switch statusCalls {
			case 1, 2:
				_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"processing"}}`))
			case 3:
				_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"completed"}}`))
			default:
				t.Fatalf("unexpected status call %d", statusCalls)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	result, err := c.ExecuteAndPoll(context.Background(), pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusCalls != 3 {
		t.Fatalf("statusCalls = %d, want 3", statusCalls)
	}
	if string(result) != `{"data":{"video_id":"vid_123","status":"completed"}}` {
		t.Fatalf("result = %s", result)
	}
}

func TestExecuteAndPoll_FailureState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/videos":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123"}}`))
		case "/v3/videos/vid_123":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"failed"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	_, err := c.ExecuteAndPoll(context.Background(), pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var failErr *ErrPollFailed
	if !errors.As(err, &failErr) {
		t.Fatalf("expected *ErrPollFailed, got %T: %v", err, err)
	}
	if failErr.Status != "failed" {
		t.Fatalf("status = %q, want %q", failErr.Status, "failed")
	}
	// Verify the full response is preserved
	if !strings.Contains(string(failErr.Data), `"status":"failed"`) {
		t.Fatalf("data = %s, want failure response", failErr.Data)
	}
}

func TestExecuteAndPoll_TimeoutWhileWaiting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/videos":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123"}}`))
		case "/v3/videos/vid_123":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"processing"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	_, err := c.ExecuteAndPoll(ctx, pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: 50 * time.Millisecond,
		MaxDelay:  50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var timeoutErr *ErrPollTimeout
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected *ErrPollTimeout, got %T", err)
	}
	if timeoutErr.ResourceID != "vid_123" {
		t.Fatalf("ResourceID = %q, want %q", timeoutErr.ResourceID, "vid_123")
	}
	if !strings.Contains(string(timeoutErr.Data), `"status":"processing"`) {
		t.Fatalf("Data = %s, want last processing response", timeoutErr.Data)
	}
}

func TestExecuteAndPoll_TimeoutDuringRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/videos":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123"}}`))
		case "/v3/videos/vid_123":
			<-r.Context().Done()
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	_, err := c.ExecuteAndPoll(ctx, pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var timeoutErr *ErrPollTimeout
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected *ErrPollTimeout, got %T", err)
	}
	if timeoutErr.ResourceID != "vid_123" {
		t.Fatalf("ResourceID = %q, want %q", timeoutErr.ResourceID, "vid_123")
	}
	if timeoutErr.Data != nil {
		t.Fatalf("Data = %s, want nil before first poll response", timeoutErr.Data)
	}
}

func TestExecuteAndPoll_TimeoutDuringCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block the create request until context expires
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	_, err := c.ExecuteAndPoll(ctx, pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != "timeout" {
		t.Fatalf("code = %q, want timeout", cliErr.Code)
	}
	if cliErr.ExitCode != clierrors.ExitTimeout {
		t.Fatalf("ExitCode = %d, want %d (ExitTimeout)", cliErr.ExitCode, clierrors.ExitTimeout)
	}
}

func TestExecuteAndPoll_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/videos":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123"}}`))
		case "/v3/videos/vid_123":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"processing"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	_, err := c.ExecuteAndPoll(ctx, pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: time.Second,
		MaxDelay:  time.Second,
		OnStatus: func(status string, elapsed time.Duration) {
			cancel()
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.Code != "canceled" {
		t.Fatalf("code = %q, want canceled", cliErr.Code)
	}
	if cliErr.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitGeneral)
	}
}

func TestExecuteAndPoll_CreateFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_parameter","message":"bad request"}}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	_, err := c.ExecuteAndPoll(context.Background(), pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.Code != "invalid_parameter" {
		t.Fatalf("code = %q, want invalid_parameter", cliErr.Code)
	}
}

func TestExecuteAndPoll_StatusCallbackCalled(t *testing.T) {
	var statuses []string
	var statusCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/videos":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123"}}`))
		case "/v3/videos/vid_123":
			statusCalls++
			w.WriteHeader(http.StatusOK)
			if statusCalls == 1 {
				_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"processing"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"video_id":"vid_123","status":"completed"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	_, err := c.ExecuteAndPoll(context.Background(), pollableVideoCreateSpec(), emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
		OnStatus: func(status string, elapsed time.Duration) {
			statuses = append(statuses, status)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 1 || statuses[0] != "processing" {
		t.Fatalf("statuses = %v, want [processing]", statuses)
	}
}

func TestExtractJSONPath_Nested(t *testing.T) {
	value, err := extractJSONPath(json.RawMessage(`{"data":{"video_id":"abc123"}}`), "data.video_id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "abc123" {
		t.Fatalf("value = %q, want %q", value, "abc123")
	}
}

func TestExtractJSONPath_Missing(t *testing.T) {
	_, err := extractJSONPath(json.RawMessage(`{"data":{}}`), "data.video_id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExtractJSONPath_ArrayIndex(t *testing.T) {
	value, err := extractJSONPath(json.RawMessage(`{"data":{"ids":["abc","def"]}}`), "data.ids.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "abc" {
		t.Fatalf("value = %q, want %q", value, "abc")
	}
}

func TestExtractJSONPath_ArrayIndexOutOfBounds(t *testing.T) {
	_, err := extractJSONPath(json.RawMessage(`{"data":{"ids":["abc"]}}`), "data.ids.5")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExtractJSONPath_ArrayOnNonArray(t *testing.T) {
	_, err := extractJSONPath(json.RawMessage(`{"data":{"id":"abc"}}`), "data.id.0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecuteAndPoll_RejectsBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Create response returns multiple IDs (batch translation)
		_, _ = w.Write([]byte(`{"data":{"video_translation_ids":["id_1","id_2","id_3"]}}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	spec := &command.Spec{
		Endpoint:     "/v3/video-translations",
		Method:       http.MethodPost,
		BodyEncoding: "json",
		PollConfig: &command.PollConfig{
			StatusEndpoint: "/v3/video-translations/{video_translation_id}",
			StatusField:    "data.status",
			TerminalOK:     []string{"completed"},
			TerminalFail:   []string{"failed"},
			IDField:        "data.video_translation_ids.0",
		},
	}

	_, err := c.ExecuteAndPoll(context.Background(), spec, emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != "batch_not_supported" {
		t.Fatalf("code = %q, want %q", cliErr.Code, "batch_not_supported")
	}
	if !strings.Contains(cliErr.Message, "3 resources") {
		t.Fatalf("message = %q, want mention of 3 resources", cliErr.Message)
	}
}

func TestExecuteAndPoll_SingleArrayElement_OK(t *testing.T) {
	var statusCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/video-translations":
			w.WriteHeader(http.StatusOK)
			// Single-language: array with 1 element
			_, _ = w.Write([]byte(`{"data":{"video_translation_ids":["trans_123"]}}`))
		case "/v3/video-translations/trans_123":
			statusCalls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"trans_123","status":"completed"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithMaxRetries(0))

	spec := &command.Spec{
		Endpoint:     "/v3/video-translations",
		Method:       http.MethodPost,
		BodyEncoding: "json",
		PollConfig: &command.PollConfig{
			StatusEndpoint: "/v3/video-translations/{video_translation_id}",
			StatusField:    "data.status",
			TerminalOK:     []string{"completed"},
			TerminalFail:   []string{"failed"},
			IDField:        "data.video_translation_ids.0",
		},
	}

	result, err := c.ExecuteAndPoll(context.Background(), spec, emptyInvocation(), PollOptions{
		BaseDelay: time.Millisecond,
		MaxDelay:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusCalls != 1 {
		t.Fatalf("statusCalls = %d, want 1", statusCalls)
	}
	if !strings.Contains(string(result), "completed") {
		t.Fatalf("result = %s, want completed", result)
	}
}

func pollableVideoCreateSpec() *command.Spec {
	return &command.Spec{
		Endpoint:     "/v3/videos",
		Method:       http.MethodPost,
		BodyEncoding: "json",
		PollConfig: &command.PollConfig{
			StatusEndpoint: "/v3/videos/{video_id}",
			StatusField:    "data.status",
			TerminalOK:     []string{"completed"},
			TerminalFail:   []string{"failed", "error"},
			IDField:        "data.video_id",
		},
	}
}

func emptyInvocation() *command.Invocation {
	return &command.Invocation{
		PathParams:  make(map[string]string),
		QueryParams: make(url.Values),
	}
}
