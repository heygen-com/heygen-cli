// Command checkdescriptions is an ADVISORY scanner for HTTP-framed text in the
// generated CLI command surface that lacks a CLI-surface description override.
//
// The CLI generates its commands from HeyGen's OpenAPI spec. Some spec
// descriptions are written in raw HTTP terms ("poll via GET /v3/videos/{id}",
// "Pass to POST /v3/video-agents") that mislead a CLI user. internal/clidesc
// reframes those few cases. On each codegen resync, new HTTP-framed text can
// arrive that nobody has reframed yet. This tool finds those: it scans every
// generated command's Summary, Description, flag help, and schema-field
// descriptions for divergence markers, and reports any flagged text whose
// specific location (summary, description, flag, or field) has no matching
// override — so a partial override never hides new HTTP-framed text elsewhere
// on the same command.
//
// It is ADVISORY: it prints a report and ALWAYS exits 0, so it never fails CI.
// Run it via `make check-descriptions` after a resync; author overrides in
// internal/clidesc for anything it flags that genuinely reads badly on the CLI.
//
// Markers (validated against the real corpus, see codegen draft analysis):
//   - uppercase HTTP verbs: \b(GET|POST|PUT|PATCH|DELETE)\b
//   - version paths: /v\d+/...
//   - poll words: \bpoll(ed|ing|s)?\b
//
// Deliberately NOT markers: cursor / next_token / presigned. The draft corpus
// analysis showed those are overwhelmingly accurate, CLI-neutral text (false
// positives), so flagging them would only add noise.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/heygen-com/heygen-cli/gen"
	"github.com/heygen-com/heygen-cli/internal/clidesc"
	"github.com/heygen-com/heygen-cli/internal/command"
)

// markers are the divergence patterns. Each returns the matched substrings.
var markers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"http-verb", regexp.MustCompile(`\b(GET|POST|PUT|PATCH|DELETE)\b`)},
	{"version-path", regexp.MustCompile(`/v\d+/\S*`)},
	{"poll", regexp.MustCompile(`(?i)\bpoll(ed|ing|s)?\b`)},
}

// finding is one flagged piece of generated text on one command.
type finding struct {
	command    string // human command path, e.g. "video-agent create"
	endpoint   string
	method     string
	location   string // "summary", "description", "flag --x", "request/response field y"
	markers    []string
	overridden bool // THIS specific location is covered by an override entry
}

func main() {
	var findings []finding

	for _, groupName := range sortedKeys(gen.Groups) {
		for _, spec := range gen.Groups[groupName] {
			// o is the zero Override when none exists; nil-map lookups below are
			// safe and return false.
			o, _ := clidesc.ForSpec(spec)
			cmdPath := strings.TrimSpace(groupName + " " + spec.Name)

			// overridden means THIS specific location (not merely the command)
			// is covered by an override entry, so a remaining marker there is
			// intentional (e.g. an override that legitimately mentions a manual
			// S3 "PUT") rather than an unaddressed gap. Suppressing per-location
			// means a partial override no longer hides new HTTP-framed text in a
			// command's uncovered summary/description/flags/fields.
			add := func(location, text string, overridden bool) {
				if hits := scan(text); len(hits) > 0 {
					findings = append(findings, finding{
						command:    cmdPath,
						endpoint:   spec.Endpoint,
						method:     spec.Method,
						location:   location,
						markers:    hits,
						overridden: overridden,
					})
				}
			}

			// Inspect the text the CLI shows AFTER the overlay is applied, so a
			// reframed location is not re-flagged for text the overlay already
			// fixed. This is the true "does the live CLI surface still read as
			// HTTP?" signal.
			add("summary", clidesc.Summary(spec), o.Summary != "")
			add("description", clidesc.Description(spec), o.Description != "")
			for _, f := range spec.Flags {
				_, ok := o.Flags[f.Name]
				add("flag --"+f.Name, clidesc.FlagHelp(spec, f), ok)
			}
			for loc, text := range schemaFieldDescriptions(spec) {
				field := loc[strings.LastIndex(loc, " ")+1:]
				_, ok := o.Fields[field]
				add(loc, text, ok)
			}
		}
	}

	report(findings)
}

