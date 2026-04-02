package config

// LayeredProvider resolves config values via env > file > default precedence.
type LayeredProvider struct {
	Env  *EnvProvider
	File *FileProvider
}

func defaultFor(key string) string {
	switch key {
	case KeyOutput:
		return DefaultOutput
	case KeyAnalytics:
		return "true"
	case KeyAutoUpdate:
		return "true"
	default:
		return ""
	}
}

func (p *LayeredProvider) envSource(key string) (Source, bool) {
	if _, ok := p.Env.GetEnv(key); !ok {
		return Source{}, false
	}

	switch key {
	case KeyAnalytics:
		return Source{Value: "false", Origin: "env"}, true
	case KeyAutoUpdate:
		return Source{Value: "false", Origin: "env"}, true
	case KeyOutput:
		return Source{Value: p.Env.Output(), Origin: "env"}, true
	default:
		return Source{}, false
	}
}

func (p *LayeredProvider) resolvedValue(key, fallback string) string {
	source, err := p.Resolve(key)
	if err != nil {
		return fallback
	}
	return source.Value
}

// Resolve returns the effective value and its origin.
func (p *LayeredProvider) Resolve(key string) (Source, error) {
	if source, ok := p.envSource(key); ok {
		return source, nil
	}

	val, ok, err := p.File.Get(key)
	if err != nil {
		return Source{}, err
	}
	if ok {
		return Source{Value: val, Origin: "file"}, nil
	}

	return Source{Value: defaultFor(key), Origin: "default"}, nil
}

func (p *LayeredProvider) BaseURL() string {
	return p.Env.BaseURL()
}

func (p *LayeredProvider) Output() string {
	return p.resolvedValue(KeyOutput, DefaultOutput)
}

func (p *LayeredProvider) Analytics() bool {
	return p.resolvedValue(KeyAnalytics, "true") == "true"
}

func (p *LayeredProvider) AutoUpdate() bool {
	return p.resolvedValue(KeyAutoUpdate, "true") == "true"
}

// Set persists a configuration value via the file provider.
func (p *LayeredProvider) Set(key, value string) error {
	return p.File.Set(key, value)
}
