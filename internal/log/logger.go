// Package log is hydra's stderr logger. Everything here writes to stderr so it
// can never corrupt the JSON envelope on stdout.
package log

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"

	"github.com/mssantosdev/hydra/internal/ui/styles"
)

// Logger is the global logger instance.
var Logger *log.Logger

// Verbose controls whether debug messages are emitted.
var Verbose bool

func init() {
	levels := log.DefaultStyles()

	levels.Levels[log.InfoLevel] = badge("✓", styles.Green)
	levels.Levels[log.ErrorLevel] = badge("✗", styles.Red)
	levels.Levels[log.WarnLevel] = badge("!", styles.Orange)
	levels.Levels[log.DebugLevel] = badge("•", styles.Purple)

	Logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: false,
		Level:           log.InfoLevel,
	})
	Logger.SetStyles(levels)
}

func badge(symbol string, bg lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().
		SetString(symbol).
		Padding(0, 1).
		Background(bg).
		Foreground(styles.BgDark).
		Bold(true)
}

// SetVerbose enables or disables debug logging. This is the ONLY place the level
// is set: mutating it per call races the concurrent goroutines in `hydra sync`.
func SetVerbose(v bool) {
	Verbose = v
	if v {
		Logger.SetLevel(log.DebugLevel)
		return
	}
	Logger.SetLevel(log.InfoLevel)
}

// Info logs an info message.
func Info(msg string, keyvals ...interface{}) {
	Logger.Info(msg, keyvals...)
}

// Error logs an error message.
func Error(msg string, keyvals ...interface{}) {
	Logger.Error(msg, keyvals...)
}

// Warn logs a warning message.
func Warn(msg string, keyvals ...interface{}) {
	Logger.Warn(msg, keyvals...)
}

// Debug logs a debug message; the level filter already gates it.
func Debug(msg string, keyvals ...interface{}) {
	Logger.Debug(msg, keyvals...)
}

// Success logs a success message.
func Success(msg string, keyvals ...interface{}) {
	Logger.Info(msg, keyvals...)
}

// Print prints a message without a level prefix.
func Print(msg string) {
	Logger.Print(msg)
}

// Header prints a styled header.
func Header(title string) {
	Logger.Print(styles.Header.Render(title))
}

// Subtitle prints a styled subtitle.
func Subtitle(text string) {
	Logger.Print(styles.Subtitle.Render(text))
}
