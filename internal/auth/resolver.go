package auth

// CredentialResolver locates an API key from a credential source.
// Implementations include environment and file-based resolvers.
type CredentialResolver interface {
	Resolve() (string, error)
}
