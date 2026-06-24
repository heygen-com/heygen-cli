package auth

// CredentialSource identifies where a credential was resolved from.
type CredentialSource string

const (
	SourceEnv  CredentialSource = "env"
	SourceFile CredentialSource = "file"
)

// CredentialResult pairs a resolved API key with its source. Retained
// for callers that don't yet understand OAuth.
type CredentialResult struct {
	Key    string
	Source CredentialSource
}

// CredentialResolver locates an API key from a credential source.
// Implementations include environment and file-based resolvers.
//
// Resolve returns the API-key string form. OAuth-aware resolvers also
// satisfy TypedCredentialResolver and return a richer typed credential
// — see ChainCredentialResolver.ResolveTypedCredential.
type CredentialResolver interface {
	Resolve() (string, error)
}

// TypedCredentialResolver is implemented by resolvers that can return a
// typed Credential (api-key OR oauth). The chain resolver opportunistically
// uses this when present and falls back to the string form otherwise.
type TypedCredentialResolver interface {
	ResolveCredential() (*Credential, error)
}
