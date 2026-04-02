package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthLogin_Success(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("test-key-123\n"), "auth", "login")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, res.Stdout)
	}
	if parsed["message"] == "" {
		t.Fatalf("expected success message, got %v", parsed)
	}

	data, err := os.ReadFile(filepath.Join(os.Getenv("HEYGEN_CONFIG_DIR"), "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "test-key-123\n" {
		t.Fatalf("credentials = %q, want %q", string(data), "test-key-123\n")
	}
}

func TestAuthLogin_EmptyInput(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("\n"), "auth", "login")

	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestAuthLogin_OverwriteExisting(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	first := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("first-key\n"), "auth", "login")
	if first.ExitCode != 0 {
		t.Fatalf("first login failed: %#v", first)
	}

	second := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("second-key\n"), "auth", "login")
	if second.ExitCode != 0 {
		t.Fatalf("second login failed: %#v", second)
	}

	data, err := os.ReadFile(filepath.Join(os.Getenv("HEYGEN_CONFIG_DIR"), "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "second-key\n" {
		t.Fatalf("credentials = %q, want %q", string(data), "second-key\n")
	}
}

func TestAuthLogin_SkipsAuth(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("test-key-123\n"), "auth", "login")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if res.Stderr != "" {
		t.Fatalf("stderr = %q, want empty", res.Stderr)
	}
}
