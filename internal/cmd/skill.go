package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/skill"
)

var (
	skillInstall bool
	skillDir     string
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Emit hydra's agent skill",
	Long: `Emit the agent skill that travels with this binary.

DESCRIPTION
  LLMs have no training data on hydra, so the CLI ships an explicit agent contract:
  the workspace model, the rules an agent must follow, the command table, the JSON
  envelope, and the full error-code to exit-code mapping.

  The skill is embedded in the binary, so it can never describe a different version
  than the hydra that emits it. A test asserts it matches the real command tree and
  the real error-code enum, so it cannot silently rot.

WHEN TO USE
  • Onboarding an AI agent to a repository managed by hydra
  • Refreshing an installed skill after upgrading hydra
  • Reading the machine contract without digging through --help

EXAMPLES
  # Print the skill
  $ hydra skill

  # Install it into the current workspace
  $ hydra skill --install

  # Install it somewhere else
  $ hydra skill --install --dir path/to/skills

FLAGS
  --install       write the skill to <dir>/hydra/SKILL.md instead of stdout
  --dir <path>    install directory (default .agents/skills)

NOTES
  Installing overwrites any existing copy: the embedded skill is authoritative.
  This command works outside a hydra workspace, which is precisely where an agent
  installs it.

EXIT CODES
  0  Success
  1  General error (could not write the skill)

SEE ALSO
  • hydra glossary   - hydra's vocabulary for humans
  • hydra completion - shell completion scripts`,
	Args: cobra.NoArgs,
	RunE: runSkill,
}

func init() {
	skillCmd.Flags().BoolVar(&skillInstall, "install", false, "write the skill into a workspace instead of stdout")
	skillCmd.Flags().StringVar(&skillDir, "dir", skill.DefaultInstallDir, "install directory")
	rootCmd.AddCommand(skillCmd)
}

func runSkill(cmd *cobra.Command, args []string) error {
	if !skillInstall {
		// The skill is the payload, not a report: it goes to stdout verbatim in
		// every mode, so `hydra skill > SKILL.md` works and `hydra skill --output
		// json` never mangles the markdown.
		_, _ = fmt.Fprint(cmd.OutOrStdout(), skill.Content())
		return nil
	}

	path, err := skill.Install(skillDir)
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to install the hydra skill")
	}

	return emit(cmd, fmt.Sprintf("skill %q installed at %s", skill.Name, path), map[string]any{"path": path, "skill": skill.Name}, nil, func() {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), path)
	})
}
