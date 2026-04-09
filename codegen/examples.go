package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Examples maps "METHOD /path" → list of curated CLI usage examples.
// Hand-written to show real usage patterns. Mandatory for every generated
// command — make generate fails if any are missing.
type Examples map[string][]string

// Example is a single curated CLI example from YAML.
type Example struct {
	Desc string `yaml:"desc"`
	Cmd  string `yaml:"cmd"`
}

func (e Example) normalized() string {
	return fmt.Sprintf("# %s\n  %s", strings.TrimSpace(e.Desc), strings.TrimSpace(e.Cmd))
}

// LoadExamples reads examples from a YAML file or a directory of YAML files.
// When given a directory, all *.yaml files are loaded and merged. Duplicate
// endpoint keys across files produce an error.
func LoadExamples(path string) (Examples, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading examples: %w", err)
	}

	if !info.IsDir() {
		return loadExamplesFile(path)
	}

	// Directory: load and merge all .yaml files
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("reading examples directory: %w", err)
	}

	merged := make(Examples)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		file := filepath.Join(path, entry.Name())
		single, err := loadExamplesFile(file)
		if err != nil {
			return nil, err
		}
		for key, examples := range single {
			if _, exists := merged[key]; exists {
				return nil, fmt.Errorf("duplicate endpoint %q found in %s (already defined in another file)", key, entry.Name())
			}
			merged[key] = examples
		}
	}

	return merged, nil
}

func loadExamplesFile(path string) (Examples, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var raw map[string][]Example
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if raw == nil {
		return make(Examples), nil
	}

	examples := make(Examples, len(raw))
	for endpoint, items := range raw {
		normalized := make([]string, 0, len(items))
		for i, item := range items {
			if strings.TrimSpace(item.Desc) == "" {
				return nil, fmt.Errorf("parsing %s: endpoint %q example %d: missing desc", path, endpoint, i+1)
			}
			if strings.TrimSpace(item.Cmd) == "" {
				return nil, fmt.Errorf("parsing %s: endpoint %q example %d: missing cmd", path, endpoint, i+1)
			}
			normalized = append(normalized, item.normalized())
		}
		examples[endpoint] = normalized
	}

	return examples, nil
}
