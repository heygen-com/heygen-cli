// Package oauth implements the HeyGen public OAuth 2.0 + PKCE driver.
//
// PR 1 of the OAuth stack: this package is a self-contained driver with
// no callers yet. The CLI surface (`heygen auth login`), credential
// resolver wiring, and Bearer transport land in PR 2.
//
// The shared on-disk credentials file (~/.heygen/credentials) already
// understands the `oauth` block (see internal/auth/file_resolver.go),
// matching the JSON layout hyperframes-CLI writes. This driver returns
// TokenResponse values that the resolver/store layer will persist; it
// does not touch disk itself.
package oauth

import "time"

// TokenResponse is the RFC 6749 §5.1 token-endpoint response, restricted
// to the fields heygen-cli cares about. The IdP may include `id_token`
// (OIDC) and other extensions — we ignore them.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// ExpiresIn is the access-token lifetime in seconds, as returned by
	// the IdP. Use Expired() to compare against a wall clock; the package
	// does not compute an absolute expiry timestamp here (callers persist
	// the absolute timestamp into the credentials file).
	ExpiresIn int    `json:"expires_in,omitempty"`
	Scope     string `json:"scope,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	// IssuedAt is set by the driver to the wall-clock time the response
	// was parsed. It is not part of the on-the-wire payload.
	IssuedAt time.Time `json:"-"`
}

// Expired reports whether the access token is past its expiry, with the
// given skew subtracted from the lifetime (so callers can refresh
// proactively). A zero IssuedAt or zero ExpiresIn is treated as "no
// information," so Expired returns false rather than guessing.
func (t *TokenResponse) Expired(now time.Time, skew time.Duration) bool {
	if t == nil {
		return true
	}
	if t.IssuedAt.IsZero() || t.ExpiresIn <= 0 {
		return false
	}
	expiry := t.IssuedAt.Add(time.Duration(t.ExpiresIn) * time.Second)
	return !now.Before(expiry.Add(-skew))
}
