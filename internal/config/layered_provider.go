package config

// LayeredProvider resolves config values via env > file > default precedence.
type LayeredProvider struct {
	Env  *EnvProvider
	File *FileProvider
}

func defaultFor(key string) string {
	switch key {
	case KeyAPIBase:
		return DefaultBaseURL
	case KeyOutput:
		return DefaultOutput
	case KeyAnalytics:
		return "unset"
	case KeyAutoUpdate:
		return "true"
	default:
		return ""
	}
}

// Resolve returns the effective value and its origin.
func (p *LayeredProvider) Resolve(key string) (Source, error) {
	if _, ok := p.Env.GetEnv(key); ok {
		switch key {
		case KeyAnalytics:
			return Source{Value: "false", Origin: "env"}, nil
		case KeyAutoUpdate:
			return Source{Value: "false", Origin: "env"}, nil
		case KeyAPIBase:
			return Source{Value: p.Env.BaseURL(), Origin: "env"}, nil
		case KeyOutput:
			return Source{Value: p.Env.Output(), Origin: "env"}, nil
		}
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
	source, err := p.Resolve(KeyAPIBase)
	if err != nil {
		return DefaultBaseURL
	}
	return source.Value
}

func (p *LayeredProvider) Output() string {
	source, err := p.Resolve(KeyOutput)
	if err != nil {
		return DefaultOutput
	}
	return source.Value
}

func (p *LayeredProvider) Analytics() *bool {
	if _, ok := p.Env.GetEnv(KeyAnalytics); ok {
		v := false
		return &v
	}

	val, ok, err := p.File.Get(KeyAnalytics)
	if err != nil || !ok {
		return nil
	}

	v := val == "true"
	return &v
}

func (p *LayeredProvider) AutoUpdate() bool {
	source, err := p.Resolve(KeyAutoUpdate)
	if err != nil {
		return true
	}
	return source.Value == "true"
}
