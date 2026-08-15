package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/fanout"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/hooks"
	"github.com/mssantosdev/hydra/internal/log"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var (
	syncAll   bool
	syncYes   bool
	syncForce bool
	// syncDirty is the non-interactive equivalent of the dirty-policy prompt. --force
	// remains as a shorthand for --dirty stash: it already ships and reads naturally,
	// but it adds no capability this does not have.
	syncDirty string
)

type syncEntry struct {
	group, repo, name, branch, path, barePath string
	upstream                                  string
	ahead, behind, changes                    int
	dirty, selected                           bool
	pullAction                                string
}

type syncWorktreeJSON struct {
	Group    string `json:"group"`
	Repo     string `json:"repo"`
	Name     string `json:"name"`
	Branch   string `json:"branch"`
	Path     string `json:"path"`
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Dirty    bool   `json:"dirty"`
	Status   string `json:"status"`
	Pulled   bool   `json:"pulled"`
}

type syncSummaryJSON struct {
	Total     int `json:"total"`
	Pulled    int `json:"pulled"`
	Skipped   int `json:"skipped"`
	LocalOnly int `json:"local_only"`
	Failed    int `json:"failed"`
}

type syncJSON struct {
	Project   string             `json:"project"`
	Root      string             `json:"root"`
	Worktrees []syncWorktreeJSON `json:"worktrees"`
	Summary   syncSummaryJSON    `json:"summary"`
}

type syncOpResult struct {
	entry  syncEntry
	status string
	pulled bool
	err    error
}

var syncCmd = &cobra.Command{Args: cobra.MaximumNArgs(1)}

func init() {
	syncCmd.Use = "sync [alias]"
	syncCmd.Short = "Pull latest changes for worktrees"
	syncCmd.Long = `Check remote for updates and pull changes to selected worktrees.

DESCRIPTION
  Fetches remote updates and pulls them into local worktrees.
  Handles dirty worktrees by stashing changes (with confirmation).

  By default:
    - Inside a worktree: syncs current repository
    - Outside: prompts to select repositories

  Worktrees with no upstream are reported as local-only and are never pulled.

FLAGS
  --all, -a     Sync all repositories (not just current)
  --yes, -y     Skip confirmation, pull all clean worktrees
  --force, -f   Force pull dirty worktrees (stash, pull, restore)

EXAMPLES
  # Sync current repository
  $ hydra sync

  # Sync all repositories without prompts
  $ hydra sync --all --yes

  # Force pull dirty worktrees (stash changes)
  $ hydra sync --force

EXIT CODES
  0  Success
  1  git_failed
  1  hook_failed
  2  not_in_project
  4  partial_failure
  5  worktree_dirty

SEE ALSO
  - hydra status - Check worktree status
  - hydra list - List all worktrees`
	syncCmd.RunE = runSync
	syncCmd.ValidArgsFunction = completeRepoAliases
	rootCmd.AddCommand(syncCmd)
	syncCmd.Flags().BoolVarP(&syncAll, "all", "a", false, "Sync all repositories")
	syncCmd.Flags().BoolVarP(&syncYes, "yes", "y", false, "Skip confirmation, pull all clean worktrees")
	syncCmd.Flags().BoolVarP(&syncForce, "force", "f", false, "Pull dirty worktrees (same as --dirty stash)")
	syncCmd.Flags().StringVar(&syncDirty, "dirty", "",
		"Policy for worktrees with uncommitted changes: stash, reset, or skip")
	_ = syncCmd.RegisterFlagCompletionFunc("dirty",
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return dirtyPolicies, cobra.ShellCompDirectiveNoFileComp
		})
}

