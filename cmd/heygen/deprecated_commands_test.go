package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/analytics"
	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/heygen-com/heygen-cli/internal/output"
)

// A deprecated command is one the API still serves but no longer wants used.
// Removing it would break every script that calls it, so it keeps working,
// stops being advertised, and says so on stderr where it cannot corrupt the
// JSON an agent reads from stdout.

func deprecatedCommandGroups() map[string][]*command.Spec {
	return map[string][]*command.Spec{
		// Endpoint matches a deprecatedCommandReplacements key so the notice
		// carries a replacement.
		"voice": {{
			Group: "voice", Name: "list", Summary: "List voices",
			Endpoint: "/v3/voices", Method: "GET", Deprecated: true,
			Examples: []string{"heygen voice list"},
		}},
		"widget": {{
			Group: "widget", Name: "list", Summary: "List widgets",
			Endpoint: "/v3/widgets", Method: "GET",
			Examples: []string{"heygen widget list"},
		}},
	}
}

func TestDeprecatedCommandRunsAndWarnsOnStderr(t *testing.T) {
	reached := false
	srv := setupTestServer(t, map[string]testHandler{
		"GET /v3/voices": {
			StatusCode:      200,
			Body:            `{"data":[]}`,
			ValidateRequest: func(*testing.T, *http.Request) { reached = true },
		},
		"GET /v3/widgets": {StatusCode: 200, Body: `{"data":[]}`},
	})
	defer srv.Close()
	groups := deprecatedCommandGroups()

	res := runGeneratedRootCommand(t, srv.URL, "test-key", groups, "voice", "list")
	if res.ExitCode != 0 || !reached {
		t.Fatalf("exit = %d, request reached API = %v; a deprecated command must still run. stderr=%s", res.ExitCode, reached, res.Stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		t.Fatalf("stdout is not parseable JSON, the deprecation notice leaked into it: %q", res.Stdout)
	}
	want := `"heygen voice list" is deprecated and will be removed in a future version; use "heygen model audio voices list" instead`
	var envelope struct {
		Warning struct{ Message string } `json:"warning"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stderr)), &envelope); err != nil || envelope.Warning.Message != want {
		t.Errorf("stderr = %q, want one warning envelope with message %q", res.Stderr, want)
	}

	control := runGeneratedRootCommand(t, srv.URL, "test-key", groups, "widget", "list")
	if control.Stderr != "" {
		t.Errorf("a command that is not deprecated must not warn, stderr = %q", control.Stderr)
	}
}

// The group hides because every command in it is deprecated; the leaf's own
// --help still works and leads with the notice.
func TestDeprecatedCommandHiddenFromHelp(t *testing.T) {
	groups := deprecatedCommandGroups()

	root := runGeneratedRootCommand(t, "http://unused", "test-key", groups, "--help")
	if strings.Contains(root.Stdout, "voice") || !strings.Contains(root.Stdout, "widget") {
		t.Errorf("root help should list widget and omit the all-deprecated voice group:\n%s", root.Stdout)
	}

	leaf := runGeneratedRootCommand(t, "http://unused", "test-key", groups, "voice", "list", "--help")
	if !strings.Contains(leaf.Stdout, `Deprecated: "heygen voice list" is deprecated`) {
		t.Errorf("deprecated command's own help should lead with the notice:\n%s", leaf.Stdout)
	}
}

// A replacement that does not resolve sends users to a command that does not
// exist.
func TestDeprecatedCommandReplacementsExist(t *testing.T) {
	root := newRootCmd("test", output.NewJSONFormatter(&bytes.Buffer{}, &bytes.Buffer{}), analytics.New("test", false))
	for key, replacement := range deprecatedCommandReplacements {
		cmd, _, err := root.Find(strings.Fields(replacement))
		if err != nil || cmd.CommandPath() != "heygen "+replacement || !cmd.Runnable() {
			t.Errorf("%s: replacement %q does not resolve to a runnable command", key, replacement)
		}
	}
}
