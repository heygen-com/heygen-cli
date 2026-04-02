package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/heygen-com/heygen-cli/internal/paths"
)

func TestFileProvider_GetExists(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	if err := os.WriteFile(path, []byte("output = \"human\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := &FileProvider{}
	val, ok, err := p.Get(KeyOutput)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || val != "human" {
		t.Fatalf("Get = (%q, %v), want (%q, true)", val, ok, "human")
	}
}

func TestFileProvider_GetMissing(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	if err := os.WriteFile(path, []byte("output = \"human\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := &FileProvider{}
	_, ok, err := p.Get(KeyAutoUpdate)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected missing key")
	}
}

func TestFileProvider_GetNoFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	p := &FileProvider{}

	_, ok, err := p.Get(KeyOutput)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected no value")
	}
}

func TestFileProvider_GetCorruptFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	if err := os.WriteFile(path, []byte("not = [valid\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := &FileProvider{}
	if _, _, err := p.Get(KeyOutput); err == nil {
		t.Fatal("expected corrupt file error")
	}
}

func TestFileProvider_SetNewFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	p := &FileProvider{}

	if err := p.Set(KeyOutput, "human"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, err := os.Stat(filepath.Join(paths.ConfigDir(), "config.toml")); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

func TestFileProvider_SetUpdateExisting(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	if err := os.WriteFile(path, []byte("output = \"json\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := &FileProvider{}
	if err := p.Set(KeyAnalytics, "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	all, err := p.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if all[KeyOutput] != "json" {
		t.Fatalf("output = %q, want %q", all[KeyOutput], "json")
	}
	if all[KeyAnalytics] != "false" {
		t.Fatalf("analytics = %q, want %q", all[KeyAnalytics], "false")
	}
}

func TestFileProvider_SetOverwriteKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	p := &FileProvider{}

	if err := p.Set(KeyOutput, "json"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := p.Set(KeyOutput, "human"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, ok, err := p.Get(KeyOutput)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || val != "human" {
		t.Fatalf("Get = (%q, %v), want (%q, true)", val, ok, "human")
	}
}

func TestFileProvider_SetBooleanType(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	p := &FileProvider{}

	if err := p.Set(KeyAnalytics, "false"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var data map[string]any
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	if _, err := toml.DecodeFile(path, &data); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if _, ok := data[KeyAnalytics].(bool); !ok {
		t.Fatalf("analytics type = %T, want bool", data[KeyAnalytics])
	}
}

func TestFileProvider_SetStringType(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	p := &FileProvider{}

	if err := p.Set(KeyOutput, "human"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var data map[string]any
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	if _, err := toml.DecodeFile(path, &data); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if _, ok := data[KeyOutput].(string); !ok {
		t.Fatalf("output type = %T, want string", data[KeyOutput])
	}
}

func TestFileProvider_GetAll(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	p := &FileProvider{}
	if err := p.Set(KeyOutput, "human"); err != nil {
		t.Fatalf("Set output: %v", err)
	}
	if err := p.Set(KeyAutoUpdate, "false"); err != nil {
		t.Fatalf("Set auto_update: %v", err)
	}

	all, err := p.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if all[KeyOutput] != "human" || all[KeyAutoUpdate] != "false" {
		t.Fatalf("GetAll = %#v", all)
	}
}

func TestFileProvider_GetAllCorruptFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	if err := os.WriteFile(path, []byte("bad = [toml\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p := &FileProvider{}
	if _, err := p.GetAll(); err == nil {
		t.Fatal("expected corrupt file error")
	}
}
