package main

import (
	"strings"
	"testing"
)

// TestBrandKitListAliasResolves verifies the deprecated "heygen brand-kit list"
// path (shipped through v0.0.11) still resolves to the canonical brand-kits
// list handler after the EF 6ced9812 rename to "heygen brand kits list".
//
// The deprecation notice itself is verified in aliases_test.go (leaf.Deprecated
// is set). Cobra prints it via OutOrStderr, which is os.Stderr in production
// since main() does not call SetOut; the runCommand harness sets both writers,
// so notice routing is not asserted here.
func TestBrandKitListAliasResolves(t *testing.T) {
	server := setupTestServer(t, map[string]testHandler{
		"GET /v3/brand-kits": {
			StatusCode: 200,
			Body:       `{"data":[{"brand_kit_id":"bk_1","name":"Acme","logo_url":"https://x/y.png","colors":["#ffffff"]}]}`,
		},
	})
	defer server.Close()

	res := runCommand(t, server.URL, "test-key", "brand-kit", "list")

	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "bk_1") {
		t.Errorf("alias did not reach the canonical handler; stdout: %s", res.Stdout)
	}
}
