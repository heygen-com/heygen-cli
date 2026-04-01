package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

// FileCredentialResolver reads the API key from the credentials file.
type FileCredentialResolver struct{}

// Resolve returns the API key from the credentials file, classifying
// "not found" separately from broken file states.
func (r *FileCredentialResolver) Resolve() (string, error) {
	path := filepath.Join(paths.ConfigDir(), "credentials")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", &ErrNotConfigured{Msg: "no credentials file"}
		}
		return "", fmt.Errorf("cannot read credentials file %s: %w", path, err)
	}

	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("credentials file %s is empty", path)
	}

	return key, nil
}
