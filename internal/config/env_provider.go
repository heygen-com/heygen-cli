package config

import "os"

const (
	envOutput       = "HEYGEN_OUTPUT"
	envNoAnalytics  = "HEYGEN_NO_ANALYTICS"
	envNoAutoUpdate = "HEYGEN_NO_UPDATE_CHECK"

	DefaultBaseURL = "https://api.heygen.com"
	DefaultOutput  = "json"
)

var envVarByKey = map[string]string{
	KeyOutput:     envOutput,
	KeyAnalytics:  envNoAnalytics,
	KeyAutoUpdate: envNoAutoUpdate,
}

// EnvProvider implements Provider by reading from environment variables.
// Falls back to built-in defaults when a variable is not set.
type EnvProvider struct{}

// BaseURL returns the API base URL. Reads HEYGEN_API_BASE if set (undocumented,
// used by tests and internal dev only). Defaults to https://api.heygen.com.
func (p *EnvProvider) BaseURL() string {
	if v := os.Getenv("HEYGEN_API_BASE"); v != "" {
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

// Analytics returns false when HEYGEN_NO_ANALYTICS is set, true otherwise.
func (p *EnvProvider) Analytics() bool {
	return os.Getenv(envNoAnalytics) == ""
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
