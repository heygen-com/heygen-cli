package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

// TestUserInfo_DisplayName covers the priority order (email > "first
// last" > username > "") the login surfaces lean on for the "Logged in
// as ..." line.
func TestUserInfo_DisplayName(t *testing.T) {
	cases := []struct {
		name string
		ui   UserInfo
		want string
	}{
		{"email_present", UserInfo{Email: "user@example.com", FirstName: "Jane", LastName: "Doe", Username: "jdoe"}, "user@example.com"},
		{"no_email_full_name", UserInfo{FirstName: "Jane", LastName: "Doe", Username: "jdoe"}, "Jane Doe"},
		{"only_first_name", UserInfo{FirstName: "Jane", Username: "jdoe"}, "Jane"},
		{"only_last_name", UserInfo{LastName: "Doe", Username: "jdoe"}, "Doe"},
		{"only_username", UserInfo{Username: "jdoe"}, "jdoe"},
		{"all_empty", UserInfo{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ui.DisplayName(); got != tc.want {
				t.Fatalf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestUserInfo_IsZero covers the gate callers use to decide whether to
// persist a user block at all.
func TestUserInfo_IsZero(t *testing.T) {
	if !(UserInfo{}).IsZero() {
		t.Errorf("empty UserInfo should be Zero")
	}
	if (UserInfo{Email: "u@example.com"}).IsZero() {
		t.Errorf("UserInfo with email should not be Zero")
	}
	if (UserInfo{Username: "u"}).IsZero() {
		t.Errorf("UserInfo with username should not be Zero")
	}
	if (UserInfo{FirstName: "J"}).IsZero() {
		t.Errorf("UserInfo with first name should not be Zero")
	}
	if (UserInfo{LastName: "D"}).IsZero() {
		t.Errorf("UserInfo with last name should not be Zero")
	}
}

// TestSaveAndLoadUserInfo round-trips the friendly-display block through
// the credentials file.
func TestSaveAndLoadUserInfo(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// Seed an api_key first so the file has a credential alongside the
	// user block (the production state).
	if err := (&FileCredentialStore{}).Save("hg_test"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ui := UserInfo{
		Email:     "user@example.com",
		FirstName: "Jane",
		LastName:  "Doe",
		Username:  "jdoe",
	}
	if err := SaveUserInfo(ui); err != nil {
		t.Fatalf("SaveUserInfo: %v", err)
	}

	got, err := LoadUserInfo()
	if err != nil {
		t.Fatalf("LoadUserInfo: %v", err)
	}
	if got != ui {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, ui)
	}
}

// TestSaveUserInfo_PreservesAPIKey verifies that persisting the user
// block does NOT wipe the api_key that's already on disk.
func TestSaveUserInfo_PreservesAPIKey(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	if err := (&FileCredentialStore{}).Save("hg_keep"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := SaveUserInfo(UserInfo{Email: "u@example.com"}); err != nil {
		t.Fatalf("SaveUserInfo: %v", err)
	}

	creds := parseStoredFile(t)
	if creds.APIKey != "hg_keep" {
		t.Fatalf("api_key wiped: got %q, want hg_keep", creds.APIKey)
	}
	if creds.User == nil || creds.User.Email != "u@example.com" {
		t.Fatalf("user block not persisted: %+v", creds.User)
	}
}

// TestSaveUserInfo_EmptyIsNoOp guards the IsZero() short-circuit so a
// failed probe doesn't rewrite the file with an empty user block.
func TestSaveUserInfo_EmptyIsNoOp(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	if err := (&FileCredentialStore{}).Save("hg_keep"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("read pre-state: %v", err)
	}

	if err := SaveUserInfo(UserInfo{}); err != nil {
		t.Fatalf("SaveUserInfo(empty): %v", err)
	}
	after, err := os.ReadFile(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("read post-state: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("file was rewritten on empty SaveUserInfo:\nbefore=%s\nafter=%s", before, after)
	}
}

// TestLoadUserInfo_AbsentFile returns (zero, nil) — the caller should
// treat this as "no friendly display available" rather than an error.
func TestLoadUserInfo_AbsentFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	got, err := LoadUserInfo()
	if err != nil {
		t.Fatalf("LoadUserInfo: unexpected error %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero UserInfo, got %+v", got)
	}
}

// TestLoadUserInfo_FileWithoutUserBlock_ReturnsZero is the backwards-
// compat case: an existing credentials file (api_key or oauth only,
// from before this change) must parse cleanly and yield a zero
// UserInfo — login surfaces fall back to user_id / "Logged in" until
// the next re-login populates the block.
func TestLoadUserInfo_FileWithoutUserBlock_ReturnsZero(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// Pre-this-change file shape (api_key only, no user block).
	seed := `{"api_key":"hg_legacy"}`
	dir := paths.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials"), []byte(seed), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := LoadUserInfo()
	if err != nil {
		t.Fatalf("LoadUserInfo: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero UserInfo for legacy file, got %+v", got)
	}

	// Sanity: the api_key must still resolve cleanly through the
	// normal resolver — the new schema field is purely additive.
	r := &FileCredentialResolver{}
	cred, err := r.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential on legacy file: %v", err)
	}
	if cred.APIKey != "hg_legacy" {
		t.Fatalf("api_key = %q, want hg_legacy", cred.APIKey)
	}
}

// TestClearUserInfo_RemovesOnlyUserBlock checks the credential survives
// when only the user block is cleared.
func TestClearUserInfo_RemovesOnlyUserBlock(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	if err := (&FileCredentialStore{}).Save("hg_keep"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := SaveUserInfo(UserInfo{Email: "u@example.com"}); err != nil {
		t.Fatalf("SaveUserInfo: %v", err)
	}

	if err := ClearUserInfo(); err != nil {
		t.Fatalf("ClearUserInfo: %v", err)
	}

	creds := parseStoredFile(t)
	if creds.APIKey != "hg_keep" {
		t.Fatalf("api_key wiped on user clear: got %q", creds.APIKey)
	}
	if creds.User != nil {
		t.Fatalf("user block survived clear: %+v", creds.User)
	}
}

// TestClearUserInfo_RemovesEmptyFile checks that clearing the user
// block when no credential is left removes the whole file (no orphan
// metadata).
func TestClearUserInfo_RemovesEmptyFile(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	// Seed a file with ONLY a user block (no credential). This is an
	// unusual state but the post-condition we want is "file gone."
	dir := paths.ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seed := `{"user":{"email":"u@example.com"}}`
	path := filepath.Join(dir, "credentials")
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := ClearUserInfo(); err != nil {
		t.Fatalf("ClearUserInfo: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, Stat returned %v", err)
	}
}

// TestClearUserInfo_NoUserBlock_NoOp verifies the function is safe to
// call when there's no user block to clear (the production "post-
// failed-probe" path on a fresh login).
func TestClearUserInfo_NoUserBlock_NoOp(t *testing.T) {
	t.Setenv("HEYGEN_CONFIG_DIR", t.TempDir())

	if err := (&FileCredentialStore{}).Save("hg_only"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("read pre-state: %v", err)
	}

	if err := ClearUserInfo(); err != nil {
		t.Fatalf("ClearUserInfo: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(paths.ConfigDir(), "credentials"))
	if err != nil {
		t.Fatalf("read post-state: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("file was rewritten despite no user block:\nbefore=%s\nafter=%s", before, after)
	}
}

// TestJSONUserInfoOmitempty checks that every field on jsonUserInfo
// uses omitempty so a partially-populated block doesn't litter the
// credentials file with empty strings. (Catches schema-drift if a
// future field forgets the tag.)
func TestJSONUserInfoOmitempty(t *testing.T) {
	creds := jsonCredentials{
		APIKey: "hg_test",
		User:   &jsonUserInfo{Email: "u@example.com"},
	}
	body, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(body)
	for _, banned := range []string{`"first_name":""`, `"last_name":""`, `"username":""`} {
		if contains := indexOf(got, banned); contains >= 0 {
			t.Errorf("found %q in marshalled output (should be omitted):\n%s", banned, got)
		}
	}
}

// indexOf is a tiny helper so the omitempty test reads naturally
// without pulling in strings.Contains semantics.
func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
