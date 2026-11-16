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

	home, err := HomeDir()
	if err != nil {
		return "", ErrNoConfigDir
	}

	return filepath.Join(home, ".heygen"), nil
}

// ErrNoHome means the home directory is unset or relative; a relative one would resolve under the working directory.
var ErrNoHome = errors.New("no absolute home directory")

// HomeDir returns the user's home directory, only when it is absolute.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", ErrNoHome
	}
	return home, nil
}

// ReplaceFile writes data (mode 0600, parent 0700) to a temp file beside path and renames it over path, so a crash
// never leaves a truncated file and a link at path is replaced, never written through.
func ReplaceFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
