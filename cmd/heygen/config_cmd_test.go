package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSet_Success(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	res := runCommand(t, "http://example.invalid", "", "config", "set", "output", "human")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	data, err := os.ReadFile(filepath.Join(os.Getenv("HEYGEN_CONFIG_DIR"), "config.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) == "" {
		t.Fatal("expected config file contents")
	}
}

func TestConfigSet_InvalidKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	res := runCommand(t, "http://example.invalid", "", "config", "set", "bogus", "value")
	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestConfigSet_InvalidOutputValue(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	res := runCommand(t, "http://example.invalid", "", "config", "set", "output", "xml")
	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestConfigSet_SkipsAuth(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	res := runCommand(t, "http://example.invalid", "", "config", "set", "output", "human")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestConfigGet_Default(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	res := runCommand(t, "http://example.invalid", "", "config", "get", "output")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed configResponse
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if parsed.Value != "json" || parsed.Source != "default" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestConfigGet_FromEnv(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv("HEYGEN_OUTPUT", "human")

	res := runCommand(t, "http://example.invalid", "", "config", "get", "output")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed configResponse
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if parsed.Value != "human" || parsed.Source != "env" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestConfigGet_FromFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(os.Getenv("HEYGEN_CONFIG_DIR"), "config.toml"), []byte("output = \"human\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	res := runCommand(t, "http://example.invalid", "", "config", "get", "output")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed configResponse
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if parsed.Value != "human" || parsed.Source != "file" {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestConfigGet_InvalidKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	res := runCommand(t, "http://example.invalid", "", "config", "get", "bogus")
	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2\nstderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestConfigList_AllDefaults(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	res := runCommand(t, "http://example.invalid", "", "config", "list")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed []configResponse
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(parsed) != 4 {
		t.Fatalf("len(parsed) = %d, want 4", len(parsed))
	}
}

func TestConfigList_MixedSources(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv("HEYGEN_OUTPUT", "json")
	if err := os.WriteFile(filepath.Join(os.Getenv("HEYGEN_CONFIG_DIR"), "config.toml"), []byte("api_base = \"https://api-dev.heygen.com\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	res := runCommand(t, "http://example.invalid", "", "config", "list")
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0\nstderr: %s", res.ExitCode, res.Stderr)
	}

	var parsed []configResponse
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(parsed) != 4 {
		t.Fatalf("len(parsed) = %d, want 4", len(parsed))
	}
}
