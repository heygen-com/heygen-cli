package auth

// CredentialResolver locates an API key from a credential source.
// Currently only EnvCredentialResolver is implemented (reads HEYGEN_API_KEY).
// Future: file-based credential storage (~/.heygen/credentials).
type CredentialResolver interface {
	Resolve() (string, error)
}
