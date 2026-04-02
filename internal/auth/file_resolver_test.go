package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

func TestFileCredentialResolver_ReadsKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := filepath.Join(paths.ConfigDir(), "credentials")
	if err := os.WriteFile(path, []byte("test-key-123\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "test-key-123" {
		t.Fatalf("key = %q, want %q", key, "test-key-123")
	}
}

func TestFileCredentialResolver_MissingFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var notConfigured *ErrNotConfigured
	if !errors.As(err, &notConfigured) {
		t.Fatalf("expected *ErrNotConfigured, got %T", err)
	}
}

func TestFileCredentialResolver_EmptyFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := filepath.Join(paths.ConfigDir(), "credentials")
	if err := os.WriteFile(path, []byte(" \n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := &FileCredentialResolver{}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFileCredentialResolver_WhitespaceHandling(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())
	path := filepath.Join(paths.ConfigDir(), "credentials")
	if err := os.WriteFile(path, []byte("test-key-123  \n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := &FileCredentialResolver{}
	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "test-key-123" {
		t.Fatalf("key = %q, want %q", key, "test-key-123")
	}
}
