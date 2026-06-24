package auth

import (
	"fmt"
	"time"
)

// OAuthTokens is the resolver-layer view of the on-disk oauth block. It
// is the unit `SaveOAuthTokens` / `LoadOAuthTokens` operate on, and
// mirrors the persisted JSON shape one-to-one.
type OAuthTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Scope        string
	TokenType    string
}

// SaveOAuthTokens writes the OAuth block to the shared credentials file,
// preserving any co-located api_key. The file is created with 0600 in a
// 0700 parent directory.
//
// A pre-existing malformed credentials file is refused (same contract as
// FileCredentialStore.Save) so we don't silently destroy a recoverable
// credential.
func SaveOAuthTokens(tok OAuthTokens) error {
	if tok.AccessToken == "" && tok.RefreshToken == "" {
		return fmt.Errorf("auth: SaveOAuthTokens called with no tokens")
	}
	path := credentialFilePath()
	existing, format, err := loadCredentialsFile(path)
	if err != nil {
		if format != formatAbsent {
			return fmt.Errorf("%w; delete the file and re-run `heygen auth login`", err)
		}
		existing = jsonCredentials{}
	}
	existing.OAuth = &jsonOAuthTokens{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Scope:        tok.Scope,
		TokenType:    tok.TokenType,
	}
	if !tok.ExpiresAt.IsZero() {
		existing.OAuth.ExpiresAt = tok.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return writeCredentialsFile(path, existing)
}

// LoadOAuthTokens reads the OAuth block from disk. Returns ErrNotConfigured
// when the file is missing OR when it is present but holds no OAuth
// block at all (callers usually want to distinguish "no file" from "file
// present but no oauth section"; we collapse both into ErrNotConfigured
// because the resulting UX is the same: "run heygen auth login").
func LoadOAuthTokens() (OAuthTokens, error) {
	path := credentialFilePath()
	parsed, format, err := loadCredentialsFile(path)
	if format == formatAbsent && err == nil {
		return OAuthTokens{}, &ErrNotConfigured{Msg: "no credentials file"}
	}
	if err != nil {
		return OAuthTokens{}, err
	}
	if parsed.OAuth == nil || (parsed.OAuth.AccessToken == "" && parsed.OAuth.RefreshToken == "") {
		return OAuthTokens{}, &ErrNotConfigured{Msg: "no OAuth session in credentials file"}
	}
	tok := OAuthTokens{
		AccessToken:  parsed.OAuth.AccessToken,
		RefreshToken: parsed.OAuth.RefreshToken,
		Scope:        parsed.OAuth.Scope,
		TokenType:    parsed.OAuth.TokenType,
	}
	if parsed.OAuth.ExpiresAt != "" {
		if t, perr := time.Parse(time.RFC3339, parsed.OAuth.ExpiresAt); perr == nil {
			tok.ExpiresAt = t
		}
	}
	return tok, nil
}

// ClearOAuthTokens removes the oauth block from disk, optionally also
// removing the api_key (when alsoAPIKey is true). When the resulting
// credential file would be empty (no api_key, no oauth) the file is
// removed entirely.
//
// Returns nil (no-op) when the credential file is absent — the
// post-condition the caller wants is "no oauth session on disk", which
// is already true.
func ClearOAuthTokens(alsoAPIKey bool) error {
	path := credentialFilePath()
	parsed, format, err := loadCredentialsFile(path)
	if format == formatAbsent {
		// File is already gone — same effective state.
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w; delete the file and try again", err)
	}
	parsed.OAuth = nil
	if alsoAPIKey {
		parsed.APIKey = ""
	}
	if parsed.APIKey == "" && parsed.OAuth == nil {
		return removeCredentialsFile(path)
	}
	return writeCredentialsFile(path, parsed)
}
