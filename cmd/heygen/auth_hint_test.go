package main

import (
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
	if !strings.Contains(res.Stderr, "app.heygen.com/settings/api") {
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
	if !strings.Contains(res.Stderr, filepath.Join(dir, "credentials")) {
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
	if !strings.Contains(res.Stderr, "Three ways to provide your API key") {
		t.Fatalf("no-key error should preserve authGuidance hint:\n%s", res.Stderr)
	}
}
