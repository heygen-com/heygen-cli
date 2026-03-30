package config

// Provider supplies non-secret configuration values using a precedence chain:
// CLI flag > environment variable > config file > built-in default.
//
// Credentials are not part of config — see auth.CredentialResolver.
// Currently only EnvProvider is implemented (reads HEYGEN_API_BASE env var).
type Provider interface {
	BaseURL() string
}