func runSync(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject,
			`no hydra workspace found; run "hydra init" or pass --project <name>`)
	}

	// Validate --dirty BEFORE any work. A typo'd flag value must be reported even when
	// there turns out to be nothing to sync: reaching the "no worktrees to sync" exit
	// with an invalid flag reports success for a command the user got wrong.
	if syncDirty != "" {
		if _, err := parseDirtyPolicy(syncDirty); err != nil {
			return err
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to resolve the working directory")
	}
	var targetAlias string
	if len(args) > 0 {
		targetAlias = args[0]
	} else if !syncAll {
		targetAlias = detectSyncAlias(wd)
	}
	entries, collectWarnings := gatherSyncEntries(cfg, projectRoot, targetAlias)
	if len(entries) == 0 {
		data := syncJSON{Project: cfg.Project, Root: projectRoot, Worktrees: []syncWorktreeJSON{}, Summary: syncSummaryJSON{}}
		return emit(cmd, "no worktrees to sync", data, collectWarnings, func() { log.Info("No worktrees found to sync") })
	}
	// Fetch failures become warnings and the run continues, so an unreachable remote is
	// still reported without blocking pulls for repos that can be reached.
	collectWarnings = append(collectWarnings, fetchSyncRepos(entries)...)
	entries, enrichWarnings := enrichSyncEntries(entries)
	collectWarnings = append(collectWarnings, enrichWarnings...)
	candidates := filterWithUpdates(entries)
	if len(candidates) == 0 {
		// "Nothing to pull" must still surface fetch failures from unreachable remotes.
		// This path returns early, so fault-coded warnings drive a partial outcome here too.
		results := buildIdleResults(entries)
		data, _ := buildSyncOutput(cfg.Project, projectRoot, results)
		var idleErr *output.Error
		if output.HasFault(collectWarnings) {
			idleErr = output.Errorf(output.CodePartialFailure,
				"%d repo(s) could not be fetched", countFaults(collectWarnings)).
				WithDetail("warnings", collectWarnings)
		}
		if emitErr := emitResult(cmd, output.Result{
			Summary:  syncSummaryLine(data.Summary),
			Data:     data,
			Warnings: collectWarnings,
			Err:      idleErr,
		}, func() { printSyncText(results, data.Summary) }); emitErr != nil {
			return emitErr
		}
		if idleErr != nil {
			return idleErr
		}
		return nil
	}
	if !machineMode() {
		log.Info(fmt.Sprintf("Found %d worktree(s) with available updates", len(candidates)))
	}
	var selected []syncEntry
	switch {
	case syncYes:
		selected = autoSelectYes(candidates)
	case interactive():
		selected = selectWorktreesToSync(candidates)
	default:
		selected = autoSelectYes(candidates)
	}
	// Resolve the dirty policy BEFORE filtering to selected entries.
	//
	// selectedSyncEntries drops unselected worktrees, and autoSelectYes leaves dirty
	// worktrees unselected when no policy is set. Filtering first would return
	// "no worktrees selected" without naming the dirty worktrees or asking for --dirty.
	if syncForce {
		applyForceDirty(selected)
	} else {
		withPolicy, dirtyErr := handleDirtyWorktrees(selected)
		if dirtyErr != nil {
			return dirtyErr
		}
		selected = withPolicy
	}
	selected = selectedSyncEntries(selected)
	if len(selected) == 0 {
		results := mergeSyncResults(entries, nil, nil)
		data, _ := buildSyncOutput(cfg.Project, projectRoot, results)
		return emit(cmd, "no worktrees selected", data, collectWarnings, func() { log.Info("No worktrees selected for sync") })
	}
	ops, hookWarnings := executeSync(selected)
	allWarnings := append(collectWarnings, hookWarnings...)
	results := mergeSyncResults(entries, selected, ops)
	data, summary := buildSyncOutput(cfg.Project, projectRoot, results)
	// A partial failure is reported on the SUCCESS envelope as outcome=partial: the
	// pulls that landed are real data, and a caller reading stdout must be able to
	// see both what worked and that something did not.
	// Hook failures are warnings only; syncErr below reflects git pull results.
	failed := failedWorktreeDetails(results)
	var syncErr *output.Error
	outcome := output.OutcomeSuccess
	switch {
	case len(failed) == 0:
	case summary.Total > 0 && len(failed) == summary.Total:
		outcome = output.OutcomeFailure
		syncErr = output.Errorf(output.CodeGitFailed,
			"every worktree failed to sync").WithDetail("worktrees", failed)
	default:
		outcome = output.OutcomePartial
		syncErr = output.Errorf(output.CodePartialFailure,
			"%d worktree(s) failed to sync", len(failed)).WithDetail("worktrees", failed)
	}

	// A repository that could not be fetched is a fault even when every worktree that WAS
	// reachable pulled cleanly. Without this, sync reported `outcome: partial` — the
	// envelope corrects that much on its own — while exiting 0, which is the same
	// contradiction inverted: a caller gating on the exit status saw nothing wrong.
	if syncErr == nil && output.HasFault(allWarnings) {
		syncErr = output.Errorf(output.CodePartialFailure,
			"%d repo(s) could not be fetched", countFaults(allWarnings)).
			WithDetail("warnings", allWarnings)
	}

	if err := emitResult(cmd, output.Result{
		Outcome:  outcome,
		Summary:  syncSummaryLine(summary),
		Data:     data,
		Warnings: allWarnings,
		Err:      syncErr,
	}, func() { printSyncText(results, summary) }); err != nil {
		return err
	}
	if syncErr != nil {
		return syncErr
	}
	return nil
}

