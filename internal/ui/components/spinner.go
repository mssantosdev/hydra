package components

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Spinner is a reusable spinner component with Tokyo Night theming
type Spinner struct {
	spinner  spinner.Model
	message  string
	finished bool
}

// SpinnerModel represents the spinner type
type SpinnerModel int

const (
	SpinnerDots SpinnerModel = iota
	SpinnerLine
	SpinnerMiniDot
	SpinnerJump
	SpinnerPulse
)

// NewSpinner creates a new spinner with the given message
func NewSpinner(message string, model SpinnerModel) Spinner {
	s := spinner.New()

	switch model {
	case SpinnerLine:
		s.Spinner = spinner.Line
	case SpinnerMiniDot:
		s.Spinner = spinner.MiniDot
	case SpinnerJump:
		s.Spinner = spinner.Jump
	case SpinnerPulse:
		s.Spinner = spinner.Pulse
	default:
		s.Spinner = spinner.Dot
	}

	// Tokyo Night blue
	s.Style = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7aa2f7"))

	return Spinner{
		spinner: s,
		message: message,
	}
}

// Init initializes the spinner
func (s Spinner) Init() tea.Cmd {
	return s.spinner.Tick
}

// Update updates the spinner state
func (s Spinner) Update(msg tea.Msg) (Spinner, tea.Cmd) {
	if s.finished {
		return s, nil
	}

	var cmd tea.Cmd
	s.spinner, cmd = s.spinner.Update(msg)
	return s, cmd
}

// View renders the spinner
func (s Spinner) View() string {
	if s.finished {
		return ""
	}
	return fmt.Sprintf("%s %s", s.spinner.View(), s.message)
}

// Finish marks the spinner as finished
func (s *Spinner) Finish() {
	s.finished = true
}

// SetMessage updates the spinner message
func (s *Spinner) SetMessage(msg string) {
	s.message = msg
}

// IsFinished returns whether the spinner is finished
func (s Spinner) IsFinished() bool {
	return s.finished
}

// Task represents a task with a spinner
type Task struct {
	Name      string
	Spinner   Spinner
	StartTime time.Time
	EndTime   *time.Time
	Error     error
}

// NewTask creates a new task with a spinner
func NewTask(name string) Task {
	return Task{
		Name:      name,
		Spinner:   NewSpinner(name, SpinnerDots),
		StartTime: time.Now(),
	}
}

// Complete marks the task as complete
func (t *Task) Complete() {
	t.Spinner.Finish()
	now := time.Now()
	t.EndTime = &now
}

// Fail marks the task as failed with an error
func (t *Task) Fail(err error) {
	t.Error = err
	t.Complete()
}

// Duration returns the task duration
func (t Task) Duration() time.Duration {
	if t.EndTime != nil {
		return t.EndTime.Sub(t.StartTime)
	}
	return time.Since(t.StartTime)
}

// DurationString returns a formatted duration string
func (t Task) DurationString() string {
	d := t.Duration()
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
