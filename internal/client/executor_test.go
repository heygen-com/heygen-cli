package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithNoRetry())

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

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithNoRetry())

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

func TestExecuteAll_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"v1"}],"next_token":null}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	spec := &command.Spec{
		Endpoint:   "/v3/videos",
		Method:     "GET",
		Paginated:  true,
		TokenField: "next_token",
		TokenParam: "token",
		DataField:  "data",
	}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	result, err := c.ExecuteAll(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON array: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("len(parsed) = %d, want 1", len(parsed))
	}
	if parsed[0]["id"] != "v1" {
		t.Fatalf("parsed[0].id = %v, want %q", parsed[0]["id"], "v1")
	}
}

func TestExecuteAll_MultiplePages(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Query().Get("token") {
		case "":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"v1"},{"id":"v2"}],"next_token":"cursor_2"}`))
		case "cursor_2":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"v3"},{"id":"v4"}],"next_token":"cursor_3"}`))
		case "cursor_3":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"v5"},{"id":"v6"}],"next_token":""}`))
		default:
			t.Fatalf("unexpected token %q", r.URL.Query().Get("token"))
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	spec := &command.Spec{
		Endpoint:   "/v3/videos",
		Method:     "GET",
		Paginated:  true,
		TokenField: "next_token",
		TokenParam: "token",
		DataField:  "data",
	}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	result, err := c.ExecuteAll(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON array: %v", err)
	}
	if len(parsed) != 6 {
		t.Fatalf("len(parsed) = %d, want 6", len(parsed))
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestExecuteAll_EmptyFirstPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"next_token":null}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	spec := &command.Spec{
		Endpoint:   "/v3/videos",
		Method:     "GET",
		Paginated:  true,
		TokenField: "next_token",
		TokenParam: "token",
		DataField:  "data",
	}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	result, err := c.ExecuteAll(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "[]" {
		t.Fatalf("result = %s, want []", result)
	}
}

func TestExecuteAll_ErrorOnSecondPage(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("token") == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"v1"}],"next_token":"cursor_2"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"server_error","message":"boom"}}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithNoRetry())
	spec := &command.Spec{
		Endpoint:   "/v3/videos",
		Method:     "GET",
		Paginated:  true,
		TokenField: "next_token",
		TokenParam: "token",
		DataField:  "data",
	}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.ExecuteAll(spec, inv)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestExecuteAll_MissingDataField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"next_token":null}`))
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	spec := &command.Spec{
		Endpoint:   "/v3/videos",
		Method:     "GET",
		Paginated:  true,
		TokenField: "next_token",
		TokenParam: "token",
		DataField:  "data",
	}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.ExecuteAll(spec, inv)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecuteAll_Truncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		switch token {
		case "":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(0, 2500, "cursor_1")))
		case "cursor_1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(2500, 2500, "cursor_2")))
		case "cursor_2":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(5000, 2500, "cursor_3")))
		case "cursor_3":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(7500, 2500, "cursor_4")))
		default:
			t.Fatalf("unexpected token %q", token)
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	spec := &command.Spec{
		Endpoint:   "/v3/videos",
		Method:     "GET",
		Paginated:  true,
		TokenField: "next_token",
		TokenParam: "token",
		DataField:  "data",
	}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	_, err := c.ExecuteAll(spec, inv)
	var truncErr *ErrPaginationTruncated
	if !errors.As(err, &truncErr) {
		t.Fatalf("err = %T, want *ErrPaginationTruncated", err)
	}
	if truncErr.Count != 10000 {
		t.Fatalf("Count = %d, want 10000", truncErr.Count)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(truncErr.Data, &parsed); err != nil {
		t.Fatalf("truncErr.Data is not valid JSON array: %v", err)
	}
	if len(parsed) != 10000 {
		t.Fatalf("len(parsed) = %d, want 10000", len(parsed))
	}
}

func TestExecuteAll_ExactlyAtLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		switch token {
		case "":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(0, 2000, "cursor_1")))
		case "cursor_1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(2000, 2000, "cursor_2")))
		case "cursor_2":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(4000, 2000, "cursor_3")))
		case "cursor_3":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(6000, 2000, "cursor_4")))
		case "cursor_4":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(paginationPageBody(8000, 2000, "")))
		default:
			t.Fatalf("unexpected token %q", token)
		}
	}))
	defer srv.Close()

	c := New("key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	spec := &command.Spec{
		Endpoint:   "/v3/videos",
		Method:     "GET",
		Paginated:  true,
		TokenField: "next_token",
		TokenParam: "token",
		DataField:  "data",
	}
	inv := &command.Invocation{PathParams: make(map[string]string), QueryParams: make(url.Values)}

	result, err := c.ExecuteAll(spec, inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("result is not valid JSON array: %v", err)
	}
	if len(parsed) != 10000 {
		t.Fatalf("len(parsed) = %d, want 10000", len(parsed))
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

func paginationPageBody(start, count int, nextToken string) string {
	items := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, map[string]any{"id": fmt.Sprintf("v%d", start+i)})
	}

	body := map[string]any{
		"data": items,
	}
	if nextToken == "" {
		body["next_token"] = nil
	} else {
		body["next_token"] = nextToken
	}
	raw, _ := json.Marshal(body)
	return string(raw)
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