// syncSummaryLine reports only the non-zero states, so "everything is current" is a
// short sentence rather than five zeroes a reader has to scan.
func syncSummaryLine(summary syncSummaryJSON) string {
	var parts []string
	if summary.Pulled > 0 {
		parts = append(parts, fmt.Sprintf("%d pulled", summary.Pulled))
	}
	if summary.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", summary.Failed))
	}
	if summary.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", summary.Skipped))
	}
	if summary.LocalOnly > 0 {
		parts = append(parts, fmt.Sprintf("%d local-only", summary.LocalOnly))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d worktree(s) already up to date", summary.Total)
	}
	return strings.Join(parts, ", ")
}

func detectSyncAlias(wd string) string {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(wd))
	if err != nil {
		resolved = filepath.Clean(wd)
	}
	items, _ := gatherSyncEntries(cfg, projectRoot, "")
	var best *syncEntry
	for i := range items {
		if !isPathInside(resolved, items[i].path) {
			continue
		}
		if best == nil || len(items[i].path) > len(best.path) {
			copy := items[i]
			best = &copy
		}
	}
	if best == nil {
		return ""
	}
	return best.repo
}

func gatherSyncEntries(projectCfg *config.Config, root, targetAlias string) ([]syncEntry, []*output.Diagnostic) {
	var entries []syncEntry
	var warnings []*output.Diagnostic
	for _, ref := range projectCfg.Repos() {
		if targetAlias != "" && ref.Alias != targetAlias {
			continue
		}
		bare := projectCfg.BarePath(root, ref.Alias)
		if _, err := os.Stat(bare); err != nil {
			warnings = append(warnings, output.Warnf(output.CodeBareMissing, "%s/%s: bare repository missing at %s", ref.Group, ref.Alias, bare).
				WithSubject("repo", ref.Group+"/"+ref.Alias))
			continue
		}
		wtList, err := git.ListWorktrees(bare)
		if err != nil {
			warnings = append(warnings, output.Warnf(output.CodeGitFailed, "%s/%s: %v", ref.Group, ref.Alias, err).
				WithSubject("repo", ref.Group+"/"+ref.Alias).
				WithCause(err.Error()))
			continue
		}
		for _, wt := range wtList {
			if wt.IsBare {
				continue
			}
			entries = append(entries, syncEntry{
				group: ref.Group, repo: ref.Alias, name: filepath.Base(wt.Path),
				branch: wt.Branch, path: wt.Path, barePath: bare,
			})
		}
	}
	return entries, warnings
}

// fetchSyncRepos pre-fetches every bare repository and reports the ones that failed,
// rather than aborting on the first.
//
// Matches `run`'s partial-failure model: every repo is attempted and failures accumulate
// as warnings rather than stopping the command with no summary.
//
// A repository that cannot be fetched is still handed to the pull stage: it may be
// fast-forwardable from refs already on disk, and if it is not, the failure is reported per
// worktree where a caller can see which one it was.
func fetchSyncRepos(entries []syncEntry) []*output.Diagnostic {
	seen := make(map[string]struct{})
	var failures []*output.Diagnostic
	for _, entry := range entries {
		if _, ok := seen[entry.barePath]; ok {
			continue
		}
		seen[entry.barePath] = struct{}{}
		if err := git.FetchBareRepo(entry.barePath); err != nil {
			failures = append(failures, output.Warnf(output.CodeGitFailed, "%s: failed to fetch: %v", entry.repo, err).
				WithSubject("repo", entry.repo).
				WithCause(err.Error()))
		}
	}
	return failures
}

