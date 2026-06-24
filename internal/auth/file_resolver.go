package auth

import (
	"fmt"
	"time"
)

// jsonCredentials is the on-disk format hyperframes-CLI (and heygen-cli,
// since the write-side change) persist to ~/.heygen/credentials. Mirror
// the shape used by packages/cli/src/auth/store.ts in hyperframes-oss.
type jsonCredentials struct {
	APIKey string           `json:"api_key,omitempty"`
	OAuth  *jsonOAuthTokens `json:"oauth,omitempty"`
}

// jsonOAuthTokens is parsed and round-tripped (so heygen-cli preserves a
// hyperframes-written OAuth block on save) and now also selectable by
// the resolver as the live credential.
type jsonOAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Scope        string `json:"scope,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

// nowFn is the wall-clock source used when comparing OAuth expiry. Tests
// override this to drive deterministic resolver paths.
var nowFn = time.Now

// FileCredentialResolver reads credentials from the shared credentials
// file. It supports both api_key and OAuth-token credentials, with the
// type discriminated by the on-disk content.
type FileCredentialResolver struct{}

// Resolve returns the API-key form of the resolved credential. Retained
// for backwards compatibility with callers (and the chain resolver) that
// only understand a single string. For OAuth-aware callers see
// ResolveCredential.
//
// When the resolved credential is an OAuth token, Resolve returns a
// non-ErrNotConfigured error explaining that OAuth credentials must be
// consumed via ResolveCredential / the OAuth-aware transport path. This
// preserves the historical contract for callers that haven't been
// upgraded yet, while letting the new path flow through cleanly.
func (r *FileCredentialResolver) Resolve() (string, error) {
	cred, err := r.ResolveCredential()
	if err != nil {
		return "", err
	}
	if cred.Type != CredentialTypeAPIKey {
		return "", fmt.Errorf(
			"credentials file holds an OAuth session; use the OAuth-aware transport (this is a heygen-cli internal API contract)",
		)
	}
	return cred.APIKey, nil
}

// ResolveCredential returns the typed credential heygen-cli will send.
// On-disk formats:
//
//  1. JSON layout `{ "api_key": "...", "oauth": { ... } }` produced
//     by hyperframes-CLI and by this CLI's own writes.
//  2. Legacy single-line plaintext API key (what heygen-cli wrote
//     historically). Still readable so existing users aren't logged out.
//
// Reads are side-effect-free: a legacy plaintext file is NOT rewritten
// here. Upgrading to JSON happens only on an explicit write (Save), so
// passive commands can't silently convert a file that an older pinned
// binary might still need to read.
func (r *FileCredentialResolver) ResolveCredential() (*Credential, error) {
	path := credentialFilePath()
	creds, format, err := loadCredentialsFile(path)
	if format == formatAbsent && err == nil {
		return nil, &ErrNotConfigured{Msg: "no credentials file"}
	}
	if err != nil {
		// Empty / invalid-JSON / multi-line garbage — broken state.
		// Surfaced as a plain error so the chain reports it instead of
		// silently skipping (distinct from ErrNotConfigured).
		return nil, err
	}
	return selectCredential(creds, path)
}

func isJSONObject(s string) bool {
	return len(s) > 0 && s[0] == '{'
}

// selectCredential picks the credential heygen-cli will actually send.
//
// Precedence (when both an OAuth block and an api_key are present):
//
//  1. A non-expired OAuth access_token wins. OAuth is the new
//     default-recommended path; an api_key that's still co-located is
//     treated as legacy.
//  2. An OAuth refresh_token (with no fresh access_token) wins — caller
//     will refresh + persist before the first request.
//  3. Otherwise fall back to api_key.
//
// Errors are plain `error` (not ErrNotConfigured) so the chain resolver
// surfaces a present-but-unusable file rather than skipping it.
func selectCredential(parsed jsonCredentials, path string) (*Credential, error) {
	if parsed.OAuth != nil && parsed.OAuth.AccessToken != "" {
		expiresAt := parseOAuthExpiry(parsed.OAuth.ExpiresAt)
		if !oauthExpired(expiresAt, nowFn()) {
			return &Credential{
				Type:         CredentialTypeOAuth,
				AccessToken:  parsed.OAuth.AccessToken,
				RefreshToken: parsed.OAuth.RefreshToken,
				ExpiresAt:    expiresAt,
				Scope:        parsed.OAuth.Scope,
				Source:       SourceFile,
			}, nil
		}
		if parsed.OAuth.RefreshToken != "" {
			return &Credential{
				Type:         CredentialTypeOAuthExpired,
				RefreshToken: parsed.OAuth.RefreshToken,
				ExpiresAt:    expiresAt,
				Scope:        parsed.OAuth.Scope,
				Source:       SourceFile,
			}, nil
		}
		// Expired access token, no refresh token, no api_key — unusable.
		if parsed.APIKey == "" {
			return nil, fmt.Errorf(
				"credentials file %s holds an expired OAuth session with no refresh token; run `heygen auth login`",
				path,
			)
		}
	}
	if parsed.OAuth != nil && parsed.OAuth.RefreshToken != "" && parsed.OAuth.AccessToken == "" {
		// Refresh-only block (no access token at all). Same shape as
		// "expired access token + refresh".
		return &Credential{
			Type:         CredentialTypeOAuthExpired,
			RefreshToken: parsed.OAuth.RefreshToken,
			Scope:        parsed.OAuth.Scope,
			Source:       SourceFile,
		}, nil
	}
	if parsed.APIKey != "" {
		return &Credential{
			Type:   CredentialTypeAPIKey,
			APIKey: parsed.APIKey,
			Source: SourceFile,
		}, nil
	}
	return nil, fmt.Errorf("credentials file %s has no usable credential", path)
}

// parseOAuthExpiry parses the RFC 3339 `expires_at` stored on disk.
// Returns a zero time when absent or unparseable — the caller then treats
// the token as "no expiry information" and falls back to refresh-on-401.
func parseOAuthExpiry(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// oauthExpired reports whether the access token at expiresAt is past
// (or within the clock skew of) now. A zero expiresAt is treated as
// "no information" and reported as NOT expired so the transport can
// optimistically try the access token and refresh on 401.
func oauthExpired(expiresAt, now time.Time) bool {
	if expiresAt.IsZero() {
		return false
	}
	return !now.Before(expiresAt.Add(-OAuthRefreshSkew))
}
