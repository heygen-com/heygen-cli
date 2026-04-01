package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

func TestFileCredentialStore_Save(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	s := &FileCredentialStore{}
	if err := s.Save("test-key-123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "test-key-123\n" {
		t.Fatalf("data = %q, want %q", string(data), "test-key-123\n")
	}
}

func TestFileCredentialStore_CreatesDirectory(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", filepath.Join(t.TempDir(), "nested"))

	s := &FileCredentialStore{}
	if err := s.Save("test-key-123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(paths.ConfigDir())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected config dir to exist")
	}
}

func TestFileCredentialStore_FilePermissions(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	s := &FileCredentialStore{}
	if err := s.Save("test-key-123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Fatalf("perms = %o, want %o", perms, 0o600)
	}
}

func TestFileCredentialStore_OverwriteExisting(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	s := &FileCredentialStore{}
	if err := s.Save("first-key"); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := s.Save("second-key"); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "second-key\n" {
		t.Fatalf("data = %q, want %q", string(data), "second-key\n")
	}
}