func enrichSyncEntries(entries []syncEntry) ([]syncEntry, []*output.Diagnostic) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var warnings []*output.Diagnostic
	sem := make(chan struct{}, 5)
	for i := range entries {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			status, err := git.CheckWorktreeStatus(entries[idx].path)
			if err != nil {
				mu.Lock()
				warnings = append(warnings, output.Warnf(output.CodeGitFailed, "%s/%s: %v", entries[idx].group, entries[idx].name, err).
					WithCause(err.Error()))
				mu.Unlock()
				return
			}
			entries[idx].upstream = status.Upstream
			entries[idx].ahead = status.CommitsAhead
			entries[idx].behind = status.CommitsBehind
			entries[idx].dirty = status.HasChanges
			entries[idx].changes = status.ChangeCount
		}(i)
	}
	wg.Wait()
	return entries, warnings
}

func isLocalOnlyEntry(entry syncEntry) bool { return entry.upstream == "" }

func filterWithUpdates(entries []syncEntry) []syncEntry {
	var result []syncEntry
	for _, entry := range entries {
		if isLocalOnlyEntry(entry) {
			continue
		}
		if entry.behind > 0 {
			result = append(result, entry)
		}
	}
	return result
}

func syncEntryKey(entry syncEntry) string { return entry.group + "/" + entry.name }

// autoSelectYes selects everything --yes can act on without asking.
//
// A dirty worktree is selected only when a POLICY exists for it — --force or
// --dirty. Without one it stays unselected so handleDirtyWorktrees can return
// needs_input naming --dirty instead of skipping the worktree without explanation.
func autoSelectYes(candidates []syncEntry) []syncEntry {
	havePolicy := syncForce || syncDirty != ""

	out := make([]syncEntry, len(candidates))
	copy(out, candidates)
	for i := range out {
		if !out[i].dirty {
			out[i].selected = true
			continue
		}
		if !havePolicy {
			continue
		}
		out[i].selected = true
		if syncForce {
			// --force is exactly --dirty stash; the explicit flag is applied later by
			// handleDirtyWorktrees, so only the shorthand needs resolving here.
			out[i].pullAction = "stash"
		}
	}
	return out
}

func applyForceDirty(selected []syncEntry) {
	for i := range selected {
		if selected[i].dirty {
			selected[i].pullAction = "stash"
		}
	}
}

func selectWorktreesToSync(worktrees []syncEntry) []syncEntry {
	// The header belongs on the same stream as the prompt it introduces. It used to go to stdout
	// while the form drew on stderr, so redirecting either one split a single question in half.
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	promptf("\n%s\n\n", styles.Title.Render("Worktrees with Available Updates"))
	promptf("  %s  %-15s %-15s %-8s %-12s\n", headerStyle.Render("Select"), headerStyle.Render("Repository"), headerStyle.Render("Branch"), headerStyle.Render("Behind"), headerStyle.Render("Status"))
	promptf("%s\n", strings.Repeat("-", 70))
	var defaultSelected []string
	for _, wt := range worktrees {
		if !wt.dirty {
			defaultSelected = append(defaultSelected, syncEntryKey(wt))
		}
	}
	var options []huh.Option[string]
	for _, wt := range worktrees {
		label := fmt.Sprintf("%-15s %-15s %-8d", wt.repo, wt.branch, wt.behind)
		if wt.dirty {
			label += fmt.Sprintf(" ! %d changes", wt.changes)
		} else {
			label += " clean"
		}
		options = append(options, huh.NewOption(label, syncEntryKey(wt)))
	}
	selected := defaultSelected
	form := huh.NewForm(huh.NewGroup(huh.NewMultiSelect[string]().Title("Select worktrees to update").Description("Clean worktrees are pre-selected. Dirty worktrees require special handling.").Options(options...).Value(&selected)))
	if err := runForm(form); err != nil {
		return nil
	}
	selectedMap := make(map[string]bool, len(selected))
	for _, key := range selected {
		selectedMap[key] = true
	}
	var result []syncEntry
	for _, wt := range worktrees {
		item := wt
		if selectedMap[syncEntryKey(wt)] {
			item.selected = true
		}
		result = append(result, item)
	}
	return result
}

// dirtyPolicies is the closed value set for --dirty, so it can be completed and
// validated rather than guessed.
var dirtyPolicies = []string{"stash", "reset", "skip"}

