package auth

import "time"

// OAuthRefreshSkew is the canonical leeway used when deciding whether an
// OAuth access token is "still fresh."
//
// Two layers consult this:
//
//   - The resolver (file_resolver.go) treats a token within OAuthRefreshSkew
//     of its expiry as already expired, so the resolved credential surfaces
//     as CredentialTypeOAuthExpired and the transport refreshes before
//     issuing the first request.
//   - The transport (internal/client/client.go) consults it on every Do()
//     call so proactive refresh kicks in when the in-memory token nears
//     expiry mid-session.
//
// Both layers must agree on the boundary — a mismatch would either
// trip a redundant refresh round-trip or race the IdP's clock and
// produce avoidable 401s.
const OAuthRefreshSkew = 60 * time.Second
