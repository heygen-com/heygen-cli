package auth

// CredentialStore persists an API key for future CLI invocations.
type CredentialStore interface {
	Save(apiKey string) error
}
