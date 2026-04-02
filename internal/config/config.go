package config

// Provider supplies non-secret configuration values using a precedence chain:
// CLI flag > environment variable > config file > built-in default.
//
// Credentials are not part of config — see auth.CredentialResolver.
type Provider interface {
	BaseURL() string
	Output() string
	Analytics() bool
	AutoUpdate() bool
}

// Source captures an effective config value and where it came from.
type Source struct {
	Value  string
	Origin string
}

// ProviderWithSource exposes value origins for config inspection commands.
type ProviderWithSource interface {
	Provider
	Resolve(key string) (Source, error)
}

// WritableProvider persists configuration values.
type WritableProvider interface {
	Set(key, value string) error
}

const (
	KeyOutput     = "output"
	KeyAnalytics  = "analytics"
	KeyAutoUpdate = "auto_update"
)

// ValidKeys lists config keys exposed by config set/get/list.
var ValidKeys = []string{KeyAnalytics, KeyAutoUpdate, KeyOutput}
