package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/heygen-com/heygen-cli/internal/paths"
)

// jsonCredentials is the on-disk format hyperframes-CLI (and future
// heygen-cli releases) write to ~/.heygen/credentials. Mirror the
// shape used by packages/cli/src/auth/store.ts in hyperframes-oss.
type jsonCredentials struct {
	APIKey string           `json:"api_key,omitempty"`
	OAuth  *jsonOAuthTokens `json:"oauth,omitempty"`
}

type jsonOAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// ExpiresAt is ISO-8601 UTC. The hyperframes resolver allows a 60s
	// skew; we use the same here so behavior matches across CLIs.
	ExpiresAt string `json:"expires_at,omitempty"`
	Scope     string `json:"scope,omitempty"`
	TokenType string `json:"token_type,omitempty"`
}

const oauthExpirySkew = 60 * time.Second

// FileCredentialResolver reads the API key from the credentials file.
type FileCredentialResolver struct{}

// Resolve returns a credential string from the shared credentials file.
//
// The on-disk format may be either:
//
//	1. The new JSON layout: `{ "api_key": "...", "oauth": { ... } }`
//	   produced by hyperframes-CLI's `auth login`. When both fields
//	   are present, an unexpired `oauth.access_token` wins; otherwise
//	   the `api_key` is returned.
//	2. The legacy single-line plaintext API key (what heygen-cli has
//	   written historically). Kept readable so existing users don't
//	   lose their session on upgrade.
//
// Future direction: when heygen-cli starts speaking OAuth at the HTTP
// layer, this function should return a typed credential so the
// client can pick the right header (Bearer vs x-api-key). For now,
// callers continue to treat the returned string as opaque.
func (r *FileCredentialResolver) Resolve() (string, error) {
	path := filepath.Join(paths.ConfigDir(), "credentials")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", &ErrNotConfigured{Msg: "no credentials file"}
		}
		return "", fmt.Errorf("cannot read credentials file %s: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "", fmt.Errorf("credentials file %s is empty", path)
	}

	if isJSONObject(trimmed) {
		return resolveFromJSON(trimmed, path, time.Now())
	}

	// Legacy plaintext: anything single-line non-empty. The new format
	// rejects garbage more strictly, but for the read-side we stay
	// permissive — hyperframes-CLI's tightening lives on the write side.
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", fmt.Errorf("credentials file %s is malformed (multi-line, expected JSON or a single-line key)", path)
	}
	return trimmed, nil
}

func isJSONObject(s string) bool {
	return len(s) > 0 && s[0] == '{'
}

// resolveFromJSON parses the new shape and picks the right credential.
// Errors that come out of here are typed as plain `error` (not
// ErrNotConfigured) — a parseable-but-content-less file is broken
// state, not "absent", and the chain resolver should surface it.
func resolveFromJSON(text, path string, now time.Time) (string, error) {
	var parsed jsonCredentials
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return "", fmt.Errorf("credentials file %s contains invalid JSON: %w", path, err)
	}

	// Prefer OAuth when its access_token is present and unexpired.
	// Expired-but-refreshable tokens stay unused here — refresh logic
	// lives in hyperframes-CLI. heygen-cli sees only the persisted
	// pair and treats expired access_tokens as 'not usable' so the
	// api_key (if any) wins instead.
	if parsed.OAuth != nil && parsed.OAuth.AccessToken != "" {
		if !isOAuthExpired(parsed.OAuth, now) {
			return parsed.OAuth.AccessToken, nil
		}
	}

	if parsed.APIKey != "" {
		return parsed.APIKey, nil
	}

	return "", fmt.Errorf("credentials file %s has no usable credential (oauth expired and no api_key)", path)
}

func isOAuthExpired(t *jsonOAuthTokens, now time.Time) bool {
	if t.ExpiresAt == "" {
		// No expiry stored — treat as fresh (we have no basis to
		// declare it expired).
		return false
	}
	expiry, err := time.Parse(time.RFC3339Nano, t.ExpiresAt)
	if err != nil {
		// Loose ISO-8601 fallback: try plain RFC3339.
		expiry, err = time.Parse(time.RFC3339, t.ExpiresAt)
		if err != nil {
			// Unparseable expires_at — be conservative and treat as
			// fresh; the server will reject if it's actually dead.
			return false
		}
	}
	return expiry.Add(-oauthExpirySkew).Before(now)
}
