package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Overrides contains codegen configuration that can't be derived from the OpenAPI spec.
type Overrides struct {
	// Groups maps OpenAPI tag → CLI group name (e.g., "Video Translate" → "translate")
	Groups map[string]string `yaml:"groups"`

	// Positional maps "METHOD /path" → list of fields to promote to positional args
	Positional map[string][]PositionalOverride `yaml:"positional"`

	// Examples maps "METHOD /path" → list of usage examples
	Examples map[string][]string `yaml:"examples"`
}

// PositionalOverride defines a body or file field promoted to a positional arg.
type PositionalOverride struct {
	Field  string `yaml:"field"`
	Target string `yaml:"target"` // "body" or "file"
}

// LoadOverrides reads a YAML overrides file.
func LoadOverrides(path string) (*Overrides, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading overrides: %w", err)
	}

	var o Overrides
	if err := yaml.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("parsing overrides YAML: %w", err)
	}

	if o.Groups == nil {
		o.Groups = make(map[string]string)
	}
	if o.Positional == nil {
		o.Positional = make(map[string][]PositionalOverride)
	}
	if o.Examples == nil {
		o.Examples = make(map[string][]string)
	}

	return &o, nil
}

// GetPositionalOverrides returns positional overrides for the given endpoint key.
func (o *Overrides) GetPositionalOverrides(key string) []PositionalOverride {
	if o == nil {
		return nil
	}
	return o.Positional[key]
}
