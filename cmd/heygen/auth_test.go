package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAuthLogin_EmptyStdin_NoEnvVar_NonTTY verifies that a non-TTY empty stdin
// with no HEYGEN_API_KEY set returns exit 2 with the pipe-hint message.
func TestAuthLogin_EmptyStdin_NoEnvVar_NonTTY(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv("HEYGEN_API_KEY", "") // ensure env var is not set

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader(""), "auth", "login")

	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Pipe your key") {
		t.Fatalf("stderr = %q, want pipe hint", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "HEYGEN_API_KEY") {
		t.Fatalf("stderr = %q, want env var hint", res.Stderr)
	}
}

// TestAuthLogin_EmptyStdin_WithEnvVar_NonTTY verifies that when HEYGEN_API_KEY
// is set but stdin is empty+non-TTY, the command still returns exit 2 with a
// helpful error. We don't silently exit 0 because Go can't distinguish "no
// stdin" from "piped empty" (cat /dev/null), which would mask broken automation.
func TestAuthLogin_EmptyStdin_WithEnvVar_NonTTY(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv("HEYGEN_API_KEY", "env-test-key")

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader(""), "auth", "login")

	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "Pipe your key") {
		t.Fatalf("stderr = %q, want pipe hint", res.Stderr)
	}
}

// TestAuthLogin_PipedKey_StillWorks is a regression test ensuring neither
// change 1 nor change 2 broke the happy path.
func TestAuthLogin_PipedKey_StillWorks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	res := runCommandWithInput(t, "http://example.invalid", "", strings.NewReader("real-key\n"), "auth", "login")

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "real-key\n" {
		t.Fatalf("credentials = %q, want %q", string(data), "real-key\n")
	}
}

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
