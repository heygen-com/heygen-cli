package main

import (
	"encoding/json"
	"net/http"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestVideoList_Success(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":[{"id":"v1","title":"Demo","status":"completed"}],"has_more":false,"next_token":null}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "list")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	// Verify stdout is valid JSON with data array
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	data, ok := parsed["data"].([]any)
	if !ok {
		t.Fatalf("data field missing or not array: %v", parsed)
	}
	if len(data) != 1 {
		t.Errorf("data length = %d, want 1", len(data))
	}
}

func TestVideoList_AuthMissing(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runCommand(t, srv.URL, "", "video", "list")

	if res.ExitCode != clierrors.ExitAuth {
		t.Errorf("ExitCode = %d, want %d", res.ExitCode, clierrors.ExitAuth)
	}

	// Verify stderr has JSON error envelope
	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(res.Stderr), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, res.Stderr)
	}
	inner := envelope["error"]
	if inner["code"] != "auth_error" {
		t.Errorf("error.code = %v, want %q", inner["code"], "auth_error")
	}
	if inner["hint"] == nil || inner["hint"] == "" {
		t.Error("expected non-empty hint in error envelope")
	}
}

func TestVideoList_APIError401(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 401,
			Body:       `{"error":{"code":"unauthorized","message":"invalid API key"}}`,
			Headers:    map[string]string{"X-Request-Id": "req_auth_fail"},
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "bad-key", "video", "list")

	if res.ExitCode != clierrors.ExitAuth {
		t.Errorf("ExitCode = %d, want %d", res.ExitCode, clierrors.ExitAuth)
	}

	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(res.Stderr), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, res.Stderr)
	}
	if envelope["error"]["request_id"] != "req_auth_fail" {
		t.Errorf("request_id = %v, want %q", envelope["error"]["request_id"], "req_auth_fail")
	}
}

func TestVideoList_APIError400(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 400,
			Body:       `{"error":{"code":"invalid_parameter","message":"limit must be between 1 and 100"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "list")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Errorf("ExitCode = %d, want %d", res.ExitCode, clierrors.ExitGeneral)
	}

	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(res.Stderr), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, res.Stderr)
	}
	if envelope["error"]["code"] != "invalid_parameter" {
		t.Errorf("error.code = %v, want %q", envelope["error"]["code"], "invalid_parameter")
	}
}

func TestVideoList_Flags(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":[],"has_more":false}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				q := r.URL.Query()
				if got := q.Get("limit"); got != "25" {
					t.Errorf("limit = %q, want %q", got, "25")
				}
				if got := q.Get("folder_id"); got != "folder_abc" {
					t.Errorf("folder_id = %q, want %q", got, "folder_abc")
				}
				if got := q.Get("token"); got != "cursor_xyz" {
					t.Errorf("token = %q, want %q", got, "cursor_xyz")
				}
			},
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"video", "list", "--limit", "25", "--folder-id", "folder_abc", "--token", "cursor_xyz")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestVideoList_ServerError(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 500,
			Body:       `{"error":{"code":"internal_error","message":"something went wrong"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "list")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Errorf("ExitCode = %d, want %d", res.ExitCode, clierrors.ExitGeneral)
	}

	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(res.Stderr), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, res.Stderr)
	}
}
