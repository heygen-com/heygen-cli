package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestGeneratePKCEPair_ProducesValidS256Pair(t *testing.T) {
	verifier, challenge, err := GeneratePKCEPair()
	if err != nil {
		t.Fatalf("GeneratePKCEPair: %v", err)
	}

	if got := len(verifier); got < 43 || got > 128 {
		t.Errorf("verifier length = %d, want in [43, 128]", got)
	}
	if strings.ContainsAny(verifier, "+/=") {
		t.Errorf("verifier %q is not base64url (contains padding/non-url chars)", verifier)
	}
	if strings.ContainsAny(challenge, "+/=") {
		t.Errorf("challenge %q is not base64url (contains padding/non-url chars)", challenge)
	}

	// Recompute the challenge from the verifier and compare. PKCE S256
	// is defined as base64url(SHA256(ASCII(verifier))).
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Errorf("challenge = %q, want %q", challenge, want)
	}
}

func TestGeneratePKCEPair_ReturnsDistinctValues(t *testing.T) {
	// Each call should produce a new verifier; collisions across 5 calls
	// would imply a broken random source.
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		v, _, err := GeneratePKCEPair()
		if err != nil {
			t.Fatalf("GeneratePKCEPair[%d]: %v", i, err)
		}
		if seen[v] {
			t.Fatalf("verifier collision at iteration %d: %q", i, v)
		}
		seen[v] = true
	}
}

func TestGenerateState_NonEmptyAndURLSafe(t *testing.T) {
	s, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	if s == "" {
		t.Fatal("state is empty")
	}
	if strings.ContainsAny(s, "+/=") {
		t.Errorf("state %q is not base64url", s)
	}
}

func TestGenerateState_DistinctValues(t *testing.T) {
	a, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState a: %v", err)
	}
	b, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState b: %v", err)
	}
	if a == b {
		t.Fatalf("two consecutive states were identical: %q", a)
	}
}
