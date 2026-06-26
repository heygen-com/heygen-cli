package auth

import "fmt"

// UserInfo is the resolver-layer view of the on-disk `user` block — the
// friendly-display metadata captured at login time from /v3/users/me.
//
// UserInfo is NOT a credential. It is additive metadata persisted next to
// the credential so `auth status` (and any other friendly-display
// surface) can show "Logged in as user@example.com" without re-hitting
// the API on every invocation, and so the display still works when the
// API is unreachable.
type UserInfo struct {
	Email     string
	FirstName string
	LastName  string
	Username  string
}

// IsZero reports whether ui carries no friendly fields. Used by callers
// (e.g. auth status) to decide whether to surface the user block at all.
func (ui UserInfo) IsZero() bool {
	return ui.Email == "" && ui.FirstName == "" && ui.LastName == "" && ui.Username == ""
}

// DisplayName returns the most friendly name available, in priority
// order: email > "first last" > username > "" (caller falls back to
// user_id or another marker).
func (ui UserInfo) DisplayName() string {
	if ui.Email != "" {
		return ui.Email
	}
	if name := combineName(ui.FirstName, ui.LastName); name != "" {
		return name
	}
	return ui.Username
}

// combineName joins a first + last into "First Last", tolerating either
// half being empty.
func combineName(first, last string) string {
	switch {
	case first != "" && last != "":
		return first + " " + last
	case first != "":
		return first
	default:
		return last
	}
}

// SaveUserInfo persists the friendly-display block to the shared
// credentials file, preserving any co-located api_key / oauth blocks.
//
// A pre-existing malformed credentials file is refused (same contract as
// FileCredentialStore.Save / SaveOAuthTokens) so we don't silently destroy
// a recoverable credential.
//
// Calling SaveUserInfo with an all-empty UserInfo is a no-op write that
// still preserves the existing file; callers should typically check
// ui.IsZero() and skip the call in that case so the file isn't touched
// when there's nothing to persist.
func SaveUserInfo(ui UserInfo) error {
	path := credentialFilePath()
	existing, format, err := loadCredentialsFile(path)
	if err != nil {
		if format != formatAbsent {
			return fmt.Errorf("%w; delete the file and re-run `heygen auth login`", err)
		}
		existing = jsonCredentials{}
	}
	if ui.IsZero() {
		// Nothing to persist — leave the existing file untouched rather
		// than rewriting it with no effective change.
		return nil
	}
	existing.User = &jsonUserInfo{
		Email:     ui.Email,
		FirstName: ui.FirstName,
		LastName:  ui.LastName,
		Username:  ui.Username,
	}
	return writeCredentialsFile(path, existing)
}

// LoadUserInfo reads the friendly-display block from disk. Returns a
// zero-value UserInfo (with no error) when the credentials file is
// absent or when it is present but holds no user block — the caller
// then falls back to whatever display is appropriate (user_id, "Logged
// in", etc).
//
// Errors are surfaced for genuinely broken files so the caller can warn
// rather than silently pretend the file is clean.
func LoadUserInfo() (UserInfo, error) {
	path := credentialFilePath()
	parsed, format, err := loadCredentialsFile(path)
	if format == formatAbsent && err == nil {
		return UserInfo{}, nil
	}
	if err != nil {
		return UserInfo{}, err
	}
	if parsed.User == nil {
		return UserInfo{}, nil
	}
	return UserInfo{
		Email:     parsed.User.Email,
		FirstName: parsed.User.FirstName,
		LastName:  parsed.User.LastName,
		Username:  parsed.User.Username,
	}, nil
}

// ClearUserInfo removes the friendly-display block from disk, leaving
// any co-located credential blocks intact. When the resulting credential
// file would be empty (no api_key, no oauth, no user) the file is
// removed entirely.
//
// Returns nil (no-op) when the credential file is absent or already
// holds no user block — the post-condition the caller wants is "no
// user block on disk", which is already true.
func ClearUserInfo() error {
	path := credentialFilePath()
	parsed, format, err := loadCredentialsFile(path)
	if format == formatAbsent {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w; delete the file and try again", err)
	}
	if parsed.User == nil {
		return nil
	}
	parsed.User = nil
	if parsed.APIKey == "" && parsed.OAuth == nil {
		return removeCredentialsFile(path)
	}
	return writeCredentialsFile(path, parsed)
}
