package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

// credentialFormat classifies what was found on disk.
type credentialFormat int

const (
	formatAbsent credentialFormat = iota // no file
	formatJSON                           // new `{ "api_key": ..., "oauth": ... }` layout
	formatLegacy                         // single-line plaintext API key
)

// credentialFilePath is the shared store path. Matches the path
// hyperframes-CLI uses (no `.json` extension) so the two tools share
// one file.
func credentialFilePath() (string, error) {
	dir, err := paths.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

// CredentialsFileShown is the credentials file's path for a message, or a plain name when there is none.
func CredentialsFileShown() string {
	if path, err := credentialFilePath(); err == nil {
		return path
	}
	return "the credentials file"
}

// loadCredentialsFile reads and parses the credentials file. It does
// NOT pick a credential — callers decide oauth-vs-api_key. A missing
// file yields (empty, formatAbsent, nil). A malformed JSON or
// multi-line non-JSON file is a hard error (broken state, not absent).
func loadCredentialsFile(path string) (jsonCredentials, credentialFormat, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return jsonCredentials{}, formatAbsent, nil
		}
		return jsonCredentials{}, formatAbsent, fmt.Errorf("cannot read credentials file %s: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		// File exists but is blank — broken state, distinct from a
		// missing file. Surface as an error (callers that write, like
		// Save, tolerate it and start fresh).
		return jsonCredentials{}, formatAbsent, fmt.Errorf("credentials file %s is empty", path)
	}

	if isJSONObject(trimmed) {
		var parsed jsonCredentials
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return jsonCredentials{}, formatJSON, fmt.Errorf("credentials file %s contains invalid JSON: %w", path, err)
		}
		return parsed, formatJSON, nil
	}

	// Legacy plaintext: any single-line non-empty content. Multi-line
	// non-JSON is malformed.
	if strings.ContainsAny(trimmed, "\r\n") {
		return jsonCredentials{}, formatLegacy, fmt.Errorf("credentials file %s is malformed (multi-line, expected JSON or a single-line key)", path)
	}
	return jsonCredentials{APIKey: trimmed}, formatLegacy, nil
}

// writeCredentialsFile atomically writes creds as JSON with mode 0600,
// creating the parent directory (0700) if needed.
func writeCredentialsFile(path string, creds jsonCredentials) error {
	// G117: serializing the api_key field is the entire purpose of this
	// credential store; the result is written to a 0600 file, never logged.
	body, err := json.MarshalIndent(creds, "", "  ") //nolint:gosec // intentional credential persistence to a 0600 file
	if err != nil {
		return fmt.Errorf("failed to encode credentials: %w", err)
	}
	if err := paths.ReplaceFile(path, append(body, '\n')); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}
	return nil
}

// removeCredentialsFile deletes the credentials file. A missing file is
// not an error — the desired post-condition is "no file on disk."
func removeCredentialsFile(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove credentials file: %w", err)
	}
	return nil
}
