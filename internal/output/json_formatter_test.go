package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestJSONFormatter_Data(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	f := NewJSONFormatter(&out, &errOut)

	input := json.RawMessage(`{"data":[{"id":"v1","status":"completed"}],"has_more":false}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output is valid pretty-printed JSON
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v\noutput: %s", err, out.String())
	}

	// Verify nothing went to stderr
	if errOut.Len() > 0 {
		t.Errorf("unexpected stderr output: %s", errOut.String())
	}
}

func TestJSONFormatter_Error(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	f := NewJSONFormatter(&out, &errOut)

	cliErr := &clierrors.CLIError{
		Code:      "not_found",
		Message:   "Video abc123 not found",
		Hint:      "Check the video ID with: heygen video list",
		RequestID: "req_xyz",
		ExitCode:  clierrors.ExitGeneral,
	}
	f.Error(cliErr)

	// Verify nothing went to stdout
	if out.Len() > 0 {
		t.Errorf("unexpected stdout output: %s", out.String())
	}

	// Verify stderr has JSON error envelope
	var envelope map[string]map[string]string
	if err := json.Unmarshal(errOut.Bytes(), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, errOut.String())
	}

	inner := envelope["error"]
	if inner["code"] != "not_found" {
		t.Errorf("code = %q, want %q", inner["code"], "not_found")
	}
	if inner["message"] != "Video abc123 not found" {
		t.Errorf("message = %q, want expected value", inner["message"])
	}
	if inner["hint"] != "Check the video ID with: heygen video list" {
		t.Errorf("hint = %q, want expected value", inner["hint"])
	}
	if inner["request_id"] != "req_xyz" {
		t.Errorf("request_id = %q, want %q", inner["request_id"], "req_xyz")
	}
}

func TestJSONFormatter_Data_InvalidJSON(t *testing.T) {
	var out bytes.Buffer
	f := NewJSONFormatter(&out, &bytes.Buffer{})

	err := f.Data(json.RawMessage(`not json`), "data", nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if out.Len() > 0 {
		t.Errorf("invalid JSON should not produce stdout output, got: %s", out.String())
	}
}

func TestJSONFormatter_Error_OmitsEmptyFields(t *testing.T) {
	var errOut bytes.Buffer
	f := NewJSONFormatter(&bytes.Buffer{}, &errOut)

	cliErr := &clierrors.CLIError{
		Code:     "error",
		Message:  "something failed",
		ExitCode: clierrors.ExitGeneral,
	}
	f.Error(cliErr)

	var envelope map[string]map[string]any
	if err := json.Unmarshal(errOut.Bytes(), &envelope); err != nil {
		t.Fatalf("stderr is not valid JSON: %v", err)
	}

	inner := envelope["error"]
	if _, ok := inner["hint"]; ok {
		t.Error("hint should be omitted when empty")
	}
	if _, ok := inner["request_id"]; ok {
		t.Error("request_id should be omitted when empty")
	}
}

func TestJSONFormatter_Data_IgnoresDataFieldAndColumns(t *testing.T) {
	var out bytes.Buffer
	f := NewJSONFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"id":"v1"},"meta":{"count":1}}`)
	if err := f.Data(input, "data", []command.Column{{Header: "ID", Field: "id"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if _, ok := parsed["meta"]; !ok {
		t.Fatalf("expected full response envelope, got: %v", parsed)
	}
}

// Warn shares the error envelope's shape and destination so a machine consumer
// can apply one rule to stderr, and stdout stays a clean response stream.
func TestJSONFormatter_Warn(t *testing.T) {
	var out, errOut bytes.Buffer
	f := NewJSONFormatter(&out, &errOut)

	f.Warn("--brand-voice-id is deprecated")

	if out.Len() != 0 {
		t.Errorf("warning must not touch stdout, got %q", out.String())
	}
	var envelope map[string]map[string]string
	if err := json.Unmarshal(errOut.Bytes(), &envelope); err != nil {
		t.Fatalf("warning on stderr is not valid JSON: %v (%q)", err, errOut.String())
	}
	if got := envelope["warning"]["message"]; got != "--brand-voice-id is deprecated" {
		t.Errorf(`warning.message = %q, want the supplied message`, got)
	}
}

// Pins the envelope a consumer parses, and the rule it shares with warnings:
// stdout stays untouched, since anything landing there corrupts the response.
func TestJSONFormatter_Notice(t *testing.T) {
	var out, errOut bytes.Buffer
	f := NewJSONFormatter(&out, &errOut)

	f.Notice("cli_update_available", "heygen v0.9.0 is available; you have v0.8.1")

	if out.Len() != 0 {
		t.Errorf("notice must not touch stdout, got %q", out.String())
	}
	var envelope map[string]map[string]string
	if err := json.Unmarshal(errOut.Bytes(), &envelope); err != nil {
		t.Fatalf("notice on stderr is not valid JSON: %v (%q)", err, errOut.String())
	}
	if got := envelope["notice"]["code"]; got != "cli_update_available" {
		t.Errorf("notice.code = %q, want the supplied code", got)
	}
	if got := envelope["notice"]["message"]; got != "heygen v0.9.0 is available; you have v0.8.1" {
		t.Errorf("notice.message = %q, want the supplied message", got)
	}
}

// Pins the documented stderr framing, one compact envelope per line: a single
// run can emit several, such as a first-run notice followed by an error.
func TestJSONFormatter_DiagnosticsAreOnePerLine(t *testing.T) {
	var out, errOut bytes.Buffer
	f := NewJSONFormatter(&out, &errOut)

	f.Notice("cli_telemetry_notice", "first run disclosure")
	f.Warn("--brand-voice-id is deprecated")
	f.Error(&clierrors.CLIError{Code: "usage_error", Message: "accepts 1 arg(s), received 0"})

	if out.Len() != 0 {
		t.Errorf("diagnostics must not touch stdout, got %q", out.String())
	}

	lines := strings.Split(strings.TrimSuffix(errOut.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want one per diagnostic:\n%s", len(lines), errOut.String())
	}
	wantKeys := []string{"notice", "warning", "error"}
	for i, line := range lines {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Errorf("line %d is not independently parseable: %v (%q)", i, err, line)
			continue
		}
		if _, ok := envelope[wantKeys[i]]; !ok {
			t.Errorf("line %d has keys %v, want %q", i, keysOf(envelope), wantKeys[i])
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
