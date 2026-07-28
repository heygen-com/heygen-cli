package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/gen"
)

// pollOnlyFlags are registered by the builder only when poll_configs.go has an
// entry for the command. An example using one on any other command is shipped
// guidance that fails with "unknown flag" the moment an agent copies it.
var pollOnlyFlags = []string{"--wait", "--timeout"}

var dataFlags = []string{"-d", "--data"}

// exampleCommandLines returns just the runnable lines of an example, dropping
// the leading "# description" comment. Matching against the description text
// would flag legitimate prose like "without --wait".
func exampleCommandLines(example string) []string {
	var out []string
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// shellTokens splits a command line the way a shell would, so the guards see
// the argv the CLI actually receives. Whitespace splitting is not enough:
// `--prompt 'say --wait now'` must stay one argument (otherwise the poll guard
// false-positives on prompt text), while a quoted `"--wait"` must reduce to a
// real flag (otherwise it slips past). Handles single/double quotes and
// backslash escapes; that covers everything the examples corpus can express.
func shellTokens(line string) []string {
	var tokens []string
	var cur strings.Builder
	var quote rune
	started := false

	flush := func() {
		if started {
			tokens = append(tokens, cur.String())
			cur.Reset()
			started = false
		}
	}

	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\\' && quote != '\'' && i+1 < len(runes):
			i++
			cur.WriteRune(runes[i])
			started = true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote = c
			started = true
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteRune(c)
			started = true
		}
	}
	flush()
	return tokens
}

// flagName splits a token into its flag name and inline value, so `--data=x`
// and a bare `--data` both report the name `--data`.
func flagName(token string) (name, inlineValue string, hasInline bool) {
	if before, after, found := strings.Cut(token, "="); found {
		return before, after, true
	}
	return token, "", false
}

// looksLikeAtFile reports whether a -d/--data value uses curl's `@file` form.
// readData has no such form: it takes stdin ("-"), inline JSON, or a bare path,
// so an `@` prefix is read as a literal filename and the command fails.
func looksLikeAtFile(value string) bool {
	return strings.HasPrefix(value, "@")
}

func TestGeneratedExamplesDoNotUsePollFlagsWithoutPollConfig(t *testing.T) {
	checked := 0
	for group, specs := range gen.Groups {
		for _, spec := range specs {
			key := group + "/" + spec.Name
			if pollConfigs[key] != nil {
				continue
			}
			t.Run(key, func(t *testing.T) {
				for _, ex := range spec.Examples {
					for _, line := range exampleCommandLines(ex) {
						for _, token := range shellTokens(line) {
							name, _, _ := flagName(token)
							for _, bad := range pollOnlyFlags {
								if name == bad {
									t.Errorf("%s has no poll config, so %s is not a registered flag, "+
										"but an example uses it — fix codegen/examples/%s.yaml and regenerate:\n  %s",
										key, bad, group, line)
								}
							}
						}
						checked++
					}
				}
			})
		}
	}
	if checked == 0 {
		t.Fatal("no example command lines examined — the guard would pass vacuously")
	}
}

func TestGeneratedExamplesDoNotUseAtFileSyntax(t *testing.T) {
	checked := 0
	for group, specs := range gen.Groups {
		for _, spec := range specs {
			t.Run(group+"/"+spec.Name, func(t *testing.T) {
				for _, ex := range spec.Examples {
					for _, line := range exampleCommandLines(ex) {
						tokens := shellTokens(line)
						for i, token := range tokens {
							name, inline, hasInline := flagName(token)
							isData := false
							for _, d := range dataFlags {
								if name == d {
									isData = true
								}
							}
							if !isData {
								continue
							}
							value := inline
							if !hasInline && i+1 < len(tokens) {
								value = tokens[i+1]
							}
							if looksLikeAtFile(value) {
								t.Errorf("%s %s example uses curl-style @file, which readData does not "+
									"support; drop the '@' in codegen/examples/%s.yaml and regenerate:\n  %s",
									group, spec.Name, group, line)
							}
						}
						checked++
					}
				}
			})
		}
	}
	if checked == 0 {
		t.Fatal("no example command lines examined — the guard would pass vacuously")
	}
}

