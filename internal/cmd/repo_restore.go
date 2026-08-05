package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"

	"github.com/mssantosdev/hydra/internal/fanout"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var (
	restoreDryRun bool
	restoreJobs   int
)

var repoRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Clone every repository the manifest declares but disk is missing",
	Long: `Make disk match .hydra/config.yaml.

The manifest is the committable half of a workspace: it records each repository's alias,
group, remote and default branch. Restoring it on a new machine used to mean one
"repo add" per repository, read off the file by hand.

ADDITIVE ONLY. It clones what is missing and touches nothing that exists — it never
removes a repository absent from the manifest, never rewrites a remote, and never moves a
worktree. Anything on disk that disagrees with the manifest is reported as a warning, so
reconciling stays a decision a human makes rather than something this command performs.

The manifest records each repository's DEFAULT branch, not the set of worktrees you had
open, so this restores repositories and one worktree each. Reproducing a specific set of
worktrees is "hydra apply -", which consumes what "hydra list --output json" emits.`,
	Args: cobra.NoArgs,
	RunE: runRepoRestore,
}

func init() {
	repoCmd.AddCommand(repoRestoreCmd)
	repoRestoreCmd.Flags().BoolVar(&restoreDryRun, "dry-run", false,
		"Report what would be cloned without touching disk")
	repoRestoreCmd.Flags().IntVar(&restoreJobs, "jobs", 1,
		"Clone this many repositories at once")
}

type restoreRepoJSON struct {
	Group       string `json:"group"`
	Repo        string `json:"repo"`
	Remote      string `json:"remote"`
	Branch      string `json:"branch"`
	Disposition string `json:"disposition"`
	Error       string `json:"error,omitempty"`
}

type restoreJSON struct {
	DryRun  bool              `json:"dry_run"`
	Total   int               `json:"total"`
	Cloned  int               `json:"cloned"`
	Present int               `json:"present"`
	Failed  int               `json:"failed"`
	Repos   []restoreRepoJSON `json:"repos"`
}

func runRepoRestore(cmd *cobra.Command, _ []string) error {
	if cfg == nil || projectRoot == "" {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}
	if restoreJobs < 1 {
		return output.Errorf(output.CodeInternal, "--jobs must be at least 1").
			WithDetail("jobs", restoreJobs)
	}

	declared := declaredRepos(cfg)
	payload := restoreJSON{DryRun: restoreDryRun, Total: len(declared)}
	var warnings []string

	// Each repository is its own bare repo, so cloning them concurrently contends on
	// nothing — the per-bare serialisation that worktree creation needs does not apply
	// across repositories. This is the whole point of the command: 13 repositories took
	// eight minutes one `repo add` at a time.
	targets := make([]fanout.Target, 0, len(declared))
	for _, ref := range declared {
		targets = append(targets, fanout.Target{
			Group:    ref.Group,
			Repo:     ref.Alias,
			Branch:   ref.Branch,
			BareRepo: cfg.BarePath(projectRoot, ref.Alias),
		})
	}

	var mu sync.Mutex
	entries := map[string]restoreRepoJSON{}
	byKey := map[string]declaredRepo{}
	for i, tgt := range targets {
		byKey[tgt.Key()] = declared[i]
	}

	results := fanout.Run(context.Background(), targets, fanout.Config{
		Jobs:          restoreJobs,
		SerialPerRepo: true,
	}, func(_ context.Context, tgt fanout.Target) fanout.ItemResult {
		ref := byKey[tgt.Key()]
		entry, disposition, err := restoreOne(ref)

		mu.Lock()
		entries[tgt.Key()] = entry
		mu.Unlock()

		return fanout.ItemResult{Disposition: disposition, Reason: entry.Disposition, Err: err}
	})

	for _, result := range results {
		entry := entries[result.Target.Key()]
		payload.Repos = append(payload.Repos, entry)
		switch entry.Disposition {
		case "cloned", "would_clone":
			payload.Cloned++
		case "failed":
			payload.Failed++
		default:
			payload.Present++
		}
		if entry.Disposition == "skipped" && entry.Error != "" {
			warnings = append(warnings, fmt.Sprintf("%s/%s: %s",
				entry.Group, entry.Repo, entry.Error))
		}
	}

	summary := restoreSummary(payload)
	var restoreErr *output.Error
	outcome := output.OutcomeSuccess
	switch payload.Failed {
	case 0:
	case payload.Total:
		outcome = output.OutcomeFailure
		restoreErr = output.Errorf(output.CodeGitFailed,
			"every repository failed to clone").WithDetail("failed", payload.Failed)
	default:
		outcome = output.OutcomePartial
		restoreErr = output.Errorf(output.CodePartialFailure,
			"%d of %d repositories failed to clone", payload.Failed, payload.Total).
			WithDetail("failed", payload.Failed)
	}

	if emitErr := emitResult(cmd, output.Result{
		Outcome:  outcome,
		Summary:  summary,
		Data:     payload,
		Warnings: warnings,
		Err:      restoreErr,
		Next: []output.Next{{
			Argv: []string{"hydra", "apply", "-"},
			Why:  "the manifest carries repositories, not worktrees; feed it a captured `hydra list --output json` to restore those",
		}},
	}, func() { printRestoreText(payload, summary) }); emitErr != nil {
		return emitErr
	}
	if restoreErr != nil {
		return restoreErr
	}
	return nil
}

