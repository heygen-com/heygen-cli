package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

func newLayeredProvider() *LayeredProvider {
	return &LayeredProvider{
		Env:  &EnvProvider{},
		File: &FileProvider{},
	}
}

func writeConfigFile(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLayeredProvider_DefaultValues(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	p := newLayeredProvider()

	cases := map[string]string{
		KeyOutput:     DefaultOutput,
		KeyAutoUpdate: "true",
		KeyAPIBase:    DefaultBaseURL,
		KeyAnalytics:  "unset",
	}
	for key, want := range cases {
		got, err := p.Resolve(key)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", key, err)
		}
		if got.Value != want || got.Origin != "default" {
			t.Fatalf("Resolve(%s) = %#v, want value=%q origin=default", key, got, want)
		}
	}
}

func TestLayeredProvider_EnvOverridesDefault(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv(envOutput, "human")
	p := newLayeredProvider()

	got, err := p.Resolve(KeyOutput)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Value != "human" || got.Origin != "env" {
		t.Fatalf("Resolve = %#v", got)
	}
}

func TestLayeredProvider_FileOverridesDefault(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeConfigFile(t, "output = \"human\"\n")
	p := newLayeredProvider()

	got, err := p.Resolve(KeyOutput)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Value != "human" || got.Origin != "file" {
		t.Fatalf("Resolve = %#v", got)
	}
}

func TestLayeredProvider_EnvOverridesFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv(envOutput, "json")
	writeConfigFile(t, "output = \"human\"\n")
	p := newLayeredProvider()

	got, err := p.Resolve(KeyOutput)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Value != "json" || got.Origin != "env" {
		t.Fatalf("Resolve = %#v", got)
	}
}

func TestLayeredProvider_ResolveAllKeys(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeConfigFile(t, "output = \"human\"\nauto_update = false\napi_base = \"https://api-dev.heygen.com\"\n")
	p := newLayeredProvider()

	cases := map[string]string{
		KeyOutput:     "human",
		KeyAutoUpdate: "false",
		KeyAPIBase:    "https://api-dev.heygen.com",
	}
	for key, want := range cases {
		got, err := p.Resolve(key)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", key, err)
		}
		if got.Value != want {
			t.Fatalf("Resolve(%s).Value = %q, want %q", key, got.Value, want)
		}
	}
}

func TestLayeredProvider_AnalyticsUnset(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	p := newLayeredProvider()

	if got := p.Analytics(); got != nil {
		t.Fatalf("Analytics = %v, want nil", *got)
	}
}

func TestLayeredProvider_AnalyticsFromFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	writeConfigFile(t, "analytics = true\n")
	p := newLayeredProvider()

	got := p.Analytics()
	if got == nil || !*got {
		t.Fatalf("Analytics = %v, want true", got)
	}
}

func TestLayeredProvider_AnalyticsEnvDisable(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	t.Setenv(envNoAnalytics, "1")
	writeConfigFile(t, "analytics = true\n")
	p := newLayeredProvider()

	got := p.Analytics()
	if got == nil || *got {
		t.Fatalf("Analytics = %v, want false", got)
	}
}
