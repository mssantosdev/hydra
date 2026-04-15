package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initShellCmd = &cobra.Command{
	Use:   "init-shell [bash|zsh|fish]",
	Short: "Initialize shell integration",
	Long: `Generate shell helper functions for hydra.

This command outputs shell code that should be added to your shell configuration file.
The shell helper enables automatic directory switching with 'hydra switch'.

Examples:
  # Bash
  hydra init-shell bash >> ~/.bashrc
  source ~/.bashrc

  # Zsh
  hydra init-shell zsh >> ~/.zshrc
  source ~/.zshrc

  # Fish
  hydra init-shell fish >> ~/.config/fish/config.fish`,
	RunE: runInitShell,
}

func init() {
	rootCmd.AddCommand(initShellCmd)
}

func runInitShell(cmd *cobra.Command, args []string) error {
	shell := "bash"
	if len(args) > 0 {
		shell = args[0]
	}

	switch shell {
	case "bash", "zsh":
		fmt.Print(bashZshHelper)
	case "fish":
		fmt.Print(fishHelper)
	default:
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", shell)
	}

	return nil
}

const bashZshHelper = `# Hydra shell helper
# Add this to your ~/.bashrc or ~/.zshrc

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

# Optional: alias for quick switching
alias hsw='hydra switch'
`

const fishHelper = `# Hydra shell helper for Fish
# Add this to your ~/.config/fish/config.fish

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

# Optional: alias for quick switching
alias hsw 'hydra switch'
`
