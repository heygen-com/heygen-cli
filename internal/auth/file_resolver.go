package auth

import "fmt"

// jsonCredentials is the on-disk format hyperframes-CLI (and heygen-cli,
// since the write-side change) persist to ~/.heygen/credentials. Mirror
// the shape used by packages/cli/src/auth/store.ts in hyperframes-oss.
type jsonCredentials struct {
	APIKey string           `json:"api_key,omitempty"`
	OAuth  *jsonOAuthTokens `json:"oauth,omitempty"`
}

// jsonOAuthTokens is parsed and round-tripped (so heygen-cli preserves a
// hyperframes-written OAuth block on save) but NOT selected as a usable
// credential — see selectCredential.
type jsonOAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Scope        string `json:"scope,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

// FileCredentialResolver reads the API key from the credentials file.
type FileCredentialResolver struct{}

// Resolve returns a credential string from the shared credentials file.
//
// The on-disk format may be either:
//
//	1. The JSON layout `{ "api_key": "...", "oauth": { ... } }` produced
//	   by hyperframes-CLI and by this CLI's own writes.
//	2. The legacy single-line plaintext API key (what heygen-cli wrote
//	   historically). Still readable so existing users aren't logged out.
//
// Reads are side-effect-free: a legacy plaintext file is NOT rewritten
// here. Upgrading to JSON happens only on an explicit write (Save), so
// passive commands can't silently convert a file that an older pinned
// binary might still need to read.
func (r *FileCredentialResolver) Resolve() (string, error) {
	path := credentialFilePath()
	creds, format, err := loadCredentialsFile(path)
	if format == formatAbsent && err == nil {
		return "", &ErrNotConfigured{Msg: "no credentials file"}
	}
	if err != nil {
		// Empty / invalid-JSON / multi-line garbage — broken state.
		// Surfaced as a plain error so the chain reports it instead of
		// silently skipping (distinct from ErrNotConfigured).
		return "", err
	}
	return selectCredential(creds, path)
}

func isJSONObject(s string) bool {
	return len(s) > 0 && s[0] == '{'
}

// selectCredential picks the credential heygen-cli will actually send.
//
// heygen-cli transmits credentials via the `x-api-key` header only (see
// internal/client) — it has no `Authorization: Bearer` support yet. So
// it can only use an `api_key`; an OAuth `access_token` must NOT be
// returned here, or it would be mis-sent as an API key and rejected.
// We therefore always prefer api_key and refuse an OAuth-only file with
// an actionable error. (The OAuth block is still parsed and preserved
// on write — see Save — just never selected.)
//
// Errors are plain `error` (not ErrNotConfigured) so the chain resolver
// surfaces a present-but-unusable file rather than skipping it.
func selectCredential(parsed jsonCredentials, path string) (string, error) {
	if parsed.APIKey != "" {
		return parsed.APIKey, nil
	}
	if parsed.OAuth != nil && parsed.OAuth.AccessToken != "" {
		return "", fmt.Errorf(
			"credentials file %s holds an OAuth session, which heygen-cli can't use yet; "+
				"run `heygen auth login` with an API key or set HEYGEN_API_KEY",
			path,
		)
	}
	return "", fmt.Errorf("credentials file %s has no usable credential", path)
}
