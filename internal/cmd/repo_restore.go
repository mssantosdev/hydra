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
	DryRun  bool `json:"dry_run"`
	Total   int  `json:"total"`
	Cloned  int  `json:"cloned"`
	Present int  `json:"present"`
	Failed  int  `json:"failed"`

	// Declared is how many worktrees the manifest asked for across every repo, so a caller
	// can tell "one per repo because that is all the manifest knew" from "this is the
	// declared shape" without re-reading the manifest itself.
	Declared int               `json:"declared"`
	Repos    []restoreRepoJSON `json:"repos"`
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
	payload := restoreJSON{DryRun: restoreDryRun, Total: len(declared), Declared: declaredWorktrees(declared)}
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
		Next:     restoreNext(declared),
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

	// Branches is the repo's declared shape when the manifest carries one. It is why this
	// command can now rebuild a workspace on its own: before, only the default branch was
	// recoverable and the caller was pointed at a captured `hydra list --output json` for
	// everything else — which also dragged that machine's topic membership along.
	Branches []string
}

// declaredRepos flattens the manifest into a stable order, so two runs report the same
// sequence and their envelopes can be diffed.
func declaredRepos(c *config.Config) []declaredRepo {
	var out []declaredRepo
	for group, repos := range c.Groups {
		for alias, repo := range repos {
			// Do NOT guess "main". A manifest entry can legitimately lack a default
			// branch, and a repository whose branches are prod/stage then failed with
			// `branch "main" does not exist on origin` — a guess presented as the
			// user's own configuration. Empty means "let the clone resolve the
			// remote's HEAD", which is the only defensible answer.
			branch := repo.DefaultBranch
			out = append(out, declaredRepo{
				Group:    group,
				Alias:    alias,
				Remote:   repo.Remote,
				Branch:   branch,
				Branches: repo.Branches,
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
	// Say what was actually restored. A manifest that declares `branches:` per repo IS the
	// complete shape, so claiming "default-branch worktrees only" would understate it the
	// same way reporting the repository count alone once overstated it. A manifest without
	// declared branches still gets one worktree per repo, and still says so.
	if p.Declared > 0 {
		return fmt.Sprintf("%s %d, %d already present; %d declared worktree(s)", verb, p.Cloned, p.Present, p.Declared)
	}
	return fmt.Sprintf("%s %d, %d already present; default-branch worktrees only", verb, p.Cloned, p.Present)
}

// restoreNext points at `apply -` only when the manifest cannot describe the shape itself.
// Suggesting it unconditionally told the caller to go find a captured `hydra list` even when
// the manifest had just produced the complete set — and that capture also carries the source
// machine's topic membership, which a structural restore has no business adopting.
func restoreNext(declared []declaredRepo) []output.Next {
	for _, ref := range declared {
		if len(ref.Branches) > 0 {
			return nil
		}
	}
	return []output.Next{{
		Argv: []string{"hydra", "apply", "-"},
		Why:  "this manifest declares no `branches:`, so only default branches were restored; feed a captured `hydra list --output json` for the rest",
	}}
}

// declaredWorktrees counts the worktrees the manifest EXPLICITLY asks for. A repo with no
// `branches:` contributes zero, not one: restore has always created its default branch, and
// counting that as a declaration would make every legacy manifest claim a shape it does not
// have — which is the difference between "this is the whole workspace" and "this is all the
// manifest knew".
func declaredWorktrees(declared []declaredRepo) int {
	n := 0
	for _, ref := range declared {
		n += len(ref.Branches)
	}
	return n
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
		URL:   ref.Remote,
		Alias: ref.Alias,
		Group: ref.Group,
	}
	// Prefer the declared shape when the manifest carries one: that is the whole point of
	// recording it, and without it a restored workspace is missing every long-lived branch
	// the team keeps checked out. Falling back to the single default branch — and to empty,
	// which lets the clone resolve the remote's HEAD — keeps manifests written before
	// `branches:` existed working unchanged.
	switch {
	case len(ref.Branches) > 0:
		opts.Branches = ref.Branches
	case ref.Branch != "":
		opts.Branches = []string{ref.Branch}
	}
	if _, _, err := performClone(opts, cfg, projectConfigPath, projectRoot); err != nil {
		entry.Disposition = "failed"
		entry.Error = output.Classify(err).Message
		return entry, fanout.Failed, err
	}
	entry.Disposition = "cloned"
	return entry, fanout.Created, nil
}
