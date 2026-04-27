package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

type mockResolver struct {
	key    string
	err    error
	called *int
}

func (r *mockResolver) Resolve() (string, error) {
	if r.called != nil {
		*r.called++
	}
	return r.key, r.err
}

func TestChainResolver_FirstWins(t *testing.T) {
	secondCalls := 0
	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&mockResolver{key: "env-key"},
			&mockResolver{key: "file-key", called: &secondCalls},
		},
	}

	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "env-key" {
		t.Fatalf("key = %q, want %q", key, "env-key")
	}
	if secondCalls != 0 {
		t.Fatalf("second resolver called %d times, want 0", secondCalls)
	}
}

func TestChainResolver_FallsThrough(t *testing.T) {
	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&mockResolver{err: &ErrNotConfigured{Msg: "not set"}},
			&mockResolver{key: "file-key"},
		},
	}

	key, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if key != "file-key" {
		t.Fatalf("key = %q, want %q", key, "file-key")
	}
}

func TestChainResolver_AllNotConfigured(t *testing.T) {
	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&mockResolver{err: &ErrNotConfigured{Msg: "env not set"}},
			&mockResolver{err: &ErrNotConfigured{Msg: "file missing"}},
		},
	}

	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitAuth)
	}
}

func TestChainResolver_BrokenSource(t *testing.T) {
	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&mockResolver{err: &ErrNotConfigured{Msg: "env not set"}},
			&mockResolver{err: errors.New("permission denied")},
			&mockResolver{key: "unused"},
		},
	}

	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitAuth)
	}
	if cliErr.Message != "permission denied" {
		t.Fatalf("Message = %q, want %q", cliErr.Message, "permission denied")
	}
}

func TestResolveWithSource_EnvWins(t *testing.T) {
	t.Setenv("HEYGEN_API_KEY", "env-key")
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	_ = os.WriteFile(filepath.Join(dir, "credentials"), []byte("file-key"), 0600)

	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&EnvCredentialResolver{},
			&FileCredentialResolver{},
		},
	}

	result, err := r.ResolveWithSource()
	if err != nil {
		t.Fatalf("ResolveWithSource: %v", err)
	}
	if result.Key != "env-key" {
		t.Fatalf("Key = %q, want %q", result.Key, "env-key")
	}
	if result.Source != SourceEnv {
		t.Fatalf("Source = %q, want %q", result.Source, SourceEnv)
	}
}

func TestResolveWithSource_FallsToFile(t *testing.T) {
	t.Setenv("HEYGEN_API_KEY", "")
	dir := t.TempDir()
	t.Setenv("HEYGEN_CONFIG_DIR", dir)
	_ = os.WriteFile(filepath.Join(dir, "credentials"), []byte("file-key"), 0600)

	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&EnvCredentialResolver{},
			&FileCredentialResolver{},
		},
	}

	result, err := r.ResolveWithSource()
	if err != nil {
		t.Fatalf("ResolveWithSource: %v", err)
	}
	if result.Key != "file-key" {
		t.Fatalf("Key = %q, want %q", result.Key, "file-key")
	}
	if result.Source != SourceFile {
		t.Fatalf("Source = %q, want %q", result.Source, SourceFile)
	}
}

func TestResolveWithSource_NoneConfigured(t *testing.T) {
	t.Setenv("HEYGEN_API_KEY", "")
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	r := &ChainCredentialResolver{
		Resolvers: []CredentialResolver{
			&EnvCredentialResolver{},
			&FileCredentialResolver{},
		},
	}

	_, err := r.ResolveWithSource()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.ExitCode != clierrors.ExitAuth {
		t.Fatalf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitAuth)
	}
}
