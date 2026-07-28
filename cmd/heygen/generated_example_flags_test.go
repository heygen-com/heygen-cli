package main

import (
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
	return strings.HasPrefix(strings.Trim(value, `"'`), "@")
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
						for _, token := range strings.Fields(line) {
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
						tokens := strings.Fields(line)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPoll, gotAtFile bool
			for _, line := range exampleCommandLines(tt.line) {
				tokens := strings.Fields(line)
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
