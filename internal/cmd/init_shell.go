package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

const (
	helperMarkerStart = "# === HYDRA SHELL HELPER START ==="
	helperMarkerEnd   = "# === HYDRA SHELL HELPER END ==="
)

var (
	withCompletion    bool
	withoutCompletion bool
)

var initShellCmd = &cobra.Command{
	Use:   "init-shell [bash|zsh|fish]",
	Short: "Install shell integration",
	Long: `Install shell helper for automatic directory switching.

` + "`hydra init-shell`" + ` writes a small loader block to your shell config and
stores generated helper files under ~/.config/hydra/shell/.

Use ` + "`hydra completion <shell>`" + ` to print a completion script directly, or
let init-shell install it alongside the helper.`,
	RunE: runInitShell,
}

func init() {
	rootCmd.AddCommand(initShellCmd)
	initShellCmd.Flags().BoolVar(&withCompletion, "with-completion", false, "Install completion alongside the shell helper")
	initShellCmd.Flags().BoolVar(&withoutCompletion, "without-completion", false, "Install only the shell helper")
}

func runInitShell(cmd *cobra.Command, args []string) error {
	if withCompletion && withoutCompletion {
		return fmt.Errorf("--with-completion and --without-completion are mutually exclusive")
	}

	shell := detectShell()
	if len(args) > 0 {
		shell = args[0]
	}
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", shell)
	}

	installCompletion := withCompletion
	if !withCompletion && !withoutCompletion {
		installCompletion = promptInstallCompletion(cmd, shell)
	}

	if err := writeShellAssets(shell, installCompletion); err != nil {
		return err
	}

	return renderInitShellSummary(cmd, shell, installCompletion)
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if strings.Contains(shell, "zsh") {
		return "zsh"
	}
	if strings.Contains(shell, "fish") {
		return "fish"
	}
	return "bash"
}

func promptInstallCompletion(cmd *cobra.Command, shell string) bool {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Install completion files for %s too? [Y/n]: ", shell)
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return true
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes"
}

func writeShellAssets(shell string, installCompletion bool) error {
	helperPath, completionPath, err := shellAssetPaths(shell)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(helperPath), 0o755); err != nil {
		return err
	}

	helperContent := renderShellHelper(shell, completionPath)
	if err := os.WriteFile(helperPath, []byte(helperContent), 0o644); err != nil {
		return err
	}

	if installCompletion {
		completionScript, err := renderCompletionScript(shell)
		if err != nil {
			return err
		}
		if err := os.WriteFile(completionPath, []byte(completionScript), 0o644); err != nil {
			return err
		}
	} else {
		_ = os.Remove(completionPath)
	}

	return writeLoaderBlock(shell, helperPath)
}

func renderInitShellSummary(cmd *cobra.Command, shell string, installCompletion bool) error {
	helperPath, completionPath, err := shellAssetPaths(shell)
	if err != nil {
		return err
	}

	styleOK := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9ece6a"))
	styleInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7"))
	styleDim := lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), styleOK.Render(fmt.Sprintf("✓ Shell helper installed for %s", shell)))
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintf(cmd.OutOrStdout(), "Helper: %s\n", helperPath)
	if installCompletion {
		fmt.Fprintf(cmd.OutOrStdout(), "Completion: %s\n", completionPath)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "The loader block in your shell rc now sources the generated helper.")
	fmt.Fprintln(cmd.OutOrStdout(), styleInfo.Render("Next steps:"))
	fmt.Fprintf(cmd.OutOrStdout(), "  1. Source your shell config: %s\n", styleDim.Render(shellSourceHint(shell)))
	fmt.Fprintln(cmd.OutOrStdout(), "  2. Verify: echo $HYDRA_SHELL_HELPER")
	fmt.Fprintln(cmd.OutOrStdout(), "     Should output: 1")
	fmt.Fprintln(cmd.OutOrStdout(), "")
	fmt.Fprintln(cmd.OutOrStdout(), "Then you can use:")
	fmt.Fprintln(cmd.OutOrStdout(), "  hydra switch <worktree>")
	fmt.Fprintln(cmd.OutOrStdout(), "  hsw <worktree>")
	fmt.Fprintln(cmd.OutOrStdout(), "")

	return nil
}

func shellAssetPaths(shell string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	shellDir := filepath.Join(home, ".config", "hydra", "shell")
	helperName := fmt.Sprintf("hydra-shell.%s", shell)
	completionName := fmt.Sprintf("hydra-completion.%s", shell)
	return filepath.Join(shellDir, helperName), filepath.Join(shellDir, completionName), nil
}

