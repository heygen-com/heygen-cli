package auth

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

// FileCredentialStore writes the API key to the credentials file.
type FileCredentialStore struct{}

// Save writes the API key to the credentials file with restrictive perms.
func (s *FileCredentialStore) Save(apiKey string) error {
	dir := paths.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	path := filepath.Join(dir, "credentials")
	return os.WriteFile(path, []byte(apiKey+"\n"), 0o600)
}
