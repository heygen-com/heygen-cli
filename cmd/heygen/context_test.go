package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestEnrichAuthHint_EnvSource_401(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 401,
			Body:       `{"error":{"code":"unauthorized","message":"invalid API key"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "bad-key", "video", "list")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "HEYGEN_API_KEY environment variable") {
		t.Fatalf("stderr should mention env var source:\n%s", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "app.heygen.com/settings?nav=API") {
		t.Fatalf("stderr should contain key generation URL:\n%s", res.Stderr)
	}
}

func TestEnrichAuthHint_FileSource_401(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 401,
			Body:       `{"error":{"code":"unauthorized","message":"invalid API key"}}`,
		},
	})
	defer srv.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "credentials"), []byte("bad-key"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_API_KEY", "")

	res := runCommand(t, srv.URL, "", "video", "list")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "heygen auth login") {
		t.Fatalf("stderr should mention auth login for file source:\n%s", res.Stderr)
	}
	pathJSON, _ := json.Marshal(filepath.Join(dir, "credentials"))
	escapedPath := string(pathJSON[1 : len(pathJSON)-1])
	if !strings.Contains(res.Stderr, escapedPath) {
		t.Fatalf("stderr should mention the resolved credentials path:\n%s", res.Stderr)
	}
}

func TestEnrichAuthHint_NonAuthError_NoMutation(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 500,
			Body:       `{"error":{"code":"internal_error","message":"server error"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "video", "list")

	if res.ExitCode != clierrors.ExitGeneral {
		t.Fatalf("ExitCode = %d, want %d", res.ExitCode, clierrors.ExitGeneral)
	}
	if strings.Contains(res.Stderr, "HEYGEN_API_KEY environment variable") {
		t.Fatalf("non-auth error should not get auth hint:\n%s", res.Stderr)
	}
}

func TestEnrichAuthHint_ExistingHint_Preserved(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	res := runCommand(t, "http://example.invalid", "", "video", "list")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Two ways to authenticate") {
		t.Fatalf("no-key error should preserve authGuidance hint:\n%s", res.Stderr)
	}
}

// A 403 (forbidden) exits 3 but must NOT be given a login / invalid-key hint by
// enrichAuthHint — it keeps its permission-oriented hint.
func TestEnrichAuthHint_Forbidden_NotOverwritten(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 403,
			Body:       `{"error":{"code":"forbidden","message":"not allowed"}}`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "some-key", "video", "list")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"code":"forbidden"`) {
		t.Fatalf("stderr should carry the forbidden code:\n%s", res.Stderr)
	}
	if strings.Contains(res.Stderr, "HEYGEN_API_KEY environment variable") || strings.Contains(res.Stderr, "auth login") {
		t.Fatalf("a 403 must not get a login/invalid-key hint:\n%s", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "not permitted") {
		t.Fatalf("a 403 should carry a permission hint:\n%s", res.Stderr)
	}
}

// A non-envelope (HTML) 403 — status-derived forbidden — must also avoid the
// login hint end-to-end, exercising the derived-code path through the formatter.
func TestForbidden_NonEnvelope_NoLoginHint(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/videos": {
			StatusCode: 403,
			Body:       `<html>403 Forbidden</html>`,
		},
	})
	defer srv.Close()

	res := runCommand(t, srv.URL, "some-key", "video", "list")

	if res.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d\nstderr: %s", res.ExitCode, clierrors.ExitAuth, res.Stderr)
	}
	if !strings.Contains(res.Stderr, `"code":"forbidden"`) {
		t.Fatalf("stderr should carry the derived forbidden code:\n%s", res.Stderr)
	}
	if strings.Contains(res.Stderr, "HEYGEN_API_KEY environment variable") || strings.Contains(res.Stderr, "auth login") {
		t.Fatalf("a non-envelope 403 must not get a login hint:\n%s", res.Stderr)
	}
}
