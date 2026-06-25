package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// pkceVerifierBytes produces a 64-byte random verifier, which encodes to
// 86 URL-safe base64 characters — comfortably inside the RFC 7636
// 43–128 range.
const pkceVerifierBytes = 64

// GeneratePKCEPair returns a PKCE (RFC 7636) code_verifier and the
// corresponding S256 code_challenge.
//
// The verifier is sent on the token exchange; the challenge is sent on
// the authorize URL. The server hashes the verifier and rejects the
// exchange if it doesn't match the challenge that opened the flow —
// removing the need for a client_secret on a public CLI client.
func GeneratePKCEPair() (verifier, challenge string, err error) {
	raw := make([]byte, pkceVerifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("oauth: read random bytes for PKCE verifier: %w", err)
	}
	verifier = base64URL(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64URL(sum[:])
	return verifier, challenge, nil
}

// GenerateState returns a random URL-safe string suitable for the OAuth
// `state` parameter (CSRF token for the authorize redirect).
func GenerateState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("oauth: read random bytes for state: %w", err)
	}
	return base64URL(raw), nil
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
