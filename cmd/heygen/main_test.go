package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyticsEnabled_Default(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	if !analyticsEnabled() {
		t.Fatal("analyticsEnabled = false, want true")
	}
}

func TestAnalyticsEnabled_ConfigFalse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("analytics = false\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if analyticsEnabled() {
		t.Fatal("analyticsEnabled = true, want false")
	}
}

func TestAnalyticsEnabled_EnvOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	t.Setenv("HEYGEN_NO_ANALYTICS", "1")

	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("analytics = true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if analyticsEnabled() {
		t.Fatal("analyticsEnabled = true, want false")
	}
}
