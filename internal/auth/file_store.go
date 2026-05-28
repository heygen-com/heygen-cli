package auth

import "fmt"

// FileCredentialStore writes the API key to the credentials file.
type FileCredentialStore struct{}

// Save writes the API key to the shared credentials file in the JSON
// format (`{ "api_key": "..." }`), upgrading a legacy plaintext file in
// place. Any co-located OAuth block is preserved so that saving an API
// key doesn't wipe an active OAuth session written by hyperframes-CLI.
func (s *FileCredentialStore) Save(apiKey string) error {
	path := credentialFilePath()

	// Read the existing file to preserve a co-located oauth block.
	existing, format, err := loadCredentialsFile(path)
	if err != nil {
		if format != formatAbsent {
			// The file is present but unparseable (malformed JSON /
			// multi-line garbage). We can't see whether it holds an
			// OAuth session, so refuse to overwrite it rather than
			// silently destroying a recoverable credential.
			return fmt.Errorf("%w; delete the file and re-run `heygen auth login`", err)
		}
		// Missing / empty / unreadable — nothing to preserve, so start
		// from a clean slate. (An unreadable file will fail the write
		// below too, surfacing the real error there.)
		existing = jsonCredentials{}
	}

	existing.APIKey = apiKey
	return writeCredentialsFile(path, existing)
}
