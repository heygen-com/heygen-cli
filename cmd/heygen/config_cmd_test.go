package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/config"
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

func TestConfigSet_APIBaseNotExposed(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	res := runCommand(t, "http://example.invalid", "", "config", "set", "api_base", "https://api-dev.heygen.com")
	if res.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want 2 (api_base is internal)\nstderr: %s", res.ExitCode, res.Stderr)
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

	// Use --human=false to get JSON output, since HEYGEN_OUTPUT=human
	// would otherwise switch the formatter to human mode.
	res := runCommand(t, "http://example.invalid", "", "config", "get", "output", "--human=false")
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

	// Use --human=false to get JSON output, since the config file sets
	// output=human which would otherwise switch the formatter.
	res := runCommand(t, "http://example.invalid", "", "config", "get", "output", "--human=false")
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
	if len(parsed) != len(config.ValidKeys) {
		t.Fatalf("len(parsed) = %d, want one entry per key in %v", len(parsed), config.ValidKeys)
	}
}

func TestConfigList_MixedSources(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv("HEYGEN_OUTPUT", "json")
	if err := os.WriteFile(filepath.Join(os.Getenv("HEYGEN_CONFIG_DIR"), "config.toml"), []byte("analytics = false\n"), 0o600); err != nil {
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
	if len(parsed) != len(config.ValidKeys) {
		t.Fatalf("len(parsed) = %d, want one entry per key in %v", len(parsed), config.ValidKeys)
	}
}

// update_check is stored as a TOML bool like analytics, and the env opt-out
// outranks the file.
func TestConfig_UpdateCheckKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	if res := runCommand(t, "http://example.invalid", "", "config", "set", "update_check", "false"); res.ExitCode != 0 {
		t.Fatalf("config set: exit %d, stderr %s", res.ExitCode, res.Stderr)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil || !strings.Contains(string(data), "update_check = false") {
		t.Fatalf("config.toml = %q (%v), want a TOML bool", data, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("update_check = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEYGEN_NO_UPDATE_CHECK", "1")
	res := runCommand(t, "http://example.invalid", "", "config", "get", "update_check")
	var got configResponse
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("Unmarshal: %v (%s)", err, res.Stdout)
	}
	if got.Value != "false" || got.Source != "env" {
		t.Errorf("config get update_check = %+v, want false from env", got)
	}
}
