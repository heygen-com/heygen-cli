package auth

import "time"

// CredentialType discriminates the kinds of credential the resolver can
// hand to the transport layer.
type CredentialType int

const (
	// CredentialTypeAPIKey is a static API key sent via the x-api-key
	// header.
	CredentialTypeAPIKey CredentialType = iota
	// CredentialTypeOAuth is an OAuth access token (Bearer) that is
	// currently within its lifetime.
	CredentialTypeOAuth
	// CredentialTypeOAuthExpired is an OAuth credential whose access
	// token is past (or within the skew of) its expiry, but for which we
	// still hold a refresh token. The transport refreshes before the
	// first request.
	CredentialTypeOAuthExpired
)

// Credential is the typed credential the resolver hands to the client
// transport. Exactly one of APIKey or (AccessToken / RefreshToken) is
// populated, gated by Type.
type Credential struct {
	Type CredentialType

	// APIKey is populated when Type == CredentialTypeAPIKey.
	APIKey string

	// AccessToken is populated when Type == CredentialTypeOAuth.
	AccessToken string

	// RefreshToken is populated when Type == CredentialTypeOAuth (for
	// transparent refresh on 401 or near expiry) or
	// CredentialTypeOAuthExpired (the only thing left to refresh with).
	// Empty if the IdP did not issue one — in that case the only
	// recovery is `heygen auth login`.
	RefreshToken string

	// ExpiresAt is populated when Type is OAuth/OAuthExpired. Zero when
	// the IdP did not return an `expires_in` field.
	ExpiresAt time.Time

	// Scope mirrors the granted scope returned by the IdP. Informational
	// only; the transport does not enforce it.
	Scope string

	// Source describes where this credential came from ("env", "file").
	// Plumbed alongside the typed credential so error messages can be
	// source-aware ("your env var" vs "your stored key").
	Source CredentialSource
}

// IsOAuth reports whether c carries an OAuth access token (fresh OR in
// need of refresh).
func (c *Credential) IsOAuth() bool {
	if c == nil {
		return false
	}
	return c.Type == CredentialTypeOAuth || c.Type == CredentialTypeOAuthExpired
}

// HasRefreshToken reports whether c can be refreshed without a fresh
// browser login.
func (c *Credential) HasRefreshToken() bool {
	if c == nil {
		return false
	}
	return c.RefreshToken != ""
}