// handleDirtyWorktrees decides what to do about uncommitted changes.
//
// With --dirty, apply that policy. In an interactive terminal, prompt per dirty
// worktree. Otherwise return needs_input listing the dirty worktrees and the valid
// --dirty values — including under --output json where no prompt can be shown.
func handleDirtyWorktrees(worktrees []syncEntry) ([]syncEntry, error) {
	if syncDirty != "" {
		policy, err := parseDirtyPolicy(syncDirty)
		if err != nil {
			return nil, err
		}
		return applyDirtyPolicy(worktrees, policy), nil
	}

	// Look at dirty CANDIDATES, not dirty-and-selected. With no policy autoSelectYes
	// deliberately leaves them unselected, so keying off `selected` here would find
	// nothing and skip them without reporting needs_input.
	if dirty := dirtyCandidates(worktrees); len(dirty) > 0 && !interactive() {
		return nil, output.Errorf(output.CodeNeedsInput,
			"%d worktree(s) have uncommitted changes; choose a policy", len(dirty)).
			WithDetail("missing", []string{"--dirty"}).
			WithDetail("one_of", dirtyPolicies).
			WithDetail("worktrees", dirty)
	}

	var result []syncEntry
	for _, wt := range worktrees {
		if !wt.dirty || !wt.selected {
			result = append(result, wt)
			continue
		}
		fmt.Println()
		fmt.Printf("Worktree '%s/%s' has %d uncommitted changes.\n\n", wt.repo, wt.branch, wt.changes)
		var action string
		form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("How would you like to proceed?").Options(
			huh.NewOption("Stash changes, pull, then restore", "stash"),
			huh.NewOption("Discard all changes (reset --hard)", "reset"),
			huh.NewOption("Skip this worktree", "skip"),
		).Value(&action)))
		if err := runForm(form); err != nil {
			// A cancelled prompt means "not this one", which is the safe reading.
			item := wt
			item.selected = false
			result = append(result, item)
			continue
		}
		result = append(result, applyOneDirtyPolicy(wt, action))
	}
	return result, nil
}

func parseDirtyPolicy(value string) (string, error) {
	for _, policy := range dirtyPolicies {
		if value == policy {
			return policy, nil
		}
	}
	return "", output.Errorf(output.CodeUsage,
		"invalid --dirty value %q (want %s)", value, strings.Join(dirtyPolicies, ", ")).
		WithDetail("dirty", value).
		WithDetail("valid", dirtyPolicies)
}

// dirtyCandidates names the worktrees blocking the run, so a needs_input error says
// which ones rather than only how many.
func dirtyCandidates(worktrees []syncEntry) []string {
	var out []string
	for _, wt := range worktrees {
		if wt.dirty {
			out = append(out, wt.repo+"/"+wt.branch)
		}
	}
	return out
}

func applyDirtyPolicy(worktrees []syncEntry, policy string) []syncEntry {
	result := make([]syncEntry, 0, len(worktrees))
	for _, wt := range worktrees {
		if !wt.dirty || !wt.selected {
			result = append(result, wt)
			continue
		}
		result = append(result, applyOneDirtyPolicy(wt, policy))
	}
	return result
}

func applyOneDirtyPolicy(wt syncEntry, policy string) syncEntry {
	item := wt
	switch policy {
	case "stash", "reset":
		item.pullAction = policy
	case "skip":
		item.selected = false
	}
	return item
}

func selectedSyncEntries(candidates []syncEntry) []syncEntry {
	var selected []syncEntry
	for _, entry := range candidates {
		if entry.selected {
			selected = append(selected, entry)
		}
	}
	return selected
}

