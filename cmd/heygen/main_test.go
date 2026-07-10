package main

import (
	"os"
	"path/filepath"
	"strings"
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

// U3: first analytics-enabled run with no local notice-shown flag prints
// the disclosure once and persists the flag to heygen-cli's own config dir
// (not the shared ~/.hyperframes/config.json — see internal/analytics).
func TestMaybeShowTelemetryNotice_FirstRun(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	var stderr strings.Builder
	maybeShowTelemetryNotice(true, &stderr)

	if !strings.Contains(stderr.String(), "telemetry") {
		t.Fatalf("stderr = %q, want a telemetry disclosure", stderr.String())
	}
	if _, err := os.Stat(telemetryNoticePath()); err != nil {
		t.Fatalf("notice-shown flag not persisted: %v", err)
	}
}

func TestMaybeShowTelemetryNotice_AlreadyShown(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "telemetry_notice_shown"), []byte("1"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var stderr strings.Builder
	maybeShowTelemetryNotice(true, &stderr)

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty (notice already shown)", stderr.String())
	}
}

func TestMaybeShowTelemetryNotice_AnalyticsDisabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)

	var stderr strings.Builder
	maybeShowTelemetryNotice(false, &stderr)

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty (analytics disabled)", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "telemetry_notice_shown")); err == nil {
		t.Fatal("notice-shown flag written despite analytics being disabled")
	}
}
