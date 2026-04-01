package paths

import (
	"os"
	"path/filepath"
)

// ConfigDir returns the CLI config directory. Defaults to ~/.heygen.
// Override with HEYGEN_CONFIG_DIR for tests and non-standard setups.
func ConfigDir() string {
	if dir := os.Getenv("HEYGEN_CONFIG_DIR"); dir != "" {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ".heygen"
	}

	return filepath.Join(home, ".heygen")
}
