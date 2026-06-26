package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

// readStoredFile returns the raw bytes of the credentials file.
func readStoredFile(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return data
}

// parseStoredFile decodes the credentials file as the JSON format.
func parseStoredFile(t *testing.T) jsonCredentials {
	t.Helper()
	var creds jsonCredentials
	if err := json.Unmarshal(readStoredFile(t), &creds); err != nil {
		t.Fatalf("stored file is not valid JSON: %v\ncontents: %s", err, readStoredFile(t))
	}
	return creds
}

func TestFileCredentialStore_Save(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	s := &FileCredentialStore{}
	if err := s.Save("test-key-123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	creds := parseStoredFile(t)
	if creds.APIKey != "test-key-123" {
		t.Fatalf("api_key = %q, want %q", creds.APIKey, "test-key-123")
	}
	if creds.OAuth != nil {
		t.Fatalf("did not expect an oauth block, got %+v", creds.OAuth)
	}
}

func TestFileCredentialStore_CreatesDirectory(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", filepath.Join(t.TempDir(), "nested"))

	s := &FileCredentialStore{}
	if err := s.Save("test-key-123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(paths.ConfigDir())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected config dir to exist")
	}
}

func TestFileCredentialStore_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not supported on Windows")
	}

	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	s := &FileCredentialStore{}
	if err := s.Save("test-key-123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Fatalf("perms = %o, want %o", perms, 0o600)
	}
}

func TestFileCredentialStore_OverwriteExisting(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	s := &FileCredentialStore{}
	if err := s.Save("first-key"); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := s.Save("second-key"); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	if got := parseStoredFile(t).APIKey; got != "second-key" {
		t.Fatalf("api_key = %q, want %q", got, "second-key")
	}
}

func TestFileCredentialStore_PreservesExistingOAuth(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// Simulate a JSON file written by hyperframes-CLI carrying an OAuth
	// session, then run heygen-cli's api-key save over it.
	path := filepath.Join(paths.ConfigDir(), "credentials")
	if err := os.MkdirAll(paths.ConfigDir(), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seed := `{"oauth":{"access_token":"at_keep","refresh_token":"rt_keep","expires_at":"2099-01-01T00:00:00Z"}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := &FileCredentialStore{}
	if err := s.Save("hg_new_key"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	creds := parseStoredFile(t)
	if creds.APIKey != "hg_new_key" {
		t.Fatalf("api_key = %q, want hg_new_key", creds.APIKey)
	}
	if creds.OAuth == nil || creds.OAuth.AccessToken != "at_keep" || creds.OAuth.RefreshToken != "rt_keep" {
		t.Fatalf("oauth block not preserved: %+v", creds.OAuth)
	}
}

func TestFileCredentialStore_PreservesUnknownFieldsThroughSave(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// The cross-CLI data-loss scenario exercised through the public Save
	// path: the Node hyperframes CLI wrote a top-level key (and a nested
	// oauth key) heygen-cli doesn't model. A heygen-cli `auth login`
	// (FileCredentialStore.Save) must NOT strip them.
	path := filepath.Join(paths.ConfigDir(), "credentials")
	if err := os.MkdirAll(paths.ConfigDir(), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seed := `{"oauth":{"access_token":"at_keep","id_token":"future_id"},"future_field":{"flag":true}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := &FileCredentialStore{}
	if err := s.Save("hg_new_key"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("on-disk file is not valid JSON: %v\ncontents: %s", err, data)
	}

	if onDisk["api_key"] != "hg_new_key" {
		t.Fatalf("api_key = %v, want hg_new_key", onDisk["api_key"])
	}
	ff, ok := onDisk["future_field"].(map[string]any)
	if !ok || ff["flag"] != true {
		t.Fatalf("unknown top-level future_field dropped by Save: %v", onDisk["future_field"])
	}
	oauth, ok := onDisk["oauth"].(map[string]any)
	if !ok {
		t.Fatalf("oauth block missing after Save: %v", onDisk)
	}
	if oauth["access_token"] != "at_keep" {
		t.Fatalf("oauth.access_token = %v, want at_keep", oauth["access_token"])
	}
	if oauth["id_token"] != "future_id" {
		t.Fatalf("unknown oauth.id_token dropped by Save: %v", oauth["id_token"])
	}
}

func TestFileCredentialStore_RefusesToClobberMalformedFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// A present-but-unparseable file might hold a recoverable oauth
	// block we can't see. Save must refuse rather than silently
	// overwrite it.
	path := filepath.Join(paths.ConfigDir(), "credentials")
	if err := os.MkdirAll(paths.ConfigDir(), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	garbage := `{"oauth":{"access_token":"at_maybe_recoverable"` // truncated JSON
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := &FileCredentialStore{}
	if err := s.Save("hg_new_key"); err == nil {
		t.Fatal("expected Save to refuse overwriting a malformed file, got nil")
	}

	// File must be untouched.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != garbage {
		t.Fatalf("malformed file was modified: %q", string(data))
	}
}

func TestFileCredentialStore_OverwritesEmptyFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// An empty file has nothing to preserve — Save should proceed.
	path := filepath.Join(paths.ConfigDir(), "credentials")
	if err := os.MkdirAll(paths.ConfigDir(), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := &FileCredentialStore{}
	if err := s.Save("hg_fresh"); err != nil {
		t.Fatalf("Save over empty file: %v", err)
	}
	if got := parseStoredFile(t).APIKey; got != "hg_fresh" {
		t.Fatalf("api_key = %q, want hg_fresh", got)
	}
}

func TestFileCredentialStore_UpgradesLegacyPlaintext(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// Seed a legacy single-line plaintext file, then save a new key.
	path := filepath.Join(paths.ConfigDir(), "credentials")
	if err := os.MkdirAll(paths.ConfigDir(), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("old-plaintext-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := &FileCredentialStore{}
	if err := s.Save("hg_replacement"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File must now be JSON with the new key (the legacy key is replaced
	// — Save sets the api_key the user just provided).
	creds := parseStoredFile(t)
	if creds.APIKey != "hg_replacement" {
		t.Fatalf("api_key = %q, want hg_replacement", creds.APIKey)
	}
}
