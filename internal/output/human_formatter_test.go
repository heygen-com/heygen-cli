package output

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func TestHumanFormatter_Data_TableWithCuratedColumns(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":[{"id":"vid_123","title":"Demo","status":"completed","created_at":1710000000}]}`)
	columns := []command.Column{
		{Header: "ID", Field: "id"},
		{Header: "Title", Field: "title"},
	}

	if err := f.Data(input, "data", columns); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "ID") || !strings.Contains(got, "Title") {
		t.Fatalf("missing curated headers in output:\n%s", got)
	}
	if !strings.Contains(got, "vid_123") || !strings.Contains(got, "Demo") {
		t.Fatalf("missing row values in output:\n%s", got)
	}
	if !strings.Contains(got, "Showing 2 of 4 columns") {
		t.Fatalf("missing truncation hint in output:\n%s", got)
	}
}

func TestHumanFormatter_Data_AutoColumns(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":[{"voice_id":"v1","name":"Ava","gender":"female"}]}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Voice Id") || !strings.Contains(got, "Name") || !strings.Contains(got, "Gender") {
		t.Fatalf("missing auto-generated headers in output:\n%s", got)
	}
	if strings.Contains(got, "Showing ") {
		t.Fatalf("auto-column tables should not show truncation hints:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"id":"vid_123","status":"completed","meta":{"kind":"demo"}}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Id:") || !strings.Contains(got, "vid_123") {
		t.Fatalf("missing id field in output:\n%s", got)
	}
	if !strings.Contains(got, `{"kind":"demo"}`) {
		t.Fatalf("nested objects should render as inline JSON:\n%s", got)
	}
}

func TestHumanFormatter_Data_TimestampFormatting(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":[{"id":"vid_123","created_at":1774712936}]}`)
	columns := []command.Column{
		{Header: "ID", Field: "id"},
		{Header: "Created", Field: "created_at"},
	}

	if err := f.Data(input, "data", columns); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "2026-03-28 15:48") {
		t.Fatalf("timestamp was not formatted as UTC date/time:\n%s", got)
	}
}

func TestHumanFormatter_Data_DurationFormatting(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"duration":18.9649,"status":"completed"}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Duration:  18s") {
		t.Fatalf("duration was not formatted as seconds:\n%s", got)
	}
}

func TestHumanFormatter_Data_RegularFloatUnaffected(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"score":3.14}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Score:  3.14") {
		t.Fatalf("regular float should remain unchanged:\n%s", got)
	}
}

func TestHumanFormatter_Data_PrimitiveArray(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":["en","es"]}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Value") || !strings.Contains(got, "en") || !strings.Contains(got, "es") {
		t.Fatalf("primitive arrays should render as a single-column table:\n%s", got)
	}
}

func TestHumanFormatter_Error(t *testing.T) {
	var errOut bytes.Buffer
	f := NewHumanFormatter(&bytes.Buffer{}, &errOut)

	f.Error(clierrors.NewAuth("HEYGEN_API_KEY is not set", "Set it with: export HEYGEN_API_KEY=<your-key>"))

	got := stripANSI(errOut.String())
	if !strings.Contains(got, "Error: HEYGEN_API_KEY is not set") {
		t.Fatalf("missing error line:\n%s", got)
	}
	if !strings.Contains(got, "Hint: Set it with: export HEYGEN_API_KEY=<your-key>") {
		t.Fatalf("missing hint line:\n%s", got)
	}
}
