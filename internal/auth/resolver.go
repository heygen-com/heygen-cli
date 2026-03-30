package auth

// CredentialResolver locates an API key by searching a priority chain of
// credential sources (environment variable, OS keychain, credentials file).
// The first source that returns a key wins.
//
// Currently only EnvCredentialResolver is implemented (reads HEYGEN_API_KEY).
type CredentialResolver interface {
	Resolve() (string, error)
}