func writeLoaderBlock(shell, helperPath string) error {
	configFile := getShellConfigFile(shell)
	existing, err := readShellConfig(configFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", configFile, err)
	}

	loader := renderLoaderBlock(shell, helperPath)
	newContent := existing
	if hasExistingInstallation(existing) {
		newContent = replaceInstallation(existing, loader)
	} else {
		if strings.TrimSpace(existing) != "" {
			newContent = existing + "\n"
		}
		newContent += loader + "\n"
	}

	return writeShellConfig(configFile, newContent)
}

func renderLoaderBlock(shell, helperPath string) string {
	switch shell {
	case "fish":
		return fmt.Sprintf(`%s
# Hydra shell helper loader
set -gx HYDRA_SHELL_HELPER 1
source %q
%s`, helperMarkerStart, helperPath, helperMarkerEnd)
	default:
		return fmt.Sprintf(`%s
# Hydra shell helper loader
export HYDRA_SHELL_HELPER=1
source %q
%s`, helperMarkerStart, helperPath, helperMarkerEnd)
	}
}

func renderShellHelper(shell, completionPath string) string {
	switch shell {
	case "fish":
		return fmt.Sprintf(`set -gx HYDRA_SHELL_HELPER 1

function hydra
    if test "$argv[1]" = "switch"
        set -l cleanup_output_file 0
        set -l output_file $HYDRA_SWITCH_OUTPUT_FILE
        if test -z "$output_file"
            set output_file (mktemp "TMPDIR=${TMPDIR:-/tmp} hydra-switch.XXXXXX")
            or return 1
            set cleanup_output_file 1
        end

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
    end

    command hydra $argv
end

alias hsw 'hydra switch'

if test -f %q
    source %q
end
`, completionPath, completionPath)
	default:
		return fmt.Sprintf(`export HYDRA_SHELL_HELPER=1

hydra() {
    if [ "$1" = "switch" ]; then
        local cleanup_output_file=0
        local output_file="$HYDRA_SWITCH_OUTPUT_FILE"
        if [ -z "$output_file" ]; then
            output_file=$(mktemp "${TMPDIR:-/tmp}/hydra-switch.XXXXXX") || return 1
            cleanup_output_file=1
        fi

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
    fi

    command hydra "$@"
}

alias hsw='hydra switch'

if [ -f %q ]; then
    source %q
fi
`, completionPath, completionPath)
	}
}

func renderCompletionScript(shell string) (string, error) {
	var buf strings.Builder
	switch shell {
	case "bash":
		if err := rootCmd.GenBashCompletion(&buf); err != nil {
			return "", err
		}
	case "zsh":
		if err := rootCmd.GenZshCompletion(&buf); err != nil {
			return "", err
		}
	case "fish":
		if err := rootCmd.GenFishCompletion(&buf, true); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
	return buf.String(), nil
}

func shellSourceHint(shell string) string {
	switch shell {
	case "zsh":
		return "source ~/.zshrc"
	case "fish":
		return "source ~/.config/fish/config.fish"
	default:
		return "source ~/.bashrc"
	}
}

func hasExistingInstallation(content string) bool {
	return strings.Contains(content, helperMarkerStart)
}

func replaceInstallation(existing, newContent string) string {
	startIdx := strings.Index(existing, helperMarkerStart)
	endIdx := strings.Index(existing, helperMarkerEnd)
	if startIdx == -1 || endIdx == -1 {
		return existing + "\n" + newContent + "\n"
	}
	before := existing[:startIdx]
	after := existing[endIdx+len(helperMarkerEnd):]
	return before + newContent + after
}

func getShellConfigFile(shell string) string { return shellConfigFile(shell) }

func readShellConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeShellConfig(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func shellConfigFile(shell string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	default:
		return filepath.Join(home, ".bashrc")
	}
}

func generateBashZshHelper() string { return renderShellHelper("bash", shellAssetPlaceholder("bash")) }

func generateFishHelper() string { return renderShellHelper("fish", shellAssetPlaceholder("fish")) }

func shellAssetPlaceholder(shell string) string {
	_, completionPath, err := shellAssetPaths(shell)
	if err != nil {
		return filepath.Join(os.TempDir(), "hydra-completion."+shell)
	}
	return completionPath
}
