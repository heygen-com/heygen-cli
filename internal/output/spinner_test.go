package output

import (
	"strings"
	"testing"
	"time"

	spin "github.com/charmbracelet/bubbles/spinner"
)

func TestSpinnerModelViewIncludesStatusAndElapsed(t *testing.T) {
	model := newSpinnerModel()
	model.spinner = spin.New(spin.WithSpinner(spin.Line))

	updated, cmd := model.Update(statusMsg{
		status:  "processing",
		elapsed: 12 * time.Second,
	})
	if cmd != nil {
		t.Fatalf("status update returned unexpected command")
	}

	got := updated.(spinnerModel).View()
	if !strings.Contains(got, "Processing...") {
		t.Fatalf("View() = %q, want Processing...", got)
	}
	if !strings.Contains(got, "(12s)") {
		t.Fatalf("View() = %q, want elapsed time", got)
	}
}

func TestSpinnerModelQuitClearsLine(t *testing.T) {
	model := newSpinnerModel()

	updated, cmd := model.Update(quitMsg{})
	if cmd == nil {
		t.Fatal("quit update returned nil command")
	}

	got := updated.(spinnerModel).View()
	if got != clearLine {
		t.Fatalf("View() = %q, want %q", got, clearLine)
	}
}

func TestRenderSpinnerStatusHumanizesValue(t *testing.T) {
	got := renderSpinnerStatus("in_progress")
	if got != "In progress..." {
		t.Fatalf("renderSpinnerStatus() = %q, want %q", got, "In progress...")
	}
}
