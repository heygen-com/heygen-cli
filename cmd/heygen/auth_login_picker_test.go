package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// runeKey is a tiny helper for synthesizing tea.KeyMsgs in tests.
// Bubble Tea's Update reads KeyMsg.Type/Runes/Alt, but the tea.KeyMsg
// .String() helper (which our Update calls into) covers the common
// cases — so we can just plumb a KeyType and let the rest be zero.
func runeKey(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func step(m tea.Model, msg tea.Msg) tea.Model {
	next, _ := m.Update(msg)
	return next
}

func TestLoginPickerModel_DownArrowAdvancesCursorAndWrapsAround(t *testing.T) {
	m := tea.Model(newLoginPickerModel())
	m = step(m, runeKey("down"))
	pm := m.(loginPickerModel)
	if pm.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", pm.cursor)
	}
	// Wrap-around: another "down" from the last entry should return to 0.
	m = step(m, runeKey("down"))
	pm = m.(loginPickerModel)
	if pm.cursor != 0 {
		t.Fatalf("cursor (wrap) = %d, want 0", pm.cursor)
	}
}

func TestLoginPickerModel_UpArrowFromTopWrapsToBottom(t *testing.T) {
	m := tea.Model(newLoginPickerModel())
	m = step(m, runeKey("up"))
	pm := m.(loginPickerModel)
	if pm.cursor != len(loginPickerOptions)-1 {
		t.Fatalf("cursor (wrap up) = %d, want %d", pm.cursor, len(loginPickerOptions)-1)
	}
}

func TestLoginPickerModel_JKVimMotionWorks(t *testing.T) {
	m := tea.Model(newLoginPickerModel())
	m = step(m, runeKey("j"))
	if m.(loginPickerModel).cursor != 1 {
		t.Fatalf("after j: cursor = %d, want 1", m.(loginPickerModel).cursor)
	}
	m = step(m, runeKey("k"))
	if m.(loginPickerModel).cursor != 0 {
		t.Fatalf("after k: cursor = %d, want 0", m.(loginPickerModel).cursor)
	}
}

func TestLoginPickerModel_EnterMarksSelected(t *testing.T) {
	m := tea.Model(newLoginPickerModel())
	m = step(m, runeKey("down"))
	next, cmd := m.Update(runeKey("enter"))
	pm := next.(loginPickerModel)
	if !pm.selected {
		t.Fatalf("selected = false, want true")
	}
	if pm.canceled {
		t.Fatalf("canceled = true, want false")
	}
	if pm.cursor != 1 {
		t.Fatalf("cursor at selection time = %d, want 1", pm.cursor)
	}
	if cmd == nil {
		t.Fatalf("expected tea.Quit cmd on enter, got nil")
	}
}

func TestLoginPickerModel_EscMarksCanceled(t *testing.T) {
	m := tea.Model(newLoginPickerModel())
	next, cmd := m.Update(runeKey("esc"))
	pm := next.(loginPickerModel)
	if !pm.canceled {
		t.Fatalf("canceled = false, want true")
	}
	if pm.selected {
		t.Fatalf("selected = true, want false")
	}
	if cmd == nil {
		t.Fatalf("expected tea.Quit cmd on esc, got nil")
	}
}

func TestLoginPickerModel_CtrlCMarksCanceled(t *testing.T) {
	m := tea.Model(newLoginPickerModel())
	next, _ := m.Update(runeKey("ctrl+c"))
	if !next.(loginPickerModel).canceled {
		t.Fatalf("ctrl+c did not set canceled")
	}
}

func TestLoginPickerModel_NumericShortcutsJumpAndSelect(t *testing.T) {
	m := tea.Model(newLoginPickerModel())
	next, _ := m.Update(runeKey("2"))
	pm := next.(loginPickerModel)
	if !pm.selected || pm.cursor != 1 {
		t.Fatalf("'2' shortcut: selected=%v cursor=%d, want selected=true cursor=1", pm.selected, pm.cursor)
	}
}

func TestLoginPickerModel_WindowResizeIsHarmless(t *testing.T) {
	m := tea.Model(newLoginPickerModel())
	// Tiny terminal — Bubble Tea's renderer truncates as needed; the
	// model must not panic and must remember the width so subsequent
	// View() calls can adapt.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("WindowSizeMsg panicked: %v", r)
		}
	}()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	if next.(loginPickerModel).width != 20 {
		t.Fatalf("width not recorded: got %d", next.(loginPickerModel).width)
	}
	_ = next.View() // must not panic with narrow width either
}

func TestLoginPickerModel_ViewMentionsBothOptionsAndCreditNotes(t *testing.T) {
	v := newLoginPickerModel().View()
	if !strings.Contains(v, "Welcome to HeyGen!") {
		t.Errorf("view missing welcome header:\n%s", v)
	}
	if !strings.Contains(v, "Login with HeyGen.com") {
		t.Errorf("view missing OAuth option:\n%s", v)
	}
	if !strings.Contains(v, "Use an API key") {
		t.Errorf("view missing API-key option:\n%s", v)
	}
	if !strings.Contains(v, "subscription credits") {
		t.Errorf("view missing subscription-credits note:\n%s", v)
	}
	if !strings.Contains(v, "API credits") {
		t.Errorf("view missing API-credits note:\n%s", v)
	}
}

func TestLoginPickerModel_ViewIsBlankAfterExit(t *testing.T) {
	// Once selected or canceled, View() must return an empty string so
	// the picker block doesn't linger above the runner's own output.
	for _, m := range []loginPickerModel{
		{selected: true, cursor: 0},
		{canceled: true},
	} {
		if got := m.View(); got != "" {
			t.Errorf("post-exit View = %q, want empty", got)
		}
	}
}
