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
	Long: `Install shell helper for automatic directory switching.

DESCRIPTION
  Installs a shell wrapper that enables 'hydra switch' to automatically
  change directories. Without this, 'hydra switch' can only suggest paths.

  Installs into your shell config:
    • Bash: ~/.bashrc
    • Zsh:  ~/.zshrc
    • Fish: ~/.config/fish/config.fish

  What it adds:
    • hydra() wrapper function
    • HYDRA_SHELL_HELPER=1 environment variable
    • hsw alias for quick switching

WHEN TO USE
  • First-time Hydra setup (required for switch to work)
  • After reinstalling Hydra
  • When 'hydra switch' says "shell helper not initialized"

EXAMPLES
  # Auto-detect shell and install
  $ hydra init-shell

  # Install for specific shell
  $ hydra init-shell zsh

  # After installing, reload shell
  $ source ~/.bashrc  # or ~/.zshrc

  # Verify installation
  $ echo $HYDRA_SHELL_HELPER
  1

FLAGS
  -i, --install   Install to shell config (default: true)
  -h, --help      Show help

EXIT CODES
  0  Success (helper installed/updated)
  1  General error (unsupported shell, write failed)

SUPPORTED SHELLS
  bash, zsh, fish

NOTES
  • Existing installations are updated (not duplicated)
  • Removes previous helper blocks before adding new ones
  • Backup your shell config before first install

SEE ALSO
  • hydra switch - Command that requires this helper
  • hydra glossary - Learn about worktrees and other concepts
  • Docs: https://github.com/mssantosdev/hydra/blob/main/docs/commands/shell-integration.md`,
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
        local cleanup_output_file=0
        local output_file="$HYDRA_SWITCH_OUTPUT_FILE"
        if [ -z "$output_file" ]; then
            output_file=$(mktemp "${TMPDIR:-/tmp}/hydra-switch.XXXXXX") || return 1
            cleanup_output_file=1
        fi
        : > "$output_file"

        HYDRA_SWITCH_OUTPUT_FILE="$output_file" command hydra "$@"
        local exit_code=$?

        local path=""
        if [ -f "$output_file" ]; then
            path=$(cat "$output_file")
        fi

        if [ $cleanup_output_file -eq 1 ]; then
            rm -f "$output_file"
        fi

        if [ $exit_code -eq 0 ] && [ -n "$path" ]; then
            if [ -d "$path" ]; then
                cd "$path"
            else
                echo "Error: Invalid path: $path" >&2
                return 1
            fi
        fi
        return $exit_code
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
        set -l cleanup_output_file 0
        set -l output_file $HYDRA_SWITCH_OUTPUT_FILE
        if test -z "$output_file"
            set output_file (mktemp "TMPDIR=${TMPDIR:-/tmp} hydra-switch.XXXXXX")
            or return 1
            set cleanup_output_file 1
        end
        printf '' > "$output_file"

        env HYDRA_SWITCH_OUTPUT_FILE="$output_file" command hydra $argv
        set -l exit_code $status

        set -l path ''
        if test -f "$output_file"
            set path (cat "$output_file")
        end

        if test $cleanup_output_file -eq 1
            rm -f "$output_file"
        end

        if test $exit_code -eq 0 -a -n "$path"
            if test -d "$path"
                cd "$path"
            else
                echo "Error: Invalid path: $path" >&2
                return 1
            end
        end
        return $exit_code
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
