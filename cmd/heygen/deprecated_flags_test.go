package main

import (
	"encoding/json"
	"net/http"

	"github.com/heygen-com/heygen-cli/internal/command"
	"strings"
	"testing"
)

// A deprecated flag is a flag the API still accepts but no longer wants used.
// The CLI's job is to stop advertising it without breaking the scripts that
// already pass it, and to say so where it cannot corrupt the response.
//
// `template generate --brand-voice-id` is the live case: the spec has marked
// brand_voice_id deprecated for months in favour of brand_glossary_id.

const templateGenerateOK = `{"error":null,"data":{"id":"vid_1","status":"pending"}}`

func templateGenerateHandlers() map[string]testHandler {
	return map[string]testHandler{
		"POST /v3/templates/tpl_1": {StatusCode: 200, Body: templateGenerateOK},
	}
}

// The load-bearing one: a warning must never land on stdout. An agent pipes
// stdout into a JSON parser, so a stray notice there is a parse failure rather
// than a hint. Asserting "stderr mentions the flag" alone would still pass if
// the notice were ALSO written to stdout, so this decodes stdout as JSON.
func TestDeprecatedFlagWarnsOnStderrAndKeepsStdoutParseable(t *testing.T) {
	srv := setupTestServer(t, templateGenerateHandlers())
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"template", "generate", "tpl_1",
		"-d", `{"variables":{}}`,
		"--brand-voice-id", "bv_1",
	)

	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (deprecated must not mean rejected); stderr=%s", res.ExitCode, res.Stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		t.Fatalf("stdout is not parseable JSON, the deprecation notice leaked into it: %q", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "brand-voice-id") {
		t.Errorf("stderr should carry the deprecation notice, got %q", res.Stderr)
	}
}

// Pins that the flag still reaches the API. Hiding a flag is a help-text
// decision; silently dropping the value would break every caller that passes it.
func TestDeprecatedFlagStillSendsItsValue(t *testing.T) {
	var gotBody map[string]any
	handlers := templateGenerateHandlers()
	h := handlers["POST /v3/templates/tpl_1"]
	h.ValidateRequest = func(t *testing.T, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
	}
	handlers["POST /v3/templates/tpl_1"] = h

	srv := setupTestServer(t, handlers)
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"template", "generate", "tpl_1",
		"-d", `{"variables":{}}`,
		"--brand-voice-id", "bv_1",
	)
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", res.ExitCode, res.Stderr)
	}
	if got := gotBody["brand_voice_id"]; got != "bv_1" {
		t.Errorf("brand_voice_id = %v, want %q — a deprecated flag must still be sent", got, "bv_1")
	}
}

// The control. Without it, a warning emitted unconditionally for every
// deprecated flag on the command — rather than only the ones the user set —
// would still pass the positive case above.
func TestDeprecatedFlagSilentWhenNotPassed(t *testing.T) {
	srv := setupTestServer(t, templateGenerateHandlers())
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"template", "generate", "tpl_1",
		"-d", `{"variables":{}}`,
	)
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", res.ExitCode, res.Stderr)
	}
	if strings.Contains(res.Stderr, "deprecated") {
		t.Errorf("no deprecated flag was passed, so nothing should warn; stderr=%q", res.Stderr)
	}
}

// Hidden from help is the whole point: the CLI should stop advertising a field
// the API has moved on from, while `--help` still lists its replacement.
func TestDeprecatedFlagHiddenFromHelpButReplacementShown(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "template", "generate", "--help")
	if strings.Contains(res.Stdout, "--brand-voice-id") {
		t.Error("deprecated flag should be hidden from --help")
	}
	if !strings.Contains(res.Stdout, "--brand-glossary-id") {
		t.Error("the current flag should still be listed in --help")
	}
}

// An agent that composes a body with -d/--data never touches the flag, so it
// never sees the flag's notice. --request-schema is the surface it reads
// instead, so the marker has to survive into the emitted schema too.
func TestDeprecatedFieldSurfacesInRequestSchema(t *testing.T) {
	srv := setupTestServer(t, map[string]testHandler{})
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key", "template", "generate", "--request-schema")
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", res.ExitCode, res.Stderr)
	}

	var schema struct {
		Properties map[string]struct {
			Deprecated bool `json:"deprecated"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &schema); err != nil {
		t.Fatalf("--request-schema output is not valid JSON: %v", err)
	}
	if !schema.Properties["brand_voice_id"].Deprecated {
		t.Error("brand_voice_id should be marked deprecated in --request-schema")
	}
	if schema.Properties["brand_glossary_id"].Deprecated {
		t.Error("brand_glossary_id is current and must not be marked deprecated")
	}
}

// The population most in need of the notice is the one that never sees the
// flag: an agent composing a raw body reads --request-schema, not --help, so
// hiding the flag tells it nothing. Keying only on Changed() would miss it.
func TestDeprecatedFieldInRawBodyWarns(t *testing.T) {
	srv := setupTestServer(t, templateGenerateHandlers())
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"template", "generate", "tpl_1",
		"-d", `{"variables":{},"brand_voice_id":"bv_1"}`,
	)

	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stderr, "brand-voice-id") {
		t.Errorf("a deprecated field sent via -d should still warn, got stderr=%q", res.Stderr)
	}
	if err := json.Unmarshal([]byte(res.Stdout), &map[string]any{}); err != nil {
		t.Fatalf("stdout must stay parseable, got %q", res.Stdout)
	}
}

// Control for the above: an undeprecated field in the same body must not warn,
// so the -d path can't fire on every key it sees.
func TestUndeprecatedFieldInRawBodySilent(t *testing.T) {
	srv := setupTestServer(t, templateGenerateHandlers())
	defer srv.Close()

	res := runCommand(t, srv.URL, "test-key",
		"template", "generate", "tpl_1",
		"-d", `{"variables":{},"brand_glossary_id":"bg_1"}`,
	)
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%s", res.ExitCode, res.Stderr)
	}
	if strings.Contains(res.Stderr, "deprecated") {
		t.Errorf("current field must not warn; stderr=%q", res.Stderr)
	}
}

// A flag that is both required and deprecated is a contradiction the spec
// could express: hiding it leaves the user told a required flag is missing with
// no way to discover it. Staying visible is the lesser evil.
func TestDeprecatedButRequiredFlagStaysVisible(t *testing.T) {
	spec := &command.Spec{
		Group: "test", Name: "cmd", Summary: "s",
		Flags: []command.FlagSpec{
			{Name: "legacy", Type: "string", Required: true, Deprecated: true, Source: "body", JSONName: "legacy"},
		},
	}
	cmd := buildCobraCommand(spec, testCmdContext())
	f := cmd.Flags().Lookup("legacy")
	if f == nil {
		t.Fatal("flag should be registered")
	}
	if f.Hidden {
		t.Error("a required flag must stay visible even when deprecated, or its 'required flag not set' error names something undiscoverable")
	}
}