// executeSync converges the selected worktrees through the shared fan-out engine.
//
// Delegation gives deterministic result order, per-item post_sync hooks, hook failures
// as warnings, and bounded cross-repo concurrency. SerialPerRepo stays false: pulls
// across worktrees of one repo are safe; only creation contends on config.lock.
func executeSync(selected []syncEntry) ([]syncOpResult, []*output.Diagnostic) {
	targets := make([]fanout.Target, 0, len(selected))
	entryByKey := make(map[string]syncEntry, len(selected))
	for _, entry := range selected {
		t := fanout.Target{
			Group:    entry.group,
			Repo:     entry.repo,
			Branch:   entry.branch,
			Path:     entry.path,
			BareRepo: entry.barePath,
		}
		// One worktree per branch per repo, so the key is unique.
		entryByKey[t.Key()] = entry
		targets = append(targets, t)
	}

	reporter := newSyncReporter(len(targets))
	results := fanout.Run(context.Background(), targets, fanout.Config{
		SerialPerRepo: false,
		Reporter:      reporter,
		Hook: func(_ context.Context, t fanout.Target) ([]*output.Diagnostic, error) {
			entry := entryByKey[t.Key()]
			hctx := hooks.Context{
				Group:        entry.group,
				Repo:         entry.repo,
				Branch:       entry.branch,
				WorktreePath: entry.path,
				BarePath:     entry.barePath,
			}
			result, err := runHookEvent("post_sync", hctx, entry.path)
			return result.Warnings, err
		},
	}, func(_ context.Context, t fanout.Target) fanout.ItemResult {
		return syncOne(entryByKey[t.Key()]).toItemResult()
	})
	reporter.finish()

	ops := make([]syncOpResult, 0, len(results))
	var hookWarnings []*output.Diagnostic
	for _, result := range results {
		entry := entryByKey[result.Target.Key()]
		ops = append(ops, syncOpResultFrom(entry, result))
		for _, warning := range result.HookWarnings {
			hookWarnings = append(hookWarnings, warning.WithSubject("worktree", entry.repo+"/"+entry.branch))
		}
	}

	// Return all pull results and hook warnings; runSync derives partial_failure from
	// the git outcomes. Hook failures do not truncate the envelope.
	return ops, hookWarnings
}

// toItemResult maps sync's own result onto the engine's disposition vocabulary.
//
// sync filters up-to-date worktrees out during selection, so an entry that reaches
// the engine is always meant to change: the outcome is Created or Failed, never
// Skipped. Convergence lives in filterWithUpdates for this command.
func (r syncOpResult) toItemResult() fanout.ItemResult {
	if r.err != nil {
		return fanout.ItemResult{Disposition: fanout.Failed, Reason: r.status, Err: r.err}
	}
	return fanout.ItemResult{Disposition: fanout.Created, Reason: r.status}
}

// syncOpResultFrom rebuilds sync's result from the engine's, preserving the status
// string the renderer and the JSON envelope both read.
func syncOpResultFrom(entry syncEntry, result fanout.ItemResult) syncOpResult {
	op := syncOpResult{entry: entry, status: result.Reason, err: result.Err}
	if result.Disposition == fanout.Created {
		op.pulled = true
		if op.status == "" {
			op.status = "pulled"
		}
	}
	if result.Disposition == fanout.Failed {
		op.status = "failed"
	}
	return op
}

func syncOne(entry syncEntry) syncOpResult {
	result := syncOpResult{entry: entry}
	switch entry.pullAction {
	case "stash":
		if err := git.StashChanges(entry.path); err != nil {
			result.status = "failed"
			result.err = output.Wrap(output.CodeGitFailed, err, "failed to stash changes")
			return result
		}
		// Confirm a stash exists before popping. StashChanges now includes untracked
		// files so one normally will, but popping unconditionally turns "there was
		// nothing to save" into a reported sync FAILURE for a pull that succeeded.
		stashed, stashErr := git.HasStash(entry.path)
		if stashErr != nil {
			result.status = "failed"
			result.err = output.Wrap(output.CodeGitFailed, stashErr, "failed to inspect stashes")
			return result
		}
		if err := git.PullWorktree(entry.path); err != nil {
			result.status = "failed"
			result.err = output.Wrap(output.CodeGitFailed, err, "failed to pull")
			return result
		}
		if stashed {
			if err := git.PopStash(entry.path); err != nil {
				result.status = "failed"
				result.err = output.Wrap(output.CodeGitFailed, err, "pull succeeded but failed to restore stash")
				return result
			}
		}
		result.status = "stashed"
		result.pulled = true
		return result
	case "reset":
		if err := git.ResetHard(entry.path); err != nil {
			result.status = "failed"
			result.err = output.Wrap(output.CodeGitFailed, err, "failed to reset worktree")
			return result
		}
		if err := git.PullWorktree(entry.path); err != nil {
			result.status = "failed"
			result.err = output.Wrap(output.CodeGitFailed, err, "failed to pull")
			return result
		}
		result.status = "pulled"
		result.pulled = true
		return result
	default:
		if err := git.PullWorktree(entry.path); err != nil {
			result.status = "failed"
			result.err = output.Wrap(output.CodeGitFailed, err, "failed to pull")
			return result
		}
		result.status = "pulled"
		result.pulled = true
		return result
	}
}

