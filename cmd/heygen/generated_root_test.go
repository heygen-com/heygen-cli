package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

var generatedRootANSIPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripGeneratedRootANSI(s string) string {
	return generatedRootANSIPattern.ReplaceAllString(s, "")
}

func TestGeneratedRoot_VoiceSpeechCreate(t *testing.T) {
	var gotBody map[string]any

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/voices/speech": {
			StatusCode: 200,
			Body:       `{"data":{"id":"speech_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &gotBody)
			},
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"voice", "speech", "create",
		"--text", "Hello world",
		"--voice-id", "en_male")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if gotBody["text"] != "Hello world" {
		t.Errorf("body.text = %v, want %q", gotBody["text"], "Hello world")
	}
	if gotBody["voice_id"] != "en_male" {
		t.Errorf("body.voice_id = %v, want %q", gotBody["voice_id"], "en_male")
	}
}

func TestGeneratedRoot_UserMeGet(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/user/me": {
			StatusCode: 200,
			Body:       `{"data":{"user_id":"user_123","email":"test@example.com"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "user", "me", "get")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	data, ok := parsed["data"].(map[string]any)
	if !ok {
		t.Fatalf("data field missing or not object: %v", parsed)
	}
	if data["user_id"] != "user_123" {
		t.Errorf("data.user_id = %v, want %q", data["user_id"], "user_123")
	}
}

func TestGeneratedRoot_AssetCreate(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "asset-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer tmpFile.Close()
	if _, err := tmpFile.WriteString("asset payload"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	srv := setupTestServer(t, map[string]testHandler{
		"POST /v3/assets": {
			StatusCode: 200,
			Body:       `{"data":{"id":"asset_123"}}`,
			ValidateRequest: func(t *testing.T, r *http.Request) {
				t.Helper()
				if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data;") {
					t.Errorf("Content-Type = %q, want multipart/form-data", got)
				}
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "asset payload") {
					t.Errorf("multipart body missing file content: %q", string(body))
				}
			},
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "asset", "create", "--file", tmpFile.Name())

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestGeneratedRoot_IntermediateHelp(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "voice", "speech", "--help")

	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Usage:\n  heygen voice speech [command]") {
		t.Errorf("help missing nested usage\nstdout: %s", res.Stdout)
	}
	if !strings.Contains(res.Stdout, "create") {
		t.Errorf("help missing nested child command\nstdout: %s", res.Stdout)
	}
}

func TestGeneratedRoot_UnknownFlagStillUsageError(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "voice", "speech", "create", "--bogus")

	if res.ExitCode != clierrors.ExitUsage {
		t.Errorf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitUsage, res.Stderr)
	}
}

func TestGeneratedRoot_HumanListOutput(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 200,
			Body:       `{"data":[{"id":"vid_123","title":"Demo","status":"completed","created_at":1710000000,"thumbnail_url":"https://cdn.example/thumb.jpg"}]}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "list", "--human")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	got := stripGeneratedRootANSI(res.Stdout)
	if strings.Contains(strings.TrimSpace(got), "{") {
		t.Fatalf("expected human table output, got JSON-like output:\n%s", got)
	}
	if !strings.Contains(got, "ID") || !strings.Contains(got, "Title") || !strings.Contains(got, "Demo") {
		t.Fatalf("missing curated human table content:\n%s", got)
	}
	if !strings.Contains(got, "Showing 4 of 4 fields") && !strings.Contains(got, "Showing 4 of") {
		t.Fatalf("expected curated column truncation/footer behavior:\n%s", got)
	}
}

func TestGeneratedRoot_HumanGetOutput(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos/abc123": {
			StatusCode: 200,
			Body:       `{"data":{"id":"abc123","status":"completed","title":"Demo"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "get", "abc123", "--human")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	got := stripGeneratedRootANSI(res.Stdout)
	if !strings.Contains(got, "Id:") || !strings.Contains(got, "abc123") || !strings.Contains(got, "Status:") {
		t.Fatalf("expected human key-value output:\n%s", got)
	}
}

func TestGeneratedRoot_HumanAPIError(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos/abc123": {
			StatusCode: 404,
			Body:       `{"error":{"code":"not_found","message":"Video abc123 not found"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "get", "abc123", "--human")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitGeneral, res.Stderr)
	}

	got := stripGeneratedRootANSI(res.Stderr)
	if strings.Contains(got, `"error"`) {
		t.Fatalf("expected human error output, got JSON:\n%s", got)
	}
	if !strings.Contains(got, "Error: Video abc123 not found") {
		t.Fatalf("missing human error line:\n%s", got)
	}
}

func TestGeneratedRoot_HumanCuratedNestedListOutput(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/avatars/looks": {
			StatusCode: 200,
			Body:       `{"data":[{"id":"look_001","name":"Default","avatar_type":"studio_avatar","preview_image_url":"https://cdn.example/look.png","gender":"female"}]}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "avatar", "looks", "list", "--human")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	got := stripGeneratedRootANSI(res.Stdout)
	if !strings.Contains(got, "Preview") || !strings.Contains(got, "Type") || !strings.Contains(got, "Default") {
		t.Fatalf("missing curated nested columns:\n%s", got)
	}
	if strings.Contains(got, "Gender") {
		t.Fatalf("expected curated nested columns to override auto-generated headers:\n%s", got)
	}
}
