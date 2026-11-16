package paths

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNoConfigDir means neither HEYGEN_CONFIG_DIR nor an absolute home directory names where settings live.
var ErrNoConfigDir = errors.New("no home directory to keep heygen settings in; set HEYGEN_CONFIG_DIR to a folder")

// ConfigDir returns the CLI config directory. Defaults to ~/.heygen.
// Override with HEYGEN_CONFIG_DIR for tests and non-standard setups.
// It is never derived from the working directory.
func ConfigDir() (string, error) {
	if dir := os.Getenv("HEYGEN_CONFIG_DIR"); dir != "" {
		return dir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", ErrNoConfigDir
	}

	return filepath.Join(home, ".heygen"), nil
}
