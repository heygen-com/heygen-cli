package main

import (
	"os"
	"os/exec"
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

// Behavioral pin for the reduction, exercised through the script itself rather
// than by grepping it for an implementation detail. A rewrite in another tool
// keeps passing; a subtly wrong edit — adding a `g` flag that over-collapses, or
// reordering the stages — fails.
//
// The three cases are the ones that decide whether the check is trustworthy:
// a gofmt re-pad must vanish, a real removal must survive, and a value change
// must survive.
func TestSurfaceReductionSuppressesPaddingButNotChanges(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	// Go's test cache keys on Go inputs, not on the shell script this shells out
	// to, so a stale PASS is possible after editing the script alone. Run with
	// -count=1 when changing release-surface.sh.
	script := filepath.Join("..", "scripts", "release-surface.sh")

	// Same fields and values; only gofmt's alignment column differs, exactly as
	// adding a longer sibling field name produces.
	narrow := "\t\t\tName:     \"brand-voice-id\",\n\t\t\tSource:   \"body\",\n\t\t\tEndpoint: \"/v2/videos:generate\",\n\t\t\tDefault:  \"fmt: pretty\",\n"
	repadded := "\t\t\tName:       \"brand-voice-id\",\n\t\t\tSource:     \"body\",\n\t\t\tEndpoint:   \"/v2/videos:generate\",\n\t\t\tDefault:    \"fmt: pretty\",\n"
	removed := "\t\t\tName:       \"brand-voice-id\",\n\t\t\tEndpoint:   \"/v2/videos:generate\",\n\t\t\tDefault:    \"fmt: pretty\",\n"
	changed := "\t\t\tName:       \"brand-voice-id\",\n\t\t\tSource:     \"query\",\n\t\t\tEndpoint:   \"/v2/videos:generate\",\n\t\t\tDefault:    \"fmt: pretty\",\n"

	// Same fields, but the Default value's internal spacing differs — a real
	// difference in what the API receives.
	spacedValue := strings.Replace(repadded, `"fmt: pretty"`, `"fmt:   pretty"`, 1)

	reduce := func(in string) string {
		cmd := exec.Command("bash", script, "reduce")
		cmd.Stdin = strings.NewReader(in)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("reduce: %v", err)
		}
		return string(out)
	}

	base := reduce(narrow)
	if base == "" {
		t.Fatal("reduction produced nothing for a normal block — the check would report every release clean")
	}
	// Colons inside values must survive untouched. This is what makes the missing
	// `g` flag load-bearing: "fmt: pretty" has a colon followed by a space, so a
	// /g would collapse it too and two different values could reduce alike.
	if !strings.Contains(base, "/v2/videos:generate") || !strings.Contains(base, `"fmt: pretty"`) {
		t.Errorf("reduction mangled a value containing a colon: %q", base)
	}

	for _, tc := range []struct {
		name     string
		other    string
		wantSame bool
	}{
		{"gofmt re-pad is invisible", repadded, true},
		{"a removed field still shows", removed, false},
		{"a changed value still shows", changed, false},
		// Two genuinely different string values that differ only in spacing
		// *inside* the value. A `g` flag would collapse both to the same text
		// and the check would go blind to the difference; without it they stay
		// distinct. This is the case that makes the missing /g load-bearing.
		{"spacing inside a value is not collapsed", spacedValue, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reduce(tc.other); (got == base) != tc.wantSame {
				verb := "differ from"
				if tc.wantSame {
					verb = "match"
				}
				t.Errorf("reduction should %s the baseline\nbase:  %q\nother: %q", verb, base, got)
			}
		})
	}
}
