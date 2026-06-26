package auth

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Cross-CLI forward compatibility: the ~/.heygen/credentials file is
// SHARED with the Node hyperframes CLI (and any future tool). Either CLI
// may write keys this version doesn't model yet. To avoid one CLI
// silently clobbering the other's data on a read → write round-trip,
// every struct below captures the unrecognized keys it sees into an
// unexported `extra` bag at unmarshal time and re-emits them verbatim at
// marshal time. Known fields stay strictly typed and validated; the
// passthrough is purely additive and never feeds an HTTP header. This
// mirrors the Node-side hardening in hyperframes-oss
// (packages/cli/src/auth/store.ts).

// jsonCredentials is the on-disk format hyperframes-CLI (and heygen-cli,
// since the write-side change) persist to ~/.heygen/credentials. Mirror
// the shape used by packages/cli/src/auth/store.ts in hyperframes-oss.
//
// The `user` block is additive friendly-display metadata captured at
// login time from /v3/users/me — NOT a credential. It is safe to persist
// alongside either credential type and is cleared whenever the credential
// itself is cleared.
type jsonCredentials struct {
	APIKey string           `json:"api_key,omitempty"`
	OAuth  *jsonOAuthTokens `json:"oauth,omitempty"`
	User   *jsonUserInfo    `json:"user,omitempty"`
	// extra holds unknown/foreign top-level keys captured at read time so
	// the next write round-trips them instead of dropping another CLI's
	// data. Never serialized as its own key — see (un)marshalExtras.
	extra map[string]json.RawMessage
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
	// extra holds unknown keys seen inside the oauth sub-object (e.g. a
	// future `id_token`), round-tripped verbatim.
	extra map[string]json.RawMessage
}

// jsonUserInfo is the friendly-display block captured at login time from
// /v3/users/me. All fields are optional — a credentials file without a
// user block is fully backwards-compatible (existing logins surface only
// after re-login).
type jsonUserInfo struct {
	Email     string `json:"email,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
	// extra holds unknown keys seen inside the user sub-object (e.g. a
	// future `avatar_url`), round-tripped verbatim.
	extra map[string]json.RawMessage
}

// unmarshalExtras decodes data into the typed value pointed to by known
// (the caller passes a recursion-breaking alias) and, separately,
// captures every top-level key NOT named in knownKeys into a raw bag.
// Returns the bag (nil when there are no unknown keys) so the caller can
// stash it on the struct's `extra` field. Known fields stay strictly
// typed; only the residue is preserved opaquely.
func unmarshalExtras(data []byte, known any, knownKeys map[string]struct{}) (map[string]json.RawMessage, error) {
	if err := json.Unmarshal(data, known); err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	var extra map[string]json.RawMessage
	for k, v := range all {
		if _, ok := knownKeys[k]; ok {
			continue
		}
		if extra == nil {
			extra = make(map[string]json.RawMessage, len(all))
		}
		extra[k] = v
	}
	return extra, nil
}

// marshalExtras serializes the typed value pointed to by known (via the
// caller's alias) and merges the preserved unknown keys back in. Known
// fields are authoritative — collection already excluded them, so a
// collision can't normally occur; on the off chance one does, the known
// field wins. Keys are emitted in sorted order for deterministic,
// diff-friendly output.
func marshalExtras(known any, extra map[string]json.RawMessage) ([]byte, error) {
	knownBytes, err := json.Marshal(known)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return knownBytes, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(knownBytes, &merged); err != nil {
		return nil, err
	}
	if merged == nil {
		merged = make(map[string]json.RawMessage, len(extra))
	}
	for k, v := range extra {
		if _, clash := merged[k]; clash {
			continue // known field wins
		}
		merged[k] = v
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := []byte{'{'}
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf = append(buf, kb...)
		buf = append(buf, ':')
		buf = append(buf, merged[k]...)
	}
	buf = append(buf, '}')
	return buf, nil
}

var (
	knownCredentialsKeys = map[string]struct{}{"api_key": {}, "oauth": {}, "user": {}}
	knownOAuthKeys       = map[string]struct{}{
		"access_token": {}, "refresh_token": {}, "expires_at": {}, "scope": {}, "token_type": {},
	}
	knownUserKeys = map[string]struct{}{
		"email": {}, "first_name": {}, "last_name": {}, "username": {},
	}
)

func (c *jsonCredentials) UnmarshalJSON(data []byte) error {
	type alias jsonCredentials
	var a alias
	extra, err := unmarshalExtras(data, &a, knownCredentialsKeys)
	if err != nil {
		return err
	}
	*c = jsonCredentials(a)
	c.extra = extra
	return nil
}

func (c jsonCredentials) MarshalJSON() ([]byte, error) {
	type alias jsonCredentials
	return marshalExtras(alias(c), c.extra)
}

func (o *jsonOAuthTokens) UnmarshalJSON(data []byte) error {
	type alias jsonOAuthTokens
	var a alias
	extra, err := unmarshalExtras(data, &a, knownOAuthKeys)
	if err != nil {
		return err
	}
	*o = jsonOAuthTokens(a)
	o.extra = extra
	return nil
}

func (o jsonOAuthTokens) MarshalJSON() ([]byte, error) {
	type alias jsonOAuthTokens
	return marshalExtras(alias(o), o.extra)
}

func (u *jsonUserInfo) UnmarshalJSON(data []byte) error {
	type alias jsonUserInfo
	var a alias
	extra, err := unmarshalExtras(data, &a, knownUserKeys)
	if err != nil {
		return err
	}
	*u = jsonUserInfo(a)
	u.extra = extra
	return nil
}

func (u jsonUserInfo) MarshalJSON() ([]byte, error) {
	type alias jsonUserInfo
	return marshalExtras(alias(u), u.extra)
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
//  1. api_key wins. heygen-cli is agent-first; the API-key path is the
//     dominant use case, so when a file holds both we prefer it.
//     Going forward the runner clears the other block on login, so
//     this branch mostly matters for pre-this-change users who still
//     have a stale OAuth block co-located with the api_key they
//     re-saved.
//  2. A non-expired OAuth access_token.
//  3. An OAuth refresh_token (no fresh access_token) — caller will
//     refresh + persist before the first request.
//
// Errors are plain `error` (not ErrNotConfigured) so the chain resolver
// surfaces a present-but-unusable file rather than skipping it.
func selectCredential(parsed jsonCredentials, path string) (*Credential, error) {
	if parsed.APIKey != "" {
		return &Credential{
			Type:   CredentialTypeAPIKey,
			APIKey: parsed.APIKey,
			Source: SourceFile,
		}, nil
	}
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
		return nil, fmt.Errorf(
			"credentials file %s holds an expired OAuth session with no refresh token; run `heygen auth login`",
			path,
		)
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
