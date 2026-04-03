package output

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	spin "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7559FF"))

const clearLine = "\r\033[2K"

type statusMsg struct {
	status  string
	elapsed time.Duration
}

type quitMsg struct{}

// PollSpinner renders interactive stderr progress for --wait polling.
type PollSpinner struct {
	program *tea.Program
	done    chan struct{}
	once    sync.Once
}

// StartPollSpinner starts a spinner program that writes to w.
func StartPollSpinner(w io.Writer) *PollSpinner {
	model := newSpinnerModel()
	program := tea.NewProgram(
		model,
		tea.WithOutput(w),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
	)

	s := &PollSpinner{
		program: program,
		done:    make(chan struct{}),
	}

	go func() {
		defer close(s.done)
		_, _ = program.Run()
	}()

	return s
}

// UpdateStatus updates the rendered status and elapsed time.
func (s *PollSpinner) UpdateStatus(status string, elapsed time.Duration) {
	if s == nil || s.program == nil {
		return
	}
	s.program.Send(statusMsg{
		status:  status,
		elapsed: elapsed,
	})
}

// Stop clears the spinner line and waits for the program to exit.
func (s *PollSpinner) Stop() {
	if s == nil || s.program == nil {
		return
	}
	s.once.Do(func() {
		s.program.Send(quitMsg{})
		<-s.done
	})
}

type spinnerModel struct {
	spinner  spin.Model
	status   string
	elapsed  time.Duration
	quitting bool
}

func newSpinnerModel() spinnerModel {
	m := spinnerModel{
		spinner: spin.New(
			spin.WithSpinner(spin.MiniDot),
			spin.WithStyle(spinnerStyle),
		),
		status: "waiting",
	}
	return m
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusMsg:
		m.status = msg.status
		m.elapsed = msg.elapsed
		return m, nil
	case quitMsg:
		m.quitting = true
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

func (m spinnerModel) View() string {
	if m.quitting {
		return clearLine
	}

	return fmt.Sprintf(
		"\r%s %s %s",
		m.spinner.View(),
		renderSpinnerStatus(m.status),
		hintStyle.Render(formatSpinnerElapsed(m.elapsed)),
	)
}

func renderSpinnerStatus(status string) string {
	text := strings.TrimSpace(status)
	if text == "" {
		text = "waiting"
	}

	text = strings.ReplaceAll(text, "_", " ")
	text = strings.ReplaceAll(text, "-", " ")
	text = strings.ToLower(text)
	if len(text) > 0 {
		text = strings.ToUpper(text[:1]) + text[1:]
	}

	return text + "..."
}

func formatSpinnerElapsed(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	return fmt.Sprintf("(%s)", elapsed.Round(time.Second))
}
