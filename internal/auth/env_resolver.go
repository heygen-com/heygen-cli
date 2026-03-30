package auth

import (
	"os"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

const EnvAPIKey = "HEYGEN_API_KEY"

// EnvCredentialResolver implements CredentialResolver by reading the
// HEYGEN_API_KEY environment variable. Returns an auth error (exit 3)
// with an actionable hint if the variable is not set.
type EnvCredentialResolver struct{}

// Resolve returns the API key from the HEYGEN_API_KEY environment variable.
func (r *EnvCredentialResolver) Resolve() (string, error) {
	key := os.Getenv(EnvAPIKey)
	if key == "" {
		return "", clierrors.NewAuth(
			"no API key found",
			"Set the HEYGEN_API_KEY environment variable: export HEYGEN_API_KEY=<your-api-key>",
		)
	}
	return key, nil
}
