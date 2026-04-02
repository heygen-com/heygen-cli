package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/heygen-com/heygen-cli/internal/paths"
)

// FileProvider reads and writes persisted CLI configuration.
type FileProvider struct{}

func (p *FileProvider) load() (map[string]any, error) {
	path := filepath.Join(paths.ConfigDir(), "config.toml")
	var data map[string]any
	_, err := toml.DecodeFile(path, &data)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}

	return data, nil
}

// Get reads a single key from config.toml.
func (p *FileProvider) Get(key string) (string, bool, error) {
	data, err := p.load()
	if err != nil {
		return "", false, err
	}
	if data == nil {
		return "", false, nil
	}

	v, ok := data[key]
	if !ok {
		return "", false, nil
	}

	return fmt.Sprintf("%v", v), true, nil
}

// Set writes a key-value pair to config.toml, preserving existing keys.
func (p *FileProvider) Set(key, value string) error {
	data, err := p.load()
	if err != nil {
		return err
	}
	if data == nil {
		data = make(map[string]any)
	}

	dir := paths.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data[key] = coerceValue(key, value)

	path := filepath.Join(dir, "config.toml")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open config file %s: %w", path, err)
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(data)
}

func coerceValue(key, value string) any {
	switch key {
	case KeyAnalytics:
		return value == "true"
	default:
		return value
	}
}