// Pins the matchers against the forms that motivated them, so a future
// refactor cannot quietly narrow detection back to a substring check.
func TestExampleFlagMatchers(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantPoll   bool
		wantAtFile bool
	}{
		{"bare poll flag", "heygen background-removal create --wait", true, false},
		{"poll flag with value", "heygen video create --timeout=5m", true, false},
		{"longer flag is not a match", "heygen x create --wait-for-caption", false, false},
		{"flag named only in description", "# runs without --wait\n  heygen x create", false, false},
		{"separated at-file", "heygen template generate id -d @vars.json", false, true},
		{"equals at-file", "heygen template generate id --data=@vars.json", false, true},
		{"quoted at-file", "heygen template generate id -d '@vars.json'", false, true},
		{"extra whitespace", "heygen template generate id -d    @vars.json", false, true},
		{"bare path is fine", "heygen template generate id -d vars.json", false, false},
		{"inline JSON is fine", `heygen video create -d '{"type":"avatar"}'`, false, false},
		{"stdin is fine", "heygen video create -d -", false, false},
		// Shell-tokenization cases: quotes must not hide a real flag, and must
		// not turn quoted prose into one.
		{"quoted flag still counts", `heygen background-removal create "--wait"`, true, false},
		{"flag-like text inside a quoted value does not", "heygen x create --prompt 'say --wait now'", false, false},
		{"escaped at-file still counts", `heygen template generate id -d \@vars.json`, false, true},
		{"quoted filename with spaces", `heygen template generate id -d '@my vars.json'`, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPoll, gotAtFile bool
			for _, line := range exampleCommandLines(tt.line) {
				tokens := shellTokens(line)
				for i, token := range tokens {
					name, inline, hasInline := flagName(token)
					for _, bad := range pollOnlyFlags {
						if name == bad {
							gotPoll = true
						}
					}
					for _, d := range dataFlags {
						if name != d {
							continue
						}
						value := inline
						if !hasInline && i+1 < len(tokens) {
							value = tokens[i+1]
						}
						if looksLikeAtFile(value) {
							gotAtFile = true
						}
					}
				}
			}
			if gotPoll != tt.wantPoll {
				t.Errorf("poll-flag match = %v, want %v for %q", gotPoll, tt.wantPoll, tt.line)
			}
			if gotAtFile != tt.wantAtFile {
				t.Errorf("at-file match = %v, want %v for %q", gotAtFile, tt.wantAtFile, tt.line)
			}
		})
	}
}

// The fail-safe rule in SKILL.md's stop conditions is the one that prevents the
// runaway this whole change came from: an install polled a deleted video ~450
// times an hour for three days. Prose is deliberately not pinned word-for-word,
// only the concept, so the section can be rewritten but not silently dropped.
func TestSkillDocumentsUnrecognizedStatusIsTerminal(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	_, after, found := strings.Cut(string(doc), "Stop conditions")
	if !found {
		t.Fatal("SKILL.md has no 'Stop conditions' section — poll guidance was removed or renamed")
	}
	section := strings.ToLower(after)
	if idx := strings.Index(section, "\n## "); idx != -1 {
		section = section[:idx]
	}
	// Collapse whitespace before matching. Without this the pin breaks whenever
	// a phrase happens to straddle a line wrap, which is a legitimate edit and
	// not a dropped rule.
	section = strings.Join(strings.Fields(section), " ")
	// Markers are stems, not phrases, so a rewrite can reword freely: "unrecogniz"
	// covers "unrecognized" and "do not recognize"; "loop" plus "cap" together
	// mean the capping rule, since "cap" alone also matches capacity/captured.
	// "--response-schema" pins the discovery step — the guidance deliberately
	// enumerates no status vocabulary, so without that pointer the section tells
	// an agent nothing about which states to trust.
	for _, concept := range []string{"--response-schema", "recogniz", "terminal", "loop", "cap"} {
		if !strings.Contains(section, concept) {
			t.Errorf("stop-conditions section no longer conveys %q; an unrecognized status must "+
				"terminate polling and loops must be capped", concept)
		}
	}
}
