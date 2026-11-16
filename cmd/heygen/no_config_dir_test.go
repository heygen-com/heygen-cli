package main

import (
	"os"
	"testing"
)

func TestUpdateCacheWithNoConfigDirLeavesTheWorkingDirectoryAlone(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("HEYGEN_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if _, err := lockUpdateCache(); err == nil {
		t.Fatal("lockUpdateCache with no config directory succeeded")
	}
	if err := writeUpdateCache(updateCheckCache{}); err == nil {
		t.Fatal("writeUpdateCache with no config directory succeeded")
	}
	if got := readUpdateCache(); got != (updateCheckCache{}) {
		t.Fatalf("readUpdateCache: got %+v, want empty", got)
	}
	if entries, _ := os.ReadDir(cwd); len(entries) != 0 {
		t.Fatalf("the working directory gained %d entries", len(entries))
	}
}
