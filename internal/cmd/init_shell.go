package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

const (
	helperMarkerStart = "# === HYDRA SHELL HELPER START ==="
	helperMarkerEnd   = "# === HYDRA SHELL HELPER END ==="
)

var (
	withCompletion    bool
	withoutCompletion bool
	installFlag       = true
	printFlag         bool
)

var initShellCmd = &cobra.Command{
	Use:   "init-shell [bash|zsh|fish]",
	Short: "Install shell integration",
	Long: `Install shell helper for automatic directory switching.

DESCRIPTION
  Generates shell helper files under ~/.config/hydra/shell/ and, by default,
  installs a small loader block into your shell configuration file.

FLAGS
  --install           Install the loader block into your shell rc (default: true)
  --install=false     Skip rc installation
  --print             Print the loader block to stdout (same as --install=false)
  --with-completion   Also install shell completion alongside the helper
  --without-completion Install only the shell helper

NOTES
  Do not redirect the default install command into your shell rc: it writes the
  rc file for you and prints a human summary to stdout. Use ` + "`--print`" + `
  or ` + "`--install=false`" + ` when you want only the raw loader snippet.

EXIT CODES
  0  Success (helper installed or loader printed)
  1  General error (unsupported shell, write failure)`,
	RunE: runInitShell,
}

func init() {
	rootCmd.AddCommand(initShellCmd)
	initShellCmd.Flags().BoolVar(&installFlag, "install", true, "Install loader block into shell config")
	initShellCmd.Flags().BoolVar(&printFlag, "print", false, "Print loader block to stdout instead of installing")
	initShellCmd.Flags().BoolVar(&withCompletion, "with-completion", false, "Install completion alongside the shell helper")
	initShellCmd.Flags().BoolVar(&withoutCompletion, "without-completion", false, "Install only the shell helper")
}

func runInitShell(cmd *cobra.Command, args []string) error {
	if withCompletion && withoutCompletion {
		return output.Errorf(output.CodeUsage,
			"--with-completion and --without-completion are mutually exclusive")
	}

	shell := detectShell()
	if len(args) > 0 {
		shell = args[0]
	}
	if shell != "bash" && shell != "zsh" && shell != "fish" {
		return output.Errorf(output.CodeUsage,
			"unsupported shell: %s (supported: bash, zsh, fish)", shell)
	}

	installCompletion := withCompletion
	if !withCompletion && !withoutCompletion {
		if shouldPromptCompletion(cmd) {
			installCompletion = promptInstallCompletion(cmd, shell)
		} else {
			installCompletion = true
		}
	}

	helperPath, completionPath, err := shellAssetPaths(shell)
	if err != nil {
		return err
	}

	if err := writeShellHelperFiles(shell, helperPath, completionPath, installCompletion); err != nil {
		return err
	}

	loader := renderLoaderBlock(shell, helperPath)

	if printFlag || !installFlag {
		return emit(cmd, fmt.Sprintf("shell helper for %s", shell), map[string]any{
			"shell":  shell,
			"loader": loader,
			"helper": helperPath,
		}, nil, func() {
			_, _ = fmt.Fprint(cmd.OutOrStdout(), loader)
			if !strings.HasSuffix(loader, "\n") {
				_, _ = fmt.Fprintln(cmd.OutOrStdout())
			}
		})
	}

	if err := writeLoaderBlock(shell, helperPath); err != nil {
		return err
	}

	return renderInitShellSummary(cmd, shell, helperPath, completionPath, installCompletion)
}

func shouldPromptCompletion(cmd *cobra.Command) bool {
	if interactive() {
		return true
	}
	if cmd != nil && cmd.InOrStdin() != os.Stdin {
		return true
	}
	return false
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

func writeShellHelperFiles(shell, helperPath, completionPath string, installCompletion bool) error {
	if err := os.MkdirAll(filepath.Dir(helperPath), 0o750); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to create shell asset directory")
	}

	helperContent := renderShellHelper(shell, completionPath)
	//nolint:gosec // G306: a shell helper script must be readable by the shell
	if err := os.WriteFile(helperPath, []byte(helperContent), 0o644); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to write shell helper")
	}

	if installCompletion {
		completionScript, err := renderCompletionScript(shell)
		if err != nil {
			return err
		}
		//nolint:gosec // G306: a shell helper script must be readable by the shell
		if err := os.WriteFile(completionPath, []byte(completionScript), 0o644); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to write completion script")
		}
	} else {
		_ = os.Remove(completionPath)
	}

	return nil
}

func renderInitShellSummary(cmd *cobra.Command, shell, helperPath, completionPath string, installCompletion bool) error {
	payload := map[string]any{
		"shell":      shell,
		"helper":     helperPath,
		"installed":  true,
		"completion": installCompletion,
	}
	if installCompletion {
		payload["completion_path"] = completionPath
	}

	return emit(cmd, fmt.Sprintf("shell helper installed for %s", shell), payload, nil, func() {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), styles.Success.Render(fmt.Sprintf("✓ Shell helper installed for %s", shell)))
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Helper: %s\n", helperPath)
		if installCompletion {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Completion: %s\n", completionPath)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "The loader block in your shell rc now sources the generated helper.")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), styles.Title.Render("Next steps:"))
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  1. Source your shell config: %s\n", styles.Dimmed.Render(shellSourceHint(shell)))
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  2. Verify: echo $HYDRA_SHELL_HELPER")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "     Should output: 1")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Then you can use:")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  hydra switch <worktree>")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  hsw <worktree>")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	})
}

func shellAssetPaths(shell string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", output.Wrap(output.CodeInternal, err, "failed to resolve home directory")
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
		return output.Wrap(output.CodeInternal, err, "failed to read %s", configFile)
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
            set -l tmpdir $TMPDIR
            if test -z "$tmpdir"
                set tmpdir /tmp
            end
            set output_file (mktemp "$tmpdir/hydra-switch.XXXXXX")
            or return 1
            set cleanup_output_file 1
        end

        set -lx HYDRA_SWITCH_OUTPUT_FILE $output_file
        command hydra $argv
        set -l exit_code $status
        set -e HYDRA_SWITCH_OUTPUT_FILE

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
		return "", output.Errorf(output.CodeUsage, "unsupported shell: %s", shell)
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
	//nolint:gosec // G304: reads the shell rc file the user chose to install into
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeShellConfig(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	//nolint:gosec // G306: the generated completion script must be readable by the shell
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

func generateFishHelper() string { return renderShellHelper("fish", shellAssetPlaceholder("fish")) }

func shellAssetPlaceholder(shell string) string {
	_, completionPath, err := shellAssetPaths(shell)
	if err != nil {
		return filepath.Join(os.TempDir(), "hydra-completion."+shell)
	}
	return completionPath
}
