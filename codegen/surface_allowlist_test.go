package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The release-time surface check (scripts/release-surface.sh, documented in
// RELEASE.md) reduces gen/ to the fields that decide what a user can type. It
// filters with an allowlist, so a field the template emits but the allowlist
// omits is invisible to the check forever — a regression in it would pass a
// release gate that reports "clean".
//
// This shipped broken once: SendDefaultWhenOmitted, which decides whether a
// value is sent when the user omits the flag, was emitted by the template and
// missing from the allowlist. It escaped review because the inventory was taken
// over gen/ rather than the template, and no endpoint currently triggers its
// emit condition — so it was absent from the output while being one spec change
// away from appearing. Hence this test reads the template, not gen/.
//
// Excluded on purpose:
//   - prose fields, which churn on nearly every resync and are not contract
//   - container lines, whose entries are matched individually by the allowlist
var (
	proseFields     = map[string]bool{"Description": true, "Summary": true, "Help": true, "Examples": true}
	containerFields = map[string]bool{"Flags": true, "Args": true}

	templateFieldRe = regexp.MustCompile(`(?m)^\s*([A-Za-z][A-Za-z0-9]*):`)
	allowlistRe     = regexp.MustCompile(`(?m)^SURFACE_FIELDS='(.*)'$`)
)

func TestSurfaceAllowlistCoversEveryTemplateField(t *testing.T) {
	tmpl, err := os.ReadFile(filepath.Join("templates", "command.go.tmpl"))
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	script, err := os.ReadFile(filepath.Join("..", "scripts", "release-surface.sh"))
	if err != nil {
		t.Fatalf("read release-surface.sh: %v", err)
	}

	m := allowlistRe.FindSubmatch(script)
	if m == nil {
		t.Fatal("no SURFACE_FIELDS assignment found in scripts/release-surface.sh — " +
			"if it was renamed or reformatted, update this test, because without it the allowlist can rot silently")
	}
	allowed := make(map[string]bool)
	for _, alt := range strings.Split(string(m[1]), "|") {
		// Alternatives look like `Group:` or `\{(Name` — take the leading identifier.
		if name := regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*`).FindString(alt); name != "" {
			allowed[name] = true
		}
	}

	var missing []string
	for _, f := range templateFieldRe.FindAllSubmatch(tmpl, -1) {
		name := string(f[1])
		if allowed[name] || proseFields[name] || containerFields[name] {
			continue
		}
		missing = append(missing, name)
	}

	if len(missing) > 0 {
		t.Errorf("codegen/templates/command.go.tmpl emits %v, which SURFACE_FIELDS in "+
			"scripts/release-surface.sh does not match. A change to that field would be invisible to the "+
			"release regression check. Add it to the allowlist, or to proseFields/containerFields here if it "+
			"genuinely is not part of the command surface.", missing)
	}
}

// The allowlist is only meaningful if it actually selects the lines it claims
// to. Guards against a well-formed regex that matches nothing — the failure
// mode the script's own runtime check exists for, caught here at build time.
func TestSurfaceAllowlistMatchesRealGeneratedLines(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "scripts", "release-surface.sh"))
	if err != nil {
		t.Fatalf("read release-surface.sh: %v", err)
	}
	m := allowlistRe.FindSubmatch(script)
	if m == nil {
		t.Fatal("no SURFACE_FIELDS assignment found")
	}
	// Go's regexp is RE2; the pattern is a POSIX ERE alternation, which is a
	// compatible subset for the constructs used here.
	re, err := regexp.Compile(`^\s*(` + string(m[1]) + `)`)
	if err != nil {
		t.Fatalf("SURFACE_FIELDS is not a valid pattern: %v", err)
	}

	sample, err := os.ReadFile(filepath.Join("..", "gen", "video.go"))
	if err != nil {
		t.Fatalf("read gen/video.go: %v", err)
	}
	var matched int
	for _, line := range strings.Split(string(sample), "\n") {
		if re.MatchString(line) {
			matched++
		}
	}
	if matched == 0 {
		t.Error("SURFACE_FIELDS matched no line in gen/video.go — the release check would report every " +
			"release as clean")
	}
}
