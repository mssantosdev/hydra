package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/config"
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
	syncCmd.Flags().BoolVarP(&syncForce, "force", "f", false, "Pull dirty worktrees (stash changes)")
}

func runSync(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject,
			`no hydra workspace found; run "hydra init" or pass --project <name>`)
	}
	wd, err := os.Getwd()
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to resolve the working directory")
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
		return emit(cmd, data, collectWarnings, func() { log.Info("No worktrees found to sync") })
	}
	if err := fetchSyncRepos(entries); err != nil {
		return err
	}
	entries, enrichWarnings := enrichSyncEntries(entries)
	collectWarnings = append(collectWarnings, enrichWarnings...)
	candidates := filterWithUpdates(entries)
	if len(candidates) == 0 {
		results := buildIdleResults(entries)
		data, _ := buildSyncOutput(cfg.Project, projectRoot, results)
		return emit(cmd, data, collectWarnings, func() { printSyncText(results, data.Summary) })
	}
	if !jsonMode() {
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
	selected = selectedSyncEntries(selected)
	if len(selected) == 0 {
		results := mergeSyncResults(entries, nil, nil)
		data, _ := buildSyncOutput(cfg.Project, projectRoot, results)
		return emit(cmd, data, collectWarnings, func() { log.Info("No worktrees selected for sync") })
	}
	if syncForce {
		applyForceDirty(selected)
	} else {
		selected = handleDirtyWorktrees(selected)
		selected = selectedSyncEntries(selected)
	}
	if len(selected) == 0 {
		results := mergeSyncResults(entries, nil, nil)
		data, _ := buildSyncOutput(cfg.Project, projectRoot, results)
		return emit(cmd, data, collectWarnings, func() { log.Info("No worktrees selected for sync") })
	}
	ops, hookWarnings, execErr := executeSync(selected)
	allWarnings := append(collectWarnings, hookWarnings...)
	results := mergeSyncResults(entries, selected, ops)
	data, summary := buildSyncOutput(cfg.Project, projectRoot, results)
	if err := emit(cmd, data, allWarnings, func() { printSyncText(results, summary) }); err != nil {
		return err
	}
	if execErr != nil {
		return execErr
	}
	failed := failedWorktreeDetails(results)
	if len(failed) > 0 {
		return output.Errorf(output.CodePartialFailure, "%d worktree(s) failed to sync", len(failed)).WithDetail("worktrees", failed)
	}
	return nil
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

func gatherSyncEntries(projectCfg *config.Config, root, targetAlias string) ([]syncEntry, []string) {
	var entries []syncEntry
	var warnings []string
	for _, ref := range projectCfg.Repos() {
		if targetAlias != "" && ref.Alias != targetAlias {
			continue
		}
		bare := projectCfg.BarePath(root, ref.Alias)
		if _, err := os.Stat(bare); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s/%s: bare repository missing at %s", ref.Group, ref.Alias, bare))
			continue
		}
		wtList, err := git.ListWorktrees(bare)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s/%s: %v", ref.Group, ref.Alias, err))
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

func fetchSyncRepos(entries []syncEntry) error {
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if _, ok := seen[entry.barePath]; ok {
			continue
		}
		seen[entry.barePath] = struct{}{}
		if err := git.FetchBareRepo(entry.barePath); err != nil {
			return output.Wrap(output.CodeGitFailed, err, "failed to fetch %s", entry.repo)
		}
	}
	return nil
}

func enrichSyncEntries(entries []syncEntry) ([]syncEntry, []string) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var warnings []string
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
				warnings = append(warnings, fmt.Sprintf("%s/%s: %v", entries[idx].group, entries[idx].name, err))
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

func autoSelectYes(candidates []syncEntry) []syncEntry {
	out := make([]syncEntry, len(candidates))
	copy(out, candidates)
	for i := range out {
		if syncForce || !out[i].dirty {
			out[i].selected = true
			if syncForce && out[i].dirty {
				out[i].pullAction = "stash"
			}
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
	fmt.Println()
	fmt.Println(styles.Title.Render("Worktrees with Available Updates"))
	fmt.Println()
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	fmt.Printf("  %s  %-15s %-15s %-8s %-12s\n", headerStyle.Render("Select"), headerStyle.Render("Repository"), headerStyle.Render("Branch"), headerStyle.Render("Behind"), headerStyle.Render("Status"))
	fmt.Println(strings.Repeat("-", 70))
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
	if err := form.Run(); err != nil {
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

func handleDirtyWorktrees(worktrees []syncEntry) []syncEntry {
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
		if err := form.Run(); err != nil {
			item := wt
			item.selected = false
			result = append(result, item)
			continue
		}
		item := wt
		switch action {
		case "stash":
			item.pullAction = "stash"
			result = append(result, item)
		case "reset":
			item.pullAction = "reset"
			result = append(result, item)
		case "skip":
			item.selected = false
			result = append(result, item)
		}
	}
	return result
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

func executeSync(selected []syncEntry) ([]syncOpResult, []string, error) {
	resultChan := make(chan syncOpResult, len(selected))
	var wg sync.WaitGroup
	for _, entry := range selected {
		wg.Add(1)
		go func(entry syncEntry) {
			defer wg.Done()
			resultChan <- syncOne(entry)
		}(entry)
	}
	go func() { wg.Wait(); close(resultChan) }()
	results := make([]syncOpResult, 0, len(selected))
	for result := range resultChan {
		results = append(results, result)
	}
	var hookWarnings []string
	for i, result := range results {
		if result.err != nil || !result.pulled {
			continue
		}
		hctx := hooks.Context{Group: result.entry.group, Repo: result.entry.repo, Branch: result.entry.branch, WorktreePath: result.entry.path, BarePath: result.entry.barePath}
		hookResult, err := runHookEvent("post_sync", hctx, result.entry.path)
		hookWarnings = append(hookWarnings, hookResult.Warnings...)
		if err != nil {
			results[i].status = "failed"
			results[i].err = err
			results[i].pulled = false
			return results, hookWarnings, err
		}
	}
	if !jsonMode() && len(results) > 0 {
		fmt.Println()
		fmt.Println(styles.Title.Render("Pulling Updates"))
		fmt.Println()
		total := len(selected)
		for i, result := range results {
			status := styles.Success.Render("ok")
			if result.err != nil {
				status = styles.Error.Render("fail")
			}
			fmt.Printf("  %s %d/%d %s/%s\n", status, i+1, total, result.entry.repo, result.entry.branch)
		}
	}
	return results, hookWarnings, nil
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
		if err := git.PullWorktree(entry.path); err != nil {
			result.status = "failed"
			result.err = output.Wrap(output.CodeGitFailed, err, "failed to pull")
			return result
		}
		if err := git.PopStash(entry.path); err != nil {
			result.status = "failed"
			result.err = output.Wrap(output.CodeGitFailed, err, "pull succeeded but failed to restore stash")
			return result
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
