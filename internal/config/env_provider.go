package config

import "os"

const (
	envBaseURL      = "HEYGEN_API_BASE"
	envOutput       = "HEYGEN_OUTPUT"
	envNoAnalytics  = "HEYGEN_NO_ANALYTICS"
	envNoAutoUpdate = "HEYGEN_NO_UPDATE_CHECK"

	DefaultBaseURL = "https://api.heygen.com"
	DefaultOutput  = "json"
)

var envVarByKey = map[string]string{
	KeyAPIBase:    envBaseURL,
	KeyOutput:     envOutput,
	KeyAnalytics:  envNoAnalytics,
	KeyAutoUpdate: envNoAutoUpdate,
}

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

// Output returns HEYGEN_OUTPUT if set, otherwise json.
func (p *EnvProvider) Output() string {
	if v := os.Getenv(envOutput); v != "" {
		return v
	}
	return DefaultOutput
}

// Analytics returns false only when HEYGEN_NO_ANALYTICS is explicitly set.
// Unset returns nil to preserve the consent-needed state.
func (p *EnvProvider) Analytics() *bool {
	if os.Getenv(envNoAnalytics) != "" {
		v := false
		return &v
	}
	return nil
}

// AutoUpdate returns false only when HEYGEN_NO_UPDATE_CHECK is set.
func (p *EnvProvider) AutoUpdate() bool {
	return os.Getenv(envNoAutoUpdate) == ""
}

// GetEnv reports whether the env var for a config key is explicitly set.
func (p *EnvProvider) GetEnv(key string) (string, bool) {
	envVar, ok := envVarByKey[key]
	if !ok {
		return "", false
	}

	val := os.Getenv(envVar)
	return val, val != ""
}
