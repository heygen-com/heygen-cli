package config

import "os"

const (
	envBaseURL = "HEYGEN_API_BASE"

	DefaultBaseURL = "https://api.heygen.com"
)

// EnvProvider implements Provider by reading from environment variables.
// Falls back to built-in defaults when a variable is not set.
type EnvProvider struct{}

// BaseURL returns HEYGEN_API_BASE if set, otherwise https://api.heygen.com.
func (p *EnvProvider) BaseURL() string {
	if v := os.Getenv(envBaseURL); v != "" {
		return v
	}
	return DefaultBaseURL
}
