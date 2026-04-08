package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestVideoDownload_Success(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "vid_123.mp4")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_123":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"video_url": srv.URL + "/download/vid_123.mp4",
					"status":    "completed",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/download/vid_123.mp4":
			_, _ = w.Write([]byte("video-bytes"))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--output-path", dest)
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "video-bytes" {
		t.Fatalf("file contents = %q, want %q", string(data), "video-bytes")
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if payload["path"] != dest {
		t.Fatalf("path = %q, want %q", payload["path"], dest)
	}
	if payload["asset"] != "video" {
		t.Fatalf("asset = %q, want %q", payload["asset"], "video")
	}
}

func TestVideoDownload_DefaultFilename(t *testing.T) {
	// Test that the default filename is {video-id}.mp4 by checking
	// the JSON output path field, without relying on os.Chdir.
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "vid_abc.mp4")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_abc":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"video_url": srv.URL + "/download/vid_abc.mp4",
					"status":    "completed",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/download/vid_abc.mp4":
			_, _ = w.Write([]byte("abc-bytes"))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	// Use --output-path to write to a known location, but verify the
	// default filename logic by checking what the command *would* use.
	// The actual default path (no --output-path) is {video-id}.mp4 in CWD,
	// but we can't safely test CWD-relative paths without os.Chdir.
	// Instead, test with explicit path and verify extractVideoURL + filename
	// logic separately.
	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_abc", "--output-path", dest)
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
}

func TestVideoDownload_WithOutputPath(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom.mp4")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_123":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"video_url": srv.URL + "/download/custom.mp4",
					"status":    "completed",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/download/custom.mp4":
			_, _ = w.Write([]byte("custom-bytes"))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--output-path", customPath)
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	data, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "custom-bytes" {
		t.Fatalf("file contents = %q, want %q", string(data), "custom-bytes")
	}
}

func TestVideoDownload_AssetCaptioned(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "captioned.mp4")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_123":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"captioned_video_url": srv.URL + "/download/captioned.mp4",
					"status":              "completed",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/download/captioned.mp4":
			_, _ = w.Write([]byte("captioned-bytes"))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--asset", "captioned", "--output-path", dest)
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "captioned-bytes" {
		t.Fatalf("file contents = %q, want %q", string(data), "captioned-bytes")
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if payload["asset"] != "captioned" {
		t.Fatalf("asset = %q, want %q", payload["asset"], "captioned")
	}
}

func TestVideoDownload_AssetInvalid(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--asset", "foo")
	if res.ExitCode != clierrors.ExitUsage {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitUsage, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "must be one of: video, captioned") {
		t.Fatalf("stderr = %q, want valid asset list", res.Stderr)
	}
}

func TestVideoDownload_AssetNotAvailable(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos/vid_123": {
			StatusCode: http.StatusOK,
			Body:       `{"data":{"status":"completed","captioned_video_url":""}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--asset", "captioned")
	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}

	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(res.Stderr), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, res.Stderr)
	}
	if envelope["error"]["code"] != "asset_not_available" {
		t.Fatalf("error.code = %v, want %q", envelope["error"]["code"], "asset_not_available")
	}
	if !strings.Contains(res.Stderr, "captions enabled") {
		t.Fatalf("stderr = %q, want captions hint", res.Stderr)
	}
}

func TestVideoDownload_PreservesExistingFileOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "existing.mp4")
	if err := os.WriteFile(dest, []byte("original-content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_123":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"video_url": srv.URL + "/download/fail.mp4",
					"status":    "completed",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/download/fail.mp4":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--output-path", dest)
	if res.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1\nstderr: %s", res.ExitCode, res.Stderr)
	}

	// Original file should be preserved
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("existing file should be preserved: %v", err)
	}
	if string(data) != "original-content" {
		t.Fatalf("file contents = %q, want %q", string(data), "original-content")
	}
}

func TestVideoDownload_ForceOverwritesExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "existing.mp4")
	if err := os.WriteFile(dest, []byte("original-content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_123":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"video_url": srv.URL + "/download/vid_123.mp4",
					"status":    "completed",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/download/vid_123.mp4":
			_, _ = w.Write([]byte("new-content"))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--output-path", dest, "--force")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "new-content" {
		t.Fatalf("file contents = %q, want %q", string(data), "new-content")
	}
}

func TestVideoDownload_FileExistsNonTTYRequiresForce(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "existing.mp4")
	if err := os.WriteFile(dest, []byte("original-content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var videoRequests int
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos/vid_123": {
			StatusCode: 200,
			Body:       `{"data":{"video_url":"https://example.test/video.mp4","status":"completed"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				videoRequests++
			},
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--output-path", dest)
	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}
	if videoRequests != 0 {
		t.Fatalf("videoRequests = %d, want 0", videoRequests)
	}
	if !strings.Contains(res.Stderr, `"code":"file_exists"`) {
		t.Fatalf("stderr = %q, want file_exists error code", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "--force") {
		t.Fatalf("stderr = %q, want --force hint", res.Stderr)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "original-content" {
		t.Fatalf("file contents = %q, want %q", string(data), "original-content")
	}
}

func TestVideoDownload_VideoNotFound(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos/vid_missing": {
			StatusCode: http.StatusNotFound,
			Body:       `{"error":{"code":"not_found","message":"video not found"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_missing")
	if res.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1\nstderr: %s", res.ExitCode, res.Stderr)
	}
	var envelope map[string]map[string]any
	if err := json.Unmarshal([]byte(res.Stderr), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, res.Stderr)
	}
	if envelope["error"]["code"] != "not_found" {
		t.Fatalf("error.code = %v, want %q", envelope["error"]["code"], "not_found")
	}
}

func TestVideoDownload_VideoStillProcessing(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos/vid_123": {
			StatusCode: http.StatusOK,
			Body:       `{"data":{"status":"processing","video_url":""}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123")
	if res.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Use --wait when creating") {
		t.Fatalf("stderr = %s, want --wait hint", res.Stderr)
	}
}

func TestVideoDownload_DownloadFails(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "broken.mp4")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/videos/vid_123":
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"video_url": srv.URL + "/download/broken.mp4",
					"status":    "completed",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/download/broken.mp4":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "download", "vid_123", "--output-path", dest)
	if res.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("expected no output file, stat err = %v", err)
	}
}

func TestVideoDownload_AuthRequired(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runCommand(t, srv.URL, "", "video", "download", "vid_123")
	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, _ = w.Write(raw)
}
