package errors

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// codeLiteral matches a CLI-minted error-code string literal: either a `Code:`
// struct field or a `code =` assignment (the two forms the CLI uses to
// synthesize a code). API codes relayed from a response come from a variable
// (apiErr.Code), not a literal, so they do not match.
var codeLiteral = regexp.MustCompile(`(?:\bCode:\s*|\bcode\s*=\s*)"([a-z][a-z0-9_]*)"`)

func registrySet(t *testing.T) map[string]string {
	t.Helper()
	m := map[string]string{}
	add := func(codes []string, bucket string) {
		for _, c := range codes {
			if prev, dup := m[c]; dup {
				t.Fatalf("code %q registered in both %q and %q", c, prev, bucket)
			}
			m[c] = bucket
		}
	}
	add(cliPrefixedCodes, "cliPrefixed")
	add(grandfatheredBareCodes, "grandfatheredBare")
	add(bareAPISemanticCodes, "bareAPISemantic")
	return m
}

// TestCLICodePartition enforces the namespace invariants on the registry.
func TestCLICodePartition(t *testing.T) {
	for _, c := range cliPrefixedCodes {
		if !strings.HasPrefix(c, CLICodePrefix) {
			t.Errorf("cliPrefixedCodes: %q must start with %q", c, CLICodePrefix)
		}
	}
	for _, c := range append(append([]string{}, grandfatheredBareCodes...), bareAPISemanticCodes...) {
		if strings.HasPrefix(c, CLICodePrefix) {
			t.Errorf("bare code %q must not start with %q (only cliPrefixedCodes may)", c, CLICodePrefix)
		}
	}
}

// TestBareAllowlistsAreFrozen pins the exact contents of the two bare sets.
// The reserved-prefix guarantee relies on these being CLOSED: without this,
// a new bare CLI code could bypass the cli_ rule by simply being appended to
// grandfatheredBareCodes. Changing either set now requires editing this
// expected list too, which surfaces the change in review.
func TestBareAllowlistsAreFrozen(t *testing.T) {
	wantGrandfathered := []string{
		"auth_error", "batch_not_supported", "canceled", "confirmation_required",
		"error", "file_exists", "network_error", "timeout", "usage_error",
		"video_failed", "video_not_ready", "wrong_install_method",
	}
	wantAPISemantic := []string{
		"asset_not_available", "conflict", "forbidden", "insufficient_credit",
		"not_found", "payload_too_large", "rate_limit_exceeded", "unauthorized",
		"unclassified_client_error", "unclassified_server_error", "validation_error",
	}
	assertSetEqual(t, "grandfatheredBareCodes", grandfatheredBareCodes, wantGrandfathered)
	assertSetEqual(t, "bareAPISemanticCodes", bareAPISemanticCodes, wantAPISemantic)
}

func assertSetEqual(t *testing.T, name string, got, want []string) {
	t.Helper()
	gm := map[string]bool{}
	for _, c := range got {
		gm[c] = true
	}
	wm := map[string]bool{}
	for _, c := range want {
		wm[c] = true
	}
	for c := range gm {
		if !wm[c] {
			t.Errorf("%s: unexpected code %q — if this is a new bare CLI code, it likely must be cli_-prefixed instead; only add to the frozen set with explicit justification", name, c)
		}
	}
	for c := range wm {
		if !gm[c] {
			t.Errorf("%s: missing expected code %q", name, c)
		}
	}
}

// TestNoUnregisteredCLICodes scans production source for CLI-minted code
// literals and fails on any that is not registered. A new CLI code therefore
// must be added to the registry, and (unless grandfathered/API-semantic) must
// carry the cli_ prefix, which is what keeps future codes collision-proof.
func TestNoUnregisteredCLICodes(t *testing.T) {
	reg := registrySet(t)
	root := repoRoot(t)

	// Codes the CLI never originates: relayed from tests as API-response bodies,
	// or test-only fixtures. These are not CLI-minted, so exclude them.
	ignore := map[string]bool{}

	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range codeLiteral.FindAllStringSubmatch(string(src), -1) {
				code := m[1]
				if ignore[code] {
					continue
				}
				if _, ok := reg[code]; !ok {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("unregistered CLI-minted code %q in %s: prefix it with %q and add it to cliPrefixedCodes in codes.go (or, if it mirrors an API code / is grandfathered, add it to the matching bare set)", code, rel, CLICodePrefix)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// this file lives at <root>/internal/errors/codes_test.go
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
