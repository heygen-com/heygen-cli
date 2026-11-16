package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/internal/command"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

const hostileJSON = `a\u001b]0;title\u0007b\u009b31mc\u007fd`

func assertNoRawControls(t *testing.T, printed string) {
	t.Helper()
	got := stripANSI(printed)
	for _, control := range []string{"\x1b", "\x07", "\u009b", "\x7f", "\r"} {
		if strings.Contains(got, control) {
			t.Fatalf("raw control %q reached the terminal:\n%q", control, got)
		}
	}
	if !strings.Contains(got, `\x1b]0;title\x07b\x9b31mc\x7fd`) {
		t.Fatalf("the escaped text is missing:\n%q", got)
	}
}

func TestHumanFormatter_TableEscapesControlsInCellsAndHeaders(t *testing.T) {
	var out bytes.Buffer
	f := NewHumanFormatter(&out, &bytes.Buffer{})
	rows := json.RawMessage(`[{"` + hostileJSON + `":"` + hostileJSON + `"}]`)

	if err := f.Data(rows, "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoRawControls(t, out.String())

	out.Reset()
	if err := f.Data(json.RawMessage(`[{"t":"`+hostileJSON+`"}]`), "", []command.Column{{Header: "T", Field: "t"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoRawControls(t, out.String())
}

func TestHumanFormatter_KeyValueEscapesControlsInLabelsAndValues(t *testing.T) {
	for _, payload := range []string{
		`{"` + hostileJSON + `":"x"}`,
		`{"title":"` + hostileJSON + `"}`,
		`{"title":"line one\n` + hostileJSON + `"}`,
		`{"tags":["` + hostileJSON + `","b"]}`,
	} {
		var out bytes.Buffer
		f := NewHumanFormatter(&out, &bytes.Buffer{})
		if err := f.Data(json.RawMessage(payload), "", nil); err != nil {
			t.Fatalf("unexpected error for %s: %v", payload, err)
		}
		if strings.Contains(payload, `line one\n`) && !strings.Contains(out.String(), "line one\n") {
			t.Fatalf("the layout's own newline was lost:\n%q", out.String())
		}
		assertNoRawControls(t, out.String())
	}
}

func TestHumanFormatter_ErrorWarnNoticeEscapeControls(t *testing.T) {
	var hostile string
	if err := json.Unmarshal([]byte(`"`+hostileJSON+`"`), &hostile); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	f := NewHumanFormatter(&bytes.Buffer{}, &errOut)

	f.Error(&clierrors.CLIError{Message: hostile, Hint: hostile})
	f.Warn(hostile)
	f.Notice("code", hostile)

	if got := strings.Count(stripANSI(errOut.String()), `a\x1b]0;title\x07b\x9b31mc\x7fd`); got != 4 {
		t.Fatalf("want message, hint, warning and note escaped, got %d:\n%q", got, errOut.String())
	}
	assertNoRawControls(t, errOut.String())
}

func TestTerminalTextKeepsPlainTextTabsAndNewlines(t *testing.T) {
	for _, s := range []string{"plain", "tab\there", "two\nlines", "über ✓"} {
		if got := terminalText(s); got != s {
			t.Fatalf("terminalText(%q) = %q", s, got)
		}
	}
}
