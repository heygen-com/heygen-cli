package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Overrides contains codegen configuration that can't be derived from the OpenAPI spec.
type Overrides struct {
	// Groups maps OpenAPI tag → CLI group name (e.g., "Video Translate" → "translate")
	Groups map[string]string `yaml:"groups"`

	// Skip is a list of patterns to exclude (e.g., "* /v1/*", "* /v2/*")
	Skip []string `yaml:"skip"`

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

// ShouldSkip returns true if the endpoint matches any skip pattern.
func (o *Overrides) ShouldSkip(method, path string) bool {
	if o == nil {
		return false
	}
	for _, pattern := range o.Skip {
		if matchSkipPattern(pattern, method, path) {
			return true
		}
	}
	return false
}

// GetPositionalOverrides returns positional overrides for the given endpoint key.
func (o *Overrides) GetPositionalOverrides(key string) []PositionalOverride {
	if o == nil {
		return nil
	}
	return o.Positional[key]
}

// matchSkipPattern matches patterns like "* /v1/*", "GET /v2/videos".
// The path pattern supports * as a wildcard that matches any characters
// including path separators (unlike filepath.Match).
func matchSkipPattern(pattern, method, path string) bool {
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) != 2 {
		return false
	}

	methodPattern := parts[0]
	pathPattern := parts[1]

	// Match method
	if methodPattern != "*" && !strings.EqualFold(methodPattern, method) {
		return false
	}

	// Match path: support prefix/* patterns and exact match
	if strings.HasSuffix(pathPattern, "/*") {
		prefix := strings.TrimSuffix(pathPattern, "/*")
		return strings.HasPrefix(path, prefix+"/")
	}

	return pathPattern == path
}
