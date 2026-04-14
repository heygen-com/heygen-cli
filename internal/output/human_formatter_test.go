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
	if !strings.Contains(got, "Meta:\n") || !strings.Contains(got, "Kind:") || !strings.Contains(got, "demo") {
		t.Fatalf("nested objects should render as indented sub-section:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValuePreservesMultilineStrings(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage("{\"data\":{\"name\":\"Line 1\\nLine 2\"}}")
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Line 1\n") || !strings.Contains(got, "Line 2") {
		t.Fatalf("key-value output should preserve multiline strings:\n%s", got)
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

func TestFormatTableCell_SanitizesWhitespace(t *testing.T) {
	got := formatTableCell("\nMark\u00a0", "name")
	if got != "Mark" {
		t.Fatalf("formatTableCell = %q, want %q", got, "Mark")
	}
}

func TestHumanFormatter_Data_KeyValue_StringArray(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"events":["avatar_video.success","avatar_video.fail"],"id":"ep_1"}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "avatar_video.success, avatar_video.fail") {
		t.Fatalf("string arrays should render comma-separated:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_NestedObject(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"error":{"code":"not_found","message":"Video not found"},"id":"vid_1"}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Error:\n") {
		t.Fatalf("nested object should start a sub-section:\n%s", got)
	}
	if !strings.Contains(got, "  Code:") || !strings.Contains(got, "not_found") {
		t.Fatalf("nested object fields should be indented:\n%s", got)
	}
	if !strings.Contains(got, "  Message:") || !strings.Contains(got, "Video not found") {
		t.Fatalf("nested object fields should be indented:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_DeepNesting(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"wallet":{"currency":"usd","remaining_balance":150,"auto_reload":{"enabled":true,"amount_usd":50}}}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Wallet:\n") {
		t.Fatalf("top-level nested object should start a sub-section:\n%s", got)
	}
	if !strings.Contains(got, "  Auto Reload:\n") {
		t.Fatalf("second-level nested object should start a sub-section:\n%s", got)
	}
	if !strings.Contains(got, "    Enabled:") || !strings.Contains(got, "true") {
		t.Fatalf("third-level fields should be double-indented:\n%s", got)
	}
	if !strings.Contains(got, "  Currency:") || !strings.Contains(got, "usd") {
		t.Fatalf("second-level scalar fields should be indented:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_ArrayOfObjects(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"messages":[{"role":"user","content":"hello"},{"role":"model","content":"hi"}]}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Messages:\n") {
		t.Fatalf("array of objects should start a sub-section:\n%s", got)
	}
	if !strings.Contains(got, "[1]") || !strings.Contains(got, "[2]") {
		t.Fatalf("array entries should be numbered:\n%s", got)
	}
	if !strings.Contains(got, "Role:") || !strings.Contains(got, "user") {
		t.Fatalf("object fields should be rendered:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_EmptyCollections(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"metadata":{},"tags":[]}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	count := strings.Count(got, "(none)")
	if count != 2 {
		t.Fatalf("empty map and array should both render as (none), got %d occurrences:\n%s", count, got)
	}
}

func TestHumanFormatter_Data_Table_StringArray(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":[{"id":"ep_1","events":["a.success","a.fail"]},{"id":"ep_2","events":["b.success"]}]}`)
	columns := []command.Column{
		{Header: "ID", Field: "id"},
		{Header: "Events", Field: "events"},
	}

	if err := f.Data(input, "data", columns); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "a.success, a.fail") {
		t.Fatalf("string arrays in tables should be comma-separated:\n%s", got)
	}
}

func TestHumanFormatter_Data_Table_FlatMap(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":[{"id":"av_1","error":{"code":"bad_input","message":"invalid"}}]}`)
	columns := []command.Column{
		{Header: "ID", Field: "id"},
		{Header: "Error", Field: "error"},
	}

	if err := f.Data(input, "data", columns); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "code=bad_input") || !strings.Contains(got, "message=invalid") {
		t.Fatalf("flat maps in tables should render as key=value:\n%s", got)
	}
}

func TestHumanFormatter_Data_Table_NullInArray(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":[{"id":"x","items":[null,"hello"]}]}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	// Arrays with null should fall back to compact JSON, not produce ", hello"
	if strings.Contains(got, ", hello") {
		t.Fatalf("arrays with null should not use inline format:\n%s", got)
	}
	if !strings.Contains(got, "null") {
		t.Fatalf("null should be preserved in compact JSON:\n%s", got)
	}
}

func TestHumanFormatter_Data_Table_NullInMap(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":[{"id":"x","info":{"a":null,"b":"val"}}]}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	// Maps with null values should not use inline format
	if strings.Contains(got, "a=") {
		t.Fatalf("maps with null values should not use inline format:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_NestedScalarArray(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"config":{"tags":["pro","team"],"name":"test"}}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "pro, team") {
		t.Fatalf("scalar arrays inside nested objects should render comma-separated:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_NestedMultilineString(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage("{\"data\":{\"msg\":{\"content\":\"Line 1\\nLine 2\\nLine 3\"}}}")
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "Line 1\n") || !strings.Contains(got, "Line 2\n") || !strings.Contains(got, "Line 3") {
		t.Fatalf("multiline strings in nested objects should preserve all lines:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_NestedTimestampAndDuration(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"credits":{"resets_at":1710000000},"stats":{"duration":125}}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "2024-03-09") {
		t.Fatalf("timestamps inside nested objects should be formatted:\n%s", got)
	}
	if !strings.Contains(got, "2m 5s") {
		t.Fatalf("durations inside nested objects should be formatted:\n%s", got)
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
