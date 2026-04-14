package log

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

// Logger is the global logger instance
var Logger *log.Logger

// Verbose controls whether to show debug messages
var Verbose bool

func init() {
	// Create logger with Tokyo Night colors
	styles := log.DefaultStyles()

	// Info - Blue
	styles.Levels[log.InfoLevel] = lipgloss.NewStyle().
		SetString("›").
		Padding(0, 1).
		Background(lipgloss.Color("#7aa2f7")).
		Foreground(lipgloss.Color("#1a1b26")).
		Bold(true)

	// Error - Red
	styles.Levels[log.ErrorLevel] = lipgloss.NewStyle().
		SetString("✗").
		Padding(0, 1).
		Background(lipgloss.Color("#f7768e")).
		Foreground(lipgloss.Color("#1a1b26")).
		Bold(true)

	// Warn - Yellow/Orange
	styles.Levels[log.WarnLevel] = lipgloss.NewStyle().
		SetString("!").
		Padding(0, 1).
		Background(lipgloss.Color("#e0af68")).
		Foreground(lipgloss.Color("#1a1b26")).
		Bold(true)

	// Debug - Purple (only shown when Verbose is true)
	styles.Levels[log.DebugLevel] = lipgloss.NewStyle().
		SetString("•").
		Padding(0, 1).
		Background(lipgloss.Color("#bb9af7")).
		Foreground(lipgloss.Color("#1a1b26")).
		Bold(true)

	// Success - Green
	styles.Levels[log.InfoLevel] = lipgloss.NewStyle().
		SetString("✓").
		Padding(0, 1).
		Background(lipgloss.Color("#9ece6a")).
		Foreground(lipgloss.Color("#1a1b26")).
		Bold(true)

	Logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: false,
		Level:           log.InfoLevel,
	})
	Logger.SetStyles(styles)
}

// SetVerbose enables or disables debug logging
func SetVerbose(v bool) {
	Verbose = v
	if v {
		Logger.SetLevel(log.DebugLevel)
	} else {
		Logger.SetLevel(log.InfoLevel)
	}
}

// Info logs an info message
func Info(msg string, keyvals ...interface{}) {
	// Temporarily set to Info level for this call
	oldLevel := Logger.GetLevel()
	Logger.SetLevel(log.InfoLevel)
	Logger.Info(msg, keyvals...)
	Logger.SetLevel(oldLevel)
}

// Error logs an error message
func Error(msg string, keyvals ...interface{}) {
	Logger.Error(msg, keyvals...)
}

// Warn logs a warning message
func Warn(msg string, keyvals ...interface{}) {
	Logger.Warn(msg, keyvals...)
}

// Debug logs a debug message (only if Verbose is true)
func Debug(msg string, keyvals ...interface{}) {
	if Verbose {
		Logger.Debug(msg, keyvals...)
	}
}

// Success logs a success message
func Success(msg string, keyvals ...interface{}) {
	// Force info level styling for success
	oldLevel := Logger.GetLevel()
	Logger.SetLevel(log.InfoLevel)
	Logger.Info(msg, keyvals...)
	Logger.SetLevel(oldLevel)
}

// Fatal logs a fatal message and exits
func Fatal(msg string, keyvals ...interface{}) {
	Logger.Fatal(msg, keyvals...)
}

// Print prints a message without level prefix
func Print(msg string) {
	Logger.Print(msg)
}

// Header prints a styled header
func Header(title string) {
	headerStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#7aa2f7")).
		Foreground(lipgloss.Color("#1a1b26")).
		Bold(true).
		Padding(0, 2)
	Logger.Print(headerStyle.Render(title))
}

// Subtitle prints a styled subtitle
func Subtitle(text string) {
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#565f89")).
		MarginBottom(1)
	Logger.Print(subtitleStyle.Render(text))
}
