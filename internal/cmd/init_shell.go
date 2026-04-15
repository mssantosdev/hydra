package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

const (
	markerStart = "# === HYDRA SHELL HELPER START ==="
	markerEnd   = "# === HYDRA SHELL HELPER END ==="
)

var initShellCmd = &cobra.Command{
	Use:   "init-shell [bash|zsh|fish]",
	Short: "Initialize shell integration",
	Long: `Install or update shell helper functions for hydra.

This command installs shell integration directly into your shell configuration file.
The shell helper enables automatic directory switching with 'hydra switch' and adds
the 'hsw' alias for quick switching.

The helper is smart - it will:
- Detect if already installed and update instead of duplicating
- Add the hsw alias for quick switching
- Handle cd automatically when using hydra switch

Examples:
  # Install for bash (auto-detects shell)
  hydra init-shell

  # Install for specific shell
  hydra init-shell bash

  # Install for zsh
  hydra init-shell zsh

After installing, reload your shell:
  source ~/.bashrc  # or ~/.zshrc`,
	RunE: runInitShell,
}

var (
	installFlag bool
)

func init() {
	rootCmd.AddCommand(initShellCmd)
	initShellCmd.Flags().BoolVarP(&installFlag, "install", "i", true, "Install directly to shell config file")
}

func runInitShell(cmd *cobra.Command, args []string) error {
	// Detect shell
	shell := detectShell()
	if len(args) > 0 {
		shell = args[0]
	}

	// Validate shell
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", shell)
	}

	// Get shell config file path
	configFile := getShellConfigFile(shell)

	// Generate helper content
	var content string
	switch shell {
	case "bash", "zsh":
		content = generateBashZshHelper()
	case "fish":
		content = generateFishHelper()
	}

	// Check if already installed
	existing, err := readShellConfig(configFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", configFile, err)
	}

	var action string
	var newContent string

	if hasExistingInstallation(existing) {
		// Replace existing installation
		newContent = replaceInstallation(existing, content)
		action = "updated"
	} else {
		// Append new installation
		newContent = existing + "\n" + content + "\n"
		action = "installed"
	}

	// Write to shell config
	if err := writeShellConfig(configFile, newContent); err != nil {
		return fmt.Errorf("failed to write %s: %w", configFile, err)
	}

	// Success message
	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9ece6a"))
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))

	fmt.Println()
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Shell helper %s in %s", action, configFile)))
	fmt.Println()
	fmt.Println("The following have been configured:")
	fmt.Println("  • HYDRA_SHELL_HELPER environment variable")
	fmt.Println("  • hydra() wrapper function for auto-cd on switch")
	fmt.Println("  • hsw alias for quick switching")
	fmt.Println()
	fmt.Println(infoStyle.Render("Next steps:"))
	fmt.Printf("  1. Run: %s\n", dimStyle.Render(fmt.Sprintf("source %s", configFile)))
	fmt.Println("  2. Verify: echo $HYDRA_SHELL_HELPER")
	fmt.Println("     Should output: 1")
	fmt.Println()
	fmt.Println("Then you can use:")
	fmt.Println("  hydra switch <worktree>  # Automatically changes directory")
	fmt.Println("  hsw <worktree>           # Quick alias")
	fmt.Println()

	return nil
}

func detectShell() string {
	// Check SHELL environment variable
	shell := os.Getenv("SHELL")
	if strings.Contains(shell, "zsh") {
		return "zsh"
	}
	if strings.Contains(shell, "fish") {
		return "fish"
	}
	// Default to bash
	return "bash"
}

func getShellConfigFile(shell string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	switch shell {
	case "bash":
		return filepath.Join(home, ".bashrc")
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	default:
		return filepath.Join(home, ".bashrc")
	}
}

func readShellConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeShellConfig(path string, content string) error {
	// Create directory if needed (for fish)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func hasExistingInstallation(content string) bool {
	return strings.Contains(content, markerStart)
}

func replaceInstallation(existing, newContent string) string {
	// Find start and end markers
	startIdx := strings.Index(existing, markerStart)
	endIdx := strings.Index(existing, markerEnd)

	if startIdx == -1 || endIdx == -1 {
		// Markers not found, just append
		return existing + "\n" + newContent + "\n"
	}

	// Replace content between markers (including markers)
	before := existing[:startIdx]
	after := existing[endIdx+len(markerEnd):]

	return before + newContent + after
}

func generateBashZshHelper() string {
	return fmt.Sprintf(`%s
# Hydra shell helper - enables automatic directory switching
# This section is managed by 'hydra init-shell'
# Do not edit manually - changes will be overwritten on next init-shell

# Mark shell helper as initialized
export HYDRA_SHELL_HELPER=1

# Wrapper function that handles cd from hydra switch
hydra() {
    # Check if this is a switch command
    if [ "$1" = "switch" ]; then
        # Run hydra switch and capture output
        local output=$(command hydra "$@" 2>&1)
        local exit_code=$?
        
        # Check if output contains cd directive
        if echo "$output" | grep -q "^__HYDRA_CD__"; then
            # Extract path and cd
            local path=$(echo "$output" | grep "^__HYDRA_CD__" | cut -d' ' -f2-)
            if [ -n "$path" ] && [ -d "$path" ]; then
                cd "$path"
            else
                echo "Error: Invalid path: $path" >&2
                return 1
            fi
        else
            # Print output (error messages, etc.)
            echo "$output"
            return $exit_code
        fi
    else
        # Pass through to regular hydra for other commands
        command hydra "$@"
    fi
}

# Alias for quick switching
alias hsw='hydra switch'
%s
`, markerStart, markerEnd)
}

func generateFishHelper() string {
	return fmt.Sprintf(`%s
# Hydra shell helper - enables automatic directory switching
# This section is managed by 'hydra init-shell'
# Do not edit manually - changes will be overwritten on next init-shell

# Mark shell helper as initialized
set -x HYDRA_SHELL_HELPER 1

# Wrapper function that handles cd from hydra switch
function hydra
    # Check if this is a switch command
    if test "$argv[1]" = "switch"
        # Run hydra switch and capture output
        set -l output (command hydra $argv 2>&1)
        set -l exit_code $status
        
        # Check if output contains cd directive
        if echo "$output" | grep -q "^__HYDRA_CD__"
            # Extract path and cd
            set -l path (echo "$output" | grep "^__HYDRA_CD__" | cut -d' ' -f2-)
            if test -n "$path" -a -d "$path"
                cd "$path"
            else
                echo "Error: Invalid path: $path" >&2
                return 1
            end
        else
            # Print output (error messages, etc.)
            echo "$output"
            return $exit_code
        end
    else
        # Pass through to regular hydra for other commands
        command hydra $argv
    end
end

# Alias for quick switching
alias hsw 'hydra switch'
%s
`, markerStart, markerEnd)
}
