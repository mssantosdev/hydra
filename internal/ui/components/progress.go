package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

// Progress is a reusable progress component with Tokyo Night theming
type Progress struct {
	progress progress.Model
	message  string
	percent  float64
	showSize bool
	sizeMB   float64
}

// NewProgress creates a new progress bar
func NewProgress(message string, showSize bool) Progress {
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	return Progress{
		progress: p,
		message:  message,
		showSize: showSize,
	}
}

// Init initializes the progress bar
func (p Progress) Init() tea.Cmd {
	return nil
}

// Update updates the progress state
func (p Progress) Update(msg tea.Msg) (Progress, tea.Cmd) {
	var cmd tea.Cmd
	newModel, cmd := p.progress.Update(msg)
	p.progress = newModel.(progress.Model)
	return p, cmd
}

// View renders the progress bar
func (p Progress) View() string {
	if p.showSize && p.sizeMB > 0 {
		return fmt.Sprintf("%s %s (%.1f MB)", p.message, p.progress.ViewAs(p.percent), p.sizeMB)
	}
	return fmt.Sprintf("%s %s", p.message, p.progress.ViewAs(p.percent))
}

// SetPercent updates the progress percentage
func (p *Progress) SetPercent(percent float64) {
	p.percent = percent
}

// GetPercent returns the current percentage
func (p Progress) GetPercent() float64 {
	return p.percent
}

// SetSizeMB updates the size in MB
func (p *Progress) SetSizeMB(size float64) {
	p.sizeMB = size
}

// SimpleProgress is a simpler progress display (spinner + size)
type SimpleProgress struct {
	spinner  Spinner
	message  string
	sizeMB   float64
	finished bool
}

// NewSimpleProgress creates a simple progress indicator
func NewSimpleProgress(message string) SimpleProgress {
	return SimpleProgress{
		spinner: NewSpinner(message, SpinnerDots),
		message: message,
	}
}

// Init initializes the simple progress
func (p SimpleProgress) Init() tea.Cmd {
	return p.spinner.Init()
}

// Update updates the simple progress state
func (p SimpleProgress) Update(msg tea.Msg) (SimpleProgress, tea.Cmd) {
	if p.finished {
		return p, nil
	}

	var cmd tea.Cmd
	p.spinner, cmd = p.spinner.Update(msg)
	return p, cmd
}

// View renders the simple progress
func (p SimpleProgress) View() string {
	if p.finished {
		return ""
	}
	if p.sizeMB > 0 {
		return fmt.Sprintf("%s (%.1f MB)", p.spinner.View(), p.sizeMB)
	}
	return p.spinner.View()
}

// SetSizeMB updates the size in MB
func (p *SimpleProgress) SetSizeMB(size float64) {
	p.sizeMB = size
}

// Finish marks the progress as finished
func (p *SimpleProgress) Finish() {
	p.finished = true
	p.spinner.Finish()
}

// IsFinished returns whether the progress is finished
func (p SimpleProgress) IsFinished() bool {
	return p.finished
}
