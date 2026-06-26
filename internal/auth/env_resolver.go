package auth

import (
	"os"
)

const EnvAPIKey = "HEYGEN_API_KEY"

// EnvCredentialResolver implements CredentialResolver by reading the
// HEYGEN_API_KEY environment variable.
type EnvCredentialResolver struct{}

// Resolve returns the API key from the HEYGEN_API_KEY environment
// variable. Retained for backwards-compat with the chain's string path.
func (r *EnvCredentialResolver) Resolve() (string, error) {
	cred, err := r.ResolveCredential()
	if err != nil {
		return "", err
	}
	return cred.APIKey, nil
}

// ResolveCredential returns the API key from HEYGEN_API_KEY as a typed
// Credential. The env path only carries an API key — OAuth flows always
// go through the credentials file.
func (r *EnvCredentialResolver) ResolveCredential() (*Credential, error) {
	key := os.Getenv(EnvAPIKey)
	if key == "" {
		return nil, &ErrNotConfigured{Msg: "HEYGEN_API_KEY not set"}
	}
	return &Credential{
		Type:   CredentialTypeAPIKey,
		APIKey: key,
		Source: SourceEnv,
	}, nil
}
