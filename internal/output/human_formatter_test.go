package output

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	if !strings.Contains(got, "Voice ID") || !strings.Contains(got, "Name") || !strings.Contains(got, "Gender") {
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
	// Nested objects render as an indented "Label:" header block; top-level
	// scalars align locally, the nested scalar aligns within its own block.
	want := "ID:      vid_123\n" +
		"Meta:\n" +
		"  Kind:  demo\n" +
		"Status:  completed\n"
	if got != want {
		t.Fatalf("indented key-value output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
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
	// Nested object renders as a humanized "Error:" header with its children
	// indented and aligned locally; the top-level Id is a sibling scalar.
	want := "Error:\n" +
		"  Code:     not_found\n" +
		"  Message:  Video not found\n" +
		"ID:  vid_1\n"
	if got != want {
		t.Fatalf("nested object should render as an indented block:\ngot:\n%s\nwant:\n%s", got, want)
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
	// Each nesting level indents 2 spaces and aligns its scalar siblings
	// locally: Currency/Remaining Balance align under Wallet; Amount Usd/Enabled
	// align within Auto Reload.
	want := "Wallet:\n" +
		"  Auto Reload:\n" +
		"    Amount Usd:  50\n" +
		"    Enabled:     true\n" +
		"  Currency:           usd\n" +
		"  Remaining Balance:  150\n"
	if got != want {
		t.Fatalf("deeply nested objects should render as indented blocks:\ngot:\n%s\nwant:\n%s", got, want)
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
	// Array of objects renders as a YAML sequence: each element's first field
	// carries the "- " marker, remaining fields align under it.
	want := "Messages:\n" +
		"  - Content:  hello\n" +
		"    Role:     user\n" +
		"  - Content:  hi\n" +
		"    Role:     model\n"
	if got != want {
		t.Fatalf("array of objects should render as a YAML sequence:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestHumanFormatter_Data_KeyValue_ArrayOfObjects_EmptyElement(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"messages":[{},{"role":"model"}]}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	// An empty object element must render as a "- (none)" sequence item, not be
	// dropped (which would leave a confusing gap between siblings).
	want := "Messages:\n" +
		"  - (none)\n" +
		"  - Role:  model\n"
	if got != want {
		t.Fatalf("empty object element must render as a (none) sequence item:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestHumanFormatter_Data_KeyValue_MixedScalarArrayUsesCompactJSON(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"items":["hello",42,true]}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	// A mixed-type scalar array falls back to compact JSON rather than an
	// ambiguous inline join, so string vs number vs bool is not lost.
	if !strings.Contains(got, `["hello",42,true]`) {
		t.Fatalf("mixed-type scalar array should render as compact JSON:\n%s", got)
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

func TestHumanFormatter_Data_KeyValue_HeterogeneousArray(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"items":[null,"hello",42]}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	// Heterogeneous arrays with null should fall back to compact JSON
	if !strings.Contains(got, "null") {
		t.Fatalf("heterogeneous arrays should preserve null in compact JSON:\n%s", got)
	}
	if strings.Contains(got, "[1]") {
		t.Fatalf("heterogeneous arrays should not be expanded as numbered entries:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_DepthGuardUsesCompactJSON(t *testing.T) {
	// Build a deeply nested object that exceeds maxNestedDepth (5).
	// depth0 -> depth1 -> depth2 -> depth3 -> depth4 -> depth5 -> {leaf: "val", empty: []}
	inner := map[string]any{"leaf": "val", "empty": []any{}}
	obj := inner
	for i := 0; i < 5; i++ {
		obj = map[string]any{fmt.Sprintf("depth%d", 5-i): obj}
	}

	data, _ := json.Marshal(map[string]any{"data": obj})
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	if err := f.Data(json.RawMessage(data), "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	// At depth 5, individual fields are rendered with compactJSON.
	// String values get JSON-quoted, empty arrays show as [].
	if !strings.Contains(got, `"val"`) {
		t.Fatalf("depth guard should render strings as compact JSON:\n%s", got)
	}
	if !strings.Contains(got, `[]`) {
		t.Fatalf("depth guard should preserve empty arrays as [], not blank:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_NullValuesInNestedObject(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"a":null,"b":"val","nested":{"x":null,"y":1}}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	// Null leaves render as (none), consistent with empty objects/arrays and
	// unambiguous versus an empty string; the nested object renders as an
	// indented block with its own local alignment.
	want := "A:  (none)\n" +
		"B:  val\n" +
		"Nested:\n" +
		"  X:  (none)\n" +
		"  Y:  1\n"
	if got != want {
		t.Fatalf("null values should render as (none):\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestHumanFormatter_Data_KeyValue_ArrayOfObjects_NestedFirstField(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	// A sequence item whose first sorted key (Config) is itself a nested object:
	// Config's child (Timeout) must indent UNDER Config, while the element's
	// sibling field (Role) stays aligned with Config under the "- " marker.
	input := json.RawMessage(`{"data":{"messages":[{"config":{"timeout":10},"role":"user"}]}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	want := "Messages:\n" +
		"  - Config:\n" +
		"      Timeout:  10\n" +
		"    Role:  user\n"
	if got != want {
		t.Fatalf("nested first field in a sequence item should not collide with its sibling:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestHumanFormatter_Data_KeyValue_NullAtDepthCap(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	// A null leaf at/below the nesting depth cap must still render (none), not a
	// blank value (the depth-cap path uses compactJSON, which returns "" for nil).
	input := json.RawMessage(`{"data":{"a":{"b":{"c":{"d":{"e":{"f":null}}}}}}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	if !strings.Contains(got, "(none)") {
		t.Fatalf("null leaf at the depth cap should render (none), not blank:\n%s", got)
	}
}

func TestHumanFormatter_Data_KeyValue_EmptyNestedObjectIsNone(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})

	input := json.RawMessage(`{"data":{"id":"x","settings":{}}}`)
	if err := f.Data(input, "data", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := stripANSI(out.String())
	// An empty nested object renders inline as (none), so it aligns with the
	// sibling scalar Id at this level.
	want := "ID:        x\n" +
		"Settings:  (none)\n"
	if got != want {
		t.Fatalf("empty nested object should render as (none):\ngot:\n%s\nwant:\n%s", got, want)
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
