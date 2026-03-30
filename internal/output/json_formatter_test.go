package output

import (
	"bytes"
	"encoding/json"
	"testing"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

func TestJSONFormatter_Data(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	f := NewJSONFormatter(&out, &errOut)

	input := json.RawMessage(`{"data":[{"id":"v1","status":"completed"}],"has_more":false}`)
	if err := f.Data(input); err != nil {
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

	err := f.Data(json.RawMessage(`not json`))
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