// scan returns the distinct marker names that match text.
func scan(text string) []string {
	if text == "" {
		return nil
	}
	var hits []string
	for _, m := range markers {
		if m.re.MatchString(text) {
			hits = append(hits, m.name)
		}
	}
	return hits
}

// schemaFieldDescriptions returns "field <name>" → description for every
// property in the command's (overlay-applied) request and response schemas.
func schemaFieldDescriptions(spec *command.Spec) map[string]string {
	out := map[string]string{}
	collect := func(kind, raw string) {
		if raw == "" {
			return
		}
		// Apply the field overlay first so reframed fields aren't re-flagged.
		var doc any
		if err := json.Unmarshal([]byte(clidesc.Schema(spec, raw)), &doc); err != nil {
			return
		}
		walk(doc, func(name, desc string) {
			if desc != "" {
				out[kind+" field "+name] = desc
			}
		})
	}
	collect("request", spec.RequestSchema)
	collect("response", spec.ResponseSchema)
	return out
}

// walk visits every property (name, description) in a decoded JSON schema.
func walk(node any, fn func(name, desc string)) {
	switch v := node.(type) {
	case map[string]any:
		if props, ok := v["properties"].(map[string]any); ok {
			for name, prop := range props {
				if pm, ok := prop.(map[string]any); ok {
					desc, _ := pm["description"].(string)
					fn(name, desc)
				}
			}
		}
		for _, child := range v {
			walk(child, fn)
		}
	case []any:
		for _, child := range v {
			walk(child, fn)
		}
	}
}

// report prints the advisory report and always exits 0.
func report(findings []finding) {
	var flagged []finding
	for _, f := range findings {
		if !f.overridden {
			flagged = append(flagged, f)
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].command != findings[j].command {
			return findings[i].command < findings[j].command
		}
		return findings[i].location < findings[j].location
	})
	sort.Slice(flagged, func(i, j int) bool {
		if flagged[i].command != flagged[j].command {
			return flagged[i].command < flagged[j].command
		}
		return flagged[i].location < flagged[j].location
	})

	out := os.Stdout
	fmt.Fprintln(out, "Advisory: HTTP-framed descriptions in the generated CLI surface")
	fmt.Fprintln(out, "================================================================")
	fmt.Fprintf(out, "Scanned generated commands for markers: HTTP verbs, /vN/ paths, poll words.\n")
	fmt.Fprintf(out, "Total marker hits: %d  |  at locations WITHOUT an override: %d\n\n", len(findings), len(flagged))

	if len(flagged) == 0 {
		fmt.Fprintln(out, "No un-overridden HTTP-framed text found. Nothing to author.")
	} else {
		fmt.Fprintln(out, "The following command locations have HTTP-framed text and NO override.")
		fmt.Fprintln(out, "Review each; if it reads badly for a CLI user, add an entry to")
		fmt.Fprintln(out, "internal/clidesc (keyed by endpoint+method):")
		fmt.Fprintln(out)
		for _, f := range flagged {
			fmt.Fprintf(out, "  heygen %s\n", f.command)
			fmt.Fprintf(out, "    %s %s\n", f.method, f.endpoint)
			fmt.Fprintf(out, "    %s [markers: %s]\n", f.location, strings.Join(f.markers, ", "))
		}
	}

	// Informational: marker hits at locations already covered by an override
	// (the override text itself mentions HTTP, e.g. a legitimate manual S3
	// "PUT"), so a reviewer can confirm the overlay covers the right locations.
	overlaid := len(findings) - len(flagged)
	if overlaid > 0 {
		fmt.Fprintf(out, "\n%d marker hit(s) are at locations already covered by an override (informational; not action items).\n", overlaid)
	}

	fmt.Fprintln(out, "\nAdvisory only — exiting 0.")
}

func sortedKeys(m map[string][]*command.Spec) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
