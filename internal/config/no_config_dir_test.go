package config

import (
	"os"
	"testing"
)

func TestFileProviderWithNoConfigDirLeavesTheWorkingDirectoryAlone(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HEYGEN_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	p := &FileProvider{}
	if _, found, err := p.Get("output"); found || err != nil {
		t.Fatalf("Get: found=%v err=%v, want not configured", found, err)
	}
	if err := p.Set("output", "human"); err == nil {
		t.Fatal("Set with no config directory succeeded")
	}
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Fatalf("the working directory gained %d entries", len(entries))
	}
}
