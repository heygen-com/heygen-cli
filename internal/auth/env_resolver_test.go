package auth

import (
	"errors"
	"testing"
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

	t.Run("returns not-configured error when unset", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "")
		_, err := r.Resolve()
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var notConfigured *ErrNotConfigured
		if !errors.As(err, &notConfigured) {
			t.Fatalf("expected *ErrNotConfigured, got %T", err)
		}
		if notConfigured.Msg == "" {
			t.Error("expected non-empty message")
		}
	})
}
