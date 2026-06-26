package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
)

// loginChoice identifies which login flow the user picked at the TUI
// prompt. The picker is shown only when stdin/stdout are TTYs and no
// explicit flag was passed; non-interactive shells default to the API
// key path without ever instantiating the model below.
type loginChoice int

// The numeric ordering of these constants is independent of how the
// picker renders them — the runner is selected by the loginChoice
// returned, not by the cursor's row. We keep the values stable so
// downstream switches stay exhaustive even as the picker ordering
// evolves.
const (
	loginChoiceOAuth loginChoice = iota
	loginChoiceAPIKey
)

// runPickerFunc is overridden in tests so the dispatch logic in
// runAuthLogin can be exercised without spinning a real Bubble Tea
// program. Production code uses runLoginPicker.
var runPickerFunc = runLoginPicker

// pickerCanceledError is the sentinel returned when the user dismisses
// the picker with Esc / Ctrl-C / q. The dispatcher unwraps it into a
// "canceled" CLI error so the user sees the same exit shape they get
// from any other destructive cancellation.
type pickerCanceledError struct{}

func (pickerCanceledError) Error() string { return "login canceled by user" }

// loginPickerOption represents a single row in the picker. Kept in this
// file (rather than the dispatcher's) so the model is the source of
// truth for how the choices render.
type loginPickerOption struct {
	choice      loginChoice
	title       string
	description string
}

// loginPickerOptions controls both the order rows render in AND the
// default highlighted choice (the first entry, cursor index 0). The
// API-key path is listed first because heygen-cli is agent-first —
// most installs feed a pre-provisioned key. OAuth stays a
// one-arrow-down option for users who want subscription pricing.
// No "Recommended" label per James — position + description carry
// the suggestion without being prescriptive.
var loginPickerOptions = []loginPickerOption{
	{
		choice:      loginChoiceAPIKey,
		title:       "Use an API key",
		description: "Paste an existing key. Uses API credits.",
	},
	{
		choice:      loginChoiceOAuth,
		title:       "Login with HeyGen.com",
		description: "Opens a browser. Uses subscription credits.",
	},
}

// loginPickerModel is the Bubble Tea model behind the interactive
// picker. It deliberately holds no styling state beyond the cursor
// position so resizes degrade to a plain rerender of the same View().
type loginPickerModel struct {
	cursor   int
	selected bool
	canceled bool
	width    int
}

func newLoginPickerModel() loginPickerModel {
	return loginPickerModel{cursor: 0}
}

func (m loginPickerModel) Init() tea.Cmd { return nil }

func (m loginPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.canceled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(loginPickerOptions) - 1
			}
			return m, nil
		case "down", "j", "tab":
			if m.cursor < len(loginPickerOptions)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
			return m, nil
		case "enter", " ":
			m.selected = true
			return m, tea.Quit
		case "1":
			m.cursor = 0
			m.selected = true
			return m, tea.Quit
		case "2":
			m.cursor = 1
			m.selected = true
			return m, tea.Quit
		}
	}
	return m, nil
}

var (
	pickerHeaderStyle = lipgloss.NewStyle().Bold(true)
	pickerCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7559FF")).Bold(true)
	pickerActiveTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7559FF")).Bold(true)
	pickerDimStyle    = lipgloss.NewStyle().Faint(true)
)

func (m loginPickerModel) View() string {
	if m.selected || m.canceled {
		// Clear the picker block on exit so the runner's own output
		// (browser handoff URL, API-key prompt) starts on a fresh line.
		return ""
	}

	var b strings.Builder
	b.WriteString(pickerHeaderStyle.Render("Welcome to HeyGen!"))
	b.WriteString("\n\n")
	b.WriteString("How would you like to log in?")
	b.WriteString("\n\n")

	for i, opt := range loginPickerOptions {
		cursor := "  "
		title := opt.title
		if i == m.cursor {
			cursor = pickerCursorStyle.Render("> ")
			title = pickerActiveTitle.Render(opt.title)
		}
		b.WriteString(cursor)
		b.WriteString(title)
		b.WriteString("\n  ")
		b.WriteString(pickerDimStyle.Render(opt.description))
		b.WriteString("\n\n")
	}

	b.WriteString(pickerDimStyle.Render("↑/↓ to move · enter to select · esc to cancel"))
	b.WriteString("\n")
	return b.String()
}

// runLoginPicker drives the picker against the supplied stdin/stderr
// streams. stderr is intentional — stdout is reserved for the CLI's
// machine-readable JSON envelope, so the picker shares the same
// channel as prompts and progress messages.
func runLoginPicker(ctx context.Context, stdin io.Reader, stderr io.Writer) (loginChoice, error) {
	prog := tea.NewProgram(
		newLoginPickerModel(),
		tea.WithContext(ctx),
		tea.WithInput(stdin),
		tea.WithOutput(stderr),
	)

	finalModel, err := prog.Run()
	if err != nil {
		return loginChoiceOAuth, clierrors.New(fmt.Sprintf("login picker failed: %v", err))
	}
	m, ok := finalModel.(loginPickerModel)
	if !ok {
		return loginChoiceOAuth, clierrors.New("login picker returned unexpected model type")
	}
	if m.canceled {
		return loginChoiceOAuth, pickerCanceledError{}
	}
	if !m.selected || m.cursor < 0 || m.cursor >= len(loginPickerOptions) {
		// Defensive fallback — the model should always set one of
		// {selected, canceled} before exiting tea.Quit, but if some
		// unexpected message slipped through we treat it as a cancel.
		return loginChoiceOAuth, pickerCanceledError{}
	}
	return loginPickerOptions[m.cursor].choice, nil
}
