package auth

import (
	"errors"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestEnvCredentialResolver_Resolve(t *testing.T) {
	r := &EnvCredentialResolver{}

	t.Run("returns key when set", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "test-key-123")
		key, err := r.Resolve()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "test-key-123" {
			t.Errorf("key = %q, want %q", key, "test-key-123")
		}
	})

	t.Run("returns auth error when unset", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "")
		_, err := r.Resolve()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var cliErr *clierrors.CLIError
		if !errors.As(err, &cliErr) {
			t.Fatalf("expected *CLIError, got %T", err)
		}
		if cliErr.ExitCode != clierrors.ExitAuth {
			t.Errorf("ExitCode = %d, want %d", cliErr.ExitCode, clierrors.ExitAuth)
		}
		if cliErr.Hint == "" {
			t.Error("expected non-empty hint")
		}
	})
}
