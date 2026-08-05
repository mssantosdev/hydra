package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var repoBranchesCmd = &cobra.Command{
	Use:   "branches <url|alias>",
	Short: "List a remote's branches without cloning",
	Long: `List the branches a remote has, so --branches can be chosen rather than guessed.

"repo add --branches prod,stage" needs the branch names up front, and there was no way to
learn them from hydra: --dry-run reports the plan but performs no network I/O, so a caller
had to shell out to "git ls-remote" and parse refs itself.

Takes either a remote URL, or the alias of a repository already in this workspace.`,
	Args:              cobra.ExactArgs(1),
	RunE:              runRepoBranches,
	ValidArgsFunction: completeRepoAliases,
}

func init() {
	repoCmd.AddCommand(repoBranchesCmd)
}

type repoBranchesJSON struct {
	Remote string `json:"remote"`
	// Source is always "remote" here — the point of this command is that it asks. It is
	// reported anyway so the field means the same thing everywhere it appears.
	Source   string   `json:"branches_source"`
	Branches []string `json:"branches"`
	Default  string   `json:"default_branch,omitempty"`
}

func runRepoBranches(cmd *cobra.Command, args []string) error {
	remote := strings.TrimSpace(args[0])
	if remote == "" {
		return output.Errorf(output.CodeNeedsInput, "a remote URL or a registered alias is required").
			WithDetail("missing", []string{"<url|alias>"})
	}

	// An alias resolves to its recorded remote, so a caller inside a workspace does not
	// have to repeat a URL hydra already knows.
	if cfg != nil {
		if ref, ok := cfg.FindRepo(remote); ok {
			if resolved := cfg.Groups[ref.Group][ref.Alias].Remote; resolved != "" {
				remote = resolved
			}
		}
	}

	found, err := git.FetchRemoteBranches(remote)
	if err != nil {
		return output.Wrap(output.CodeGitFailed, err,
			"failed to list branches on %q", remote).
			WithDetail("remote", remote)
	}

	payload := repoBranchesJSON{Remote: remote, Source: "remote", Branches: []string{}}
	for _, b := range found {
		payload.Branches = append(payload.Branches, b.Name)
		if b.IsDefault && payload.Default == "" {
			payload.Default = b.Name
		}
	}
	sort.Strings(payload.Branches)

	summary := fmt.Sprintf("%d branch(es) on %s", len(payload.Branches), remote)
	return emitResult(cmd, output.Result{
		Summary: summary,
		Data:    payload,
		Next: []output.Next{{
			Argv: []string{"hydra", "repo", "add", remote, "--branches", "<pick from branches>"},
			Why:  "create a worktree for each branch you name",
		}},
	}, func() {
		fmt.Println()
		fmt.Println(styles.Label.Render(summary))
		for _, name := range payload.Branches {
			marker := "  "
			if name == payload.Default {
				marker = "* "
			}
			fmt.Println(marker + name)
		}
		fmt.Println()
	})
}