func buildIdleResults(entries []syncEntry) []syncOpResult {
	results := make([]syncOpResult, 0, len(entries))
	for _, entry := range entries {
		results = append(results, syncOpResult{entry: entry, status: idleStatus(entry)})
	}
	return results
}

func idleStatus(entry syncEntry) string {
	if isLocalOnlyEntry(entry) {
		return "local-only"
	}
	return "up-to-date"
}

func mergeSyncResults(all []syncEntry, selected []syncEntry, ops []syncOpResult) []syncOpResult {
	opByKey := make(map[string]syncOpResult, len(ops))
	for _, op := range ops {
		opByKey[syncEntryKey(op.entry)] = op
	}
	results := make([]syncOpResult, 0, len(all))
	for _, entry := range all {
		key := syncEntryKey(entry)
		if op, ok := opByKey[key]; ok {
			results = append(results, op)
			continue
		}
		if isLocalOnlyEntry(entry) {
			results = append(results, syncOpResult{entry: entry, status: "local-only"})
			continue
		}
		if entry.behind > 0 {
			results = append(results, syncOpResult{entry: entry, status: "skipped"})
			continue
		}
		results = append(results, syncOpResult{entry: entry, status: "up-to-date"})
	}

	// Sort what is SERIALISED, not just what the engine computed. fanout.Run returns
	// its own results ordered, but this function reassembles the full list — pulled,
	// skipped and up-to-date together — in `git worktree list` order, which put the
	// repo's main worktree first and the rest in checkout order. That leaked an
	// unstable order into the envelope and the table even after the engine became
	// deterministic.
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i].entry, results[j].entry
		if a.group != b.group {
			return a.group < b.group
		}
		if a.repo != b.repo {
			return a.repo < b.repo
		}
		return a.branch < b.branch
	})
	return results
}

func entryToJSON(op syncOpResult) syncWorktreeJSON {
	return syncWorktreeJSON{
		Group: op.entry.group, Repo: op.entry.repo, Name: op.entry.name, Branch: op.entry.branch,
		Path: op.entry.path, Upstream: op.entry.upstream, Ahead: op.entry.ahead, Behind: op.entry.behind,
		Dirty: op.entry.dirty, Status: op.status, Pulled: op.pulled,
	}
}

func summaryFromResults(results []syncOpResult) syncSummaryJSON {
	summary := syncSummaryJSON{Total: len(results)}
	for _, op := range results {
		switch op.status {
		case "pulled", "stashed":
			summary.Pulled++
		case "skipped":
			summary.Skipped++
		case "local-only":
			summary.LocalOnly++
		case "failed":
			summary.Failed++
		}
	}
	return summary
}

func buildSyncOutput(project, root string, results []syncOpResult) (syncJSON, syncSummaryJSON) {
	items := make([]syncWorktreeJSON, 0, len(results))
	for _, op := range results {
		items = append(items, entryToJSON(op))
	}
	summary := summaryFromResults(results)
	return syncJSON{Project: project, Root: root, Worktrees: items, Summary: summary}, summary
}

func failedWorktreeDetails(results []syncOpResult) []map[string]string {
	var failed []map[string]string
	for _, op := range results {
		if op.status != "failed" || op.err == nil {
			continue
		}
		failed = append(failed, map[string]string{"group": op.entry.group, "repo": op.entry.repo, "branch": op.entry.branch, "error": op.err.Error()})
	}
	return failed
}

func printSyncText(results []syncOpResult, summary syncSummaryJSON) {
	fmt.Println()
	fmt.Println(styles.Title.Render("Sync Results"))
	fmt.Println()
	if summary.Pulled > 0 {
		fmt.Printf("  %s Successfully synced %d worktree(s)\n", styles.Success.Render("ok"), summary.Pulled)
	}
	if summary.LocalOnly > 0 {
		fmt.Printf("  %s %d local-only worktree(s)\n", styles.Label.Render("-"), summary.LocalOnly)
	}
	if summary.Failed > 0 {
		fmt.Printf("  %s Failed to sync %d worktree(s)\n", styles.Error.Render("fail"), summary.Failed)
		fmt.Println()
		fmt.Println(styles.Label.Render("Failed worktrees:"))
		for _, op := range results {
			if op.status == "failed" && op.err != nil {
				fmt.Printf("  - %s/%s: %s\n", op.entry.repo, op.entry.branch, op.err)
			}
		}
	}
	fmt.Println()
}
