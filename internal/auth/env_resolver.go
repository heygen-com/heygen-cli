package auth

import (
	"os"
)

const EnvAPIKey = "HEYGEN_API_KEY"

// EnvCredentialResolver implements CredentialResolver by reading the
// HEYGEN_API_KEY environment variable.
type EnvCredentialResolver struct{}

// Resolve returns the API key from the HEYGEN_API_KEY environment variable.
func (r *EnvCredentialResolver) Resolve() (string, error) {
	key := os.Getenv(EnvAPIKey)
	if key == "" {
		return "", &ErrNotConfigured{Msg: "HEYGEN_API_KEY not set"}
	}
	return key, nil
}