// declaredRepo is one repository the manifest declares.
type declaredRepo struct {
	Group  string
	Alias  string
	Remote string
	Branch string
}

// declaredRepos flattens the manifest into a stable order, so two runs report the same
// sequence and their envelopes can be diffed.
func declaredRepos(c *config.Config) []declaredRepo {
	var out []declaredRepo
	for group, repos := range c.Groups {
		for alias, repo := range repos {
			branch := repo.DefaultBranch
			if branch == "" {
				branch = "main"
			}
			out = append(out, declaredRepo{
				Group:  group,
				Alias:  alias,
				Remote: repo.Remote,
				Branch: branch,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Alias < out[j].Alias
	})
	return out
}

// barePresent reports whether the bare repository already exists, which is what makes this
// command additive: a present bare is left entirely alone.
func barePresent(c *config.Config, root, alias string) bool {
	info, err := os.Stat(c.BarePath(root, alias))
	return err == nil && info.IsDir()
}

func restoreSummary(p restoreJSON) string {
	verb := "cloned"
	if p.DryRun {
		verb = "would clone"
	}
	if p.Failed > 0 {
		return fmt.Sprintf("%s %d, %d already present, %d failed", verb, p.Cloned, p.Present, p.Failed)
	}
	return fmt.Sprintf("%s %d, %d already present", verb, p.Cloned, p.Present)
}

func printRestoreText(p restoreJSON, summary string) {
	fmt.Println()
	fmt.Println(styles.Label.Render(summary))
	for _, r := range p.Repos {
		line := fmt.Sprintf("  %-10s %s/%s", r.Disposition, r.Group, r.Repo)
		if r.Error != "" {
			line += "  " + r.Error
		}
		fmt.Println(line)
	}
	fmt.Println()
}

// restoreOne decides and performs the action for one declared repository.
func restoreOne(ref declaredRepo) (restoreRepoJSON, fanout.Disposition, error) {
	entry := restoreRepoJSON{
		Group:  ref.Group,
		Repo:   ref.Alias,
		Remote: ref.Remote,
		Branch: ref.Branch,
	}

	switch {
	case ref.Remote == "":
		// A repository with no remote cannot be cloned. That is a manifest problem, not
		// a failure of this run, so it becomes a warning and the rest proceeds.
		entry.Disposition = "skipped"
		entry.Error = "no remote recorded in the manifest"
		return entry, fanout.Skipped, nil

	case barePresent(cfg, projectRoot, ref.Alias):
		// Additive: a present bare repository is left entirely alone.
		entry.Disposition = "present"
		return entry, fanout.Skipped, nil

	case restoreDryRun:
		entry.Disposition = "would_clone"
		return entry, fanout.Created, nil
	}

	opts := &CloneOptions{
		URL:      ref.Remote,
		Alias:    ref.Alias,
		Group:    ref.Group,
		Branches: []string{ref.Branch},
	}
	if _, _, err := performClone(opts, cfg, projectConfigPath, projectRoot); err != nil {
		entry.Disposition = "failed"
		entry.Error = output.Classify(err).Message
		return entry, fanout.Failed, err
	}
	entry.Disposition = "cloned"
	return entry, fanout.Created, nil
}
