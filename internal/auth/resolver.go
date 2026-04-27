package auth

// CredentialSource identifies where a credential was resolved from.
type CredentialSource string

const (
	SourceEnv  CredentialSource = "env"
	SourceFile CredentialSource = "file"
)

// CredentialResult pairs a resolved API key with its source.
type CredentialResult struct {
	Key    string
	Source CredentialSource
}

// CredentialResolver locates an API key from a credential source.
// Implementations include environment and file-based resolvers.
type CredentialResolver interface {
	Resolve() (string, error)
}
