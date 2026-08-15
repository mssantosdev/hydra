package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

const (
	checkMissingFetchRefspec   = "missing_fetch_refspec"
	checkMissingOriginHead     = "missing_origin_head"
	checkBranchNoUpstream      = "branch_no_upstream"
	checkWorktreeInsideGitdir  = "worktree_inside_gitdir"
	checkLegacySymlink         = "legacy_symlink"
	checkWorktreeMissingOnDisk = "worktree_missing_on_disk"
	// checkWorktreeOrphanedDir is the path-still-present half: pruning the registration
	// leaves the directory behind, which then blocks `hydra add`. Deleting it is the
	// caller's decision, so this is deliberately not fixable.
	checkWorktreeOrphanedDir  = "worktree_orphaned_dir"
	checkWorktreeUnregistered = "worktree_unregistered"
	checkStaleGitState        = "stale_git_state"
	checkWorktreeDetached     = "worktree_detached"
	checkWorktreeDirty        = "worktree_dirty"
	checkRegistryDangling     = "registry_dangling"
	checkBareUnregistered     = "bare_unregistered"
	// checkTopicDanglingMember covers EVERY way membership can outlive its worktree:
	// a removal that skipped hydra, an interrupted remove, a branch renamed behind
	// hydra's back, or a hand-edited state file. One check, because they all reduce
	// to the same observable fact — a recorded member with no worktree on disk.
	checkTopicDanglingMember = "topic_dangling_member"
	// checkTopicUnknownRepo is membership naming a repository the manifest does not have —
	// a hand-edited state file, or a `repo remove` that left a topic behind. Distinct from
	// topic_dangling_member, where the repo IS registered and only the worktree is gone,
	// because the two need different messages: one says "recreate the worktree", the other
	// says "this repo does not exist here at all".
	checkTopicUnknownRepo = "topic_unknown_repo"
	// checkTopicDanglingLink is a recorded relationship whose target topic does not exist.
	// The CLI cannot produce one — removing a topic sweeps the edges naming it in the same
	// write — so this means a hand-edited state file or a file written by a hydra older than
	// that sweep. Reported because a dangling depends_on edge BLOCKS a close with
	// `dependency_missing`, which is a refusal nobody can act on until the edge is dropped.
	checkTopicDanglingLink = "topic_dangling_link"
)

var (
	doctorAll bool
	doctorFix bool
)

var doctorCmd = &cobra.Command{
	Annotations: map[string]string{annotationRegistryFanout: "all"},
	Use:         "doctor",
	Short:       "Diagnose workspace health and optionally repair fixable issues",
	Long: `Run health checks against the current Hydra workspace (or every registered project with --all).

DESCRIPTION
  doctor inspects bare repositories, worktrees, group directories, and the
  global project registry. Each check reports a stable id, a status of ok,
  warn, or fail, and the repo/worktree it concerns. Use --fix to repair the
  subset marked fixable.

WHEN TO USE
  • After upgrading hydra or hand-editing .hydra/config.yaml
  • When worktrees behave oddly (missing upstream, stale registrations)
  • Before prune/adopt to understand what will be touched
  • In CI to assert workspace invariants with --output json

EXAMPLES
  hydra doctor
  hydra doctor --output json
  hydra doctor --fix
  hydra doctor --all --output json

FLAGS
  --all   Check every project in the global registry (skips loading the cwd project)
  --fix   Repair fixable checks (fetch refspec, origin/HEAD, upstream, symlinks, prune registrations, registry)

EXIT CODES
  0  Success (no failing checks remain)
  2  not_in_project (without --all and outside a workspace)
  4  partial_failure (one or more checks are still fail after the report)

SEE ALSO
  hydra prune - Remove stale worktree registrations and empty group dirs
  hydra repo add <path> --adopt - track an existing checkout`,
	RunE: runDoctor,
}

type doctorCheck struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Repo     string `json:"repo,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	// Topic and Branch carry the exact identity a topic fix needs. They are typed
	// fields rather than substrings of Message or Worktree so --fix never has to
	// parse a human-readable string to decide what to act on.
	Topic  string `json:"topic,omitempty"`
	Branch string `json:"branch,omitempty"`
	// LinkKind and LinkTo identify one relationship, for the same reason Topic and Branch
	// exist: --fix must not parse Message to learn which edge to drop.
	LinkKind string `json:"link_kind,omitempty"`
	LinkTo   string `json:"link_to,omitempty"`
	Fixable  bool   `json:"fixable"`
	Fixed    bool   `json:"fixed,omitempty"`
}

type doctorSummary struct {
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
	Fixed int `json:"fixed"`
}

type doctorReport struct {
	Project string        `json:"project"`
	Root    string        `json:"root"`
	Checks  []doctorCheck `json:"checks"`
	Summary doctorSummary `json:"summary"`
}

type doctorAllReport struct {
	Projects []doctorReport `json:"projects"`
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().BoolVar(&doctorAll, "all", false, "Check every registered project")
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Repair fixable issues")
}

func runDoctor(cmd *cobra.Command, args []string) error {
	targets, warnings, err := projectTargets(doctorAll)
	if err != nil {
		return err
	}

	reports := make([]doctorReport, 0, len(targets))
	loaded := make([]struct {
		cfg  *config.Config
		root string
	}, len(targets))

	for i, target := range targets {
		loadedCfg := target.Cfg
		if loadedCfg == nil {
			return output.Errorf(output.CodeInternal, "project %s has no configuration", target.Name)
		}
		loaded[i] = struct {
			cfg  *config.Config
			root string
		}{cfg: loadedCfg, root: target.Root}
		reports = append(reports, diagnoseProject(loadedCfg, target.Root, target.Name))
	}

	if len(reports) > 0 {
		regChecks := diagnoseRegistry()
		reports[0].Checks = append(reports[0].Checks, regChecks...)
		recomputeDoctorSummary(&reports[0])
	}

	if doctorFix {
		for i := range reports {
			applyDoctorFixes(&reports[i], loaded[i].cfg, loaded[i].root)
			recomputeDoctorSummary(&reports[i])
		}
		if len(reports) > 0 {
			applyRegistryFixes(&reports[0])
			recomputeDoctorSummary(&reports[0])
		}
	}

	var payload any
	if doctorAll || len(reports) != 1 {
		payload = doctorAllReport{Projects: reports}
	} else if len(reports) == 1 {
		payload = reports[0]
	} else {
		payload = doctorReport{Checks: []doctorCheck{}, Summary: doctorSummary{}}
	}

	failures := countDoctorFailures(reports)

	// When checks still fail, emit outcome=partial with a partial_failure error on the
	// same envelope so exit status and JSON agree.
	var doctorErr *output.Error
	outcome := output.OutcomeSuccess
	if failures > 0 {
		outcome = output.OutcomePartial
		doctorErr = output.Errorf(output.CodePartialFailure,
			"%d health check(s) failed", failures).
			WithDetail("failed", failures)
	}

	if err := emitResult(cmd, output.Result{
		Outcome:  outcome,
		Summary:  doctorSummaryLine(reports, failures),
		Data:     payload,
		Warnings: warnings,
		Err:      doctorErr,
	}, func() { printDoctorText(reports) }); err != nil {
		return err
	}
	if doctorErr != nil {
		return doctorErr
	}
	return nil
}

// doctorSummaryLine states whether the workspace needs attention and whether hydra
// can fix it, which is the only thing a caller does next.
func doctorSummaryLine(reports []doctorReport, failures int) string {
	var checks, fixable, fixed int
	for _, report := range reports {
		for _, check := range report.Checks {
			checks++
			if check.Fixed {
				fixed++
				continue
			}
			if check.Status == "fail" && check.Fixable {
				fixable++
			}
		}
	}

	switch {
	case fixed > 0 && failures == 0:
		return fmt.Sprintf("%d check(s), %d fixed, healthy", checks, fixed)
	case failures == 0:
		return fmt.Sprintf("%d check(s), healthy", checks)
	case fixable > 0:
		return fmt.Sprintf("%d check(s), %d failing (%d fixable with --fix)", checks, failures, fixable)
	default:
		return fmt.Sprintf("%d check(s), %d failing, none auto-fixable", checks, failures)
	}
}

func diagnoseProject(cfg *config.Config, projectRoot, projectName string) doctorReport {
	report := doctorReport{Project: projectName, Root: projectRoot}
	bareRoot := filepath.Join(projectRoot, cfg.Paths.BareDir)

	for _, repo := range allRepoContexts(cfg, projectRoot) {
		report.Checks = append(report.Checks, diagnoseBareRepo(repo)...)
		worktrees, _ := listRepoWorktrees(repo)
		for _, wt := range worktrees {
			report.Checks = append(report.Checks, diagnoseWorktree(repo, bareRoot, wt)...)
		}
	}
	report.Checks = append(report.Checks, diagnoseGroupDirs(cfg, projectRoot, bareRoot)...)
	report.Checks = append(report.Checks, diagnoseBareDir(cfg, bareRoot)...)
	report.Checks = append(report.Checks, diagnoseTopicMembers(cfg, projectRoot)...)
	sortDoctorChecks(&report)
	recomputeDoctorSummary(&report)
	return report
}

func diagnoseBareRepo(repo repoContext) []doctorCheck {
	var checks []doctorCheck

	// A locally-bootstrapped project has no upstream at all. That is a valid
	// state, not damage, so neither remote check applies.
	if !git.HasRemote(repo.BareRepo) {
		return []doctorCheck{{
			ID: checkMissingFetchRefspec, Status: "ok",
			Message: "no origin configured (local-only repository)",
			Repo:    repo.Alias,
		}, {
			ID: checkMissingOriginHead, Status: "ok",
			Message: "no origin configured (local-only repository)",
			Repo:    repo.Alias,
		}}
	}

	if git.GetConfig(repo.BareRepo, "remote.origin.fetch") == "" {
		checks = append(checks, doctorCheck{
			ID: checkMissingFetchRefspec, Status: "fail",
			Message: "remote.origin.fetch is not configured",
			Repo:    repo.Alias, Fixable: true,
		})
	} else {
		checks = append(checks, doctorCheck{
			ID: checkMissingFetchRefspec, Status: "ok",
			Message: "remote.origin.fetch is configured",
			Repo:    repo.Alias, Fixable: true,
		})
	}

	if !git.HasOriginHead(repo.BareRepo) {
		checks = append(checks, doctorCheck{
			ID: checkMissingOriginHead, Status: "fail",
			Message: "refs/remotes/origin/HEAD is not set",
			Repo:    repo.Alias, Fixable: true,
		})
	} else {
		checks = append(checks, doctorCheck{
			ID: checkMissingOriginHead, Status: "ok",
			Message: "refs/remotes/origin/HEAD is set",
			Repo:    repo.Alias, Fixable: true,
		})
	}
	return checks
}

// diagnoseBareDir finds bare repositories on disk that no config entry claims.
// An interrupted clone leaves exactly this: <bare_dir>/<alias>.git exists, but the
// crash happened before the repo was registered, so every other check skips it and
// the orphan is invisible. It is reported, never auto-removed: deleting git data is
// destructive, and re-registering needs a group the user must choose.
func diagnoseBareDir(cfg *config.Config, bareRoot string) []doctorCheck {
	entries, err := os.ReadDir(bareRoot)
	if err != nil {
		// No bare directory yet is normal for a freshly initialized workspace.
		return nil
	}

	registered := make(map[string]struct{})
	for _, ref := range cfg.Repos() {
		registered[ref.Alias+".git"] = struct{}{}
	}

	var checks []doctorCheck
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasSuffix(name, ".git") {
			continue
		}
		if _, ok := registered[name]; ok {
			continue
		}
		alias := strings.TrimSuffix(name, ".git")

		// FAIL, not warn. `.bare/` is hydra's own directory — an unregistered bare repo is
		// state that list/status/run/sync omit from their listings.
		//
		// The remote is read off the bare repo so recovery can name a convergent `repo add`.
		remote := bareOriginURL(filepath.Join(bareRoot, name))
		recovery := "hydra repo add <url> --as " + alias + " --group <group>"
		if remote != "" {
			recovery = "hydra repo add " + remote + " --as " + alias + " --group <group>"
		}
		checks = append(checks, doctorCheck{
			ID:     checkBareUnregistered,
			Status: "fail",
			Message: fmt.Sprintf(
				"%s exists on disk but is not in the manifest, so hydra cannot see it; run %q to register it, or delete %s",
				name, recovery, filepath.Join(bareRoot, name)),
			Repo: alias,
		})
	}
	return checks
}

// diagnoseTopicMembers finds membership that outlived its worktree.
//
// The store deliberately records only what git cannot know, so it can drift when
// a worktree disappears without hydra. Reported per member rather than per topic,
// because the fix is per member: detach exactly the entries whose worktree is
// gone, leaving the rest of the topic intact.
func diagnoseTopicMembers(cfg *config.Config, projectRoot string) []doctorCheck {
	topics, err := topic.Open(projectRoot).List()
	if err != nil {
		// Unreadable state is itself worth reporting, but it is not fixable by
		// detaching: the file must be repaired or removed by hand.
		return []doctorCheck{{
			ID: checkTopicDanglingMember, Status: "fail", Fixable: false,
			Message: fmt.Sprintf("topic state at %s could not be read: %v", topic.Path(projectRoot), err),
		}}
	}
	if len(topics) == 0 {
		return nil
	}

	// Build the live (repo, branch) set once. A repo whose worktrees cannot be
	// listed is skipped entirely rather than treated as empty — otherwise a
	// transient git failure would report every member of that repo as dangling.
	live := make(map[string]struct{})
	listed := make(map[string]bool)
	// registered is built from the MANIFEST, not from a successful listing. The two are
	// different facts and conflating them hid a real drift: a member naming a repo the manifest
	// does not have was skipped by the same guard that exists to tolerate a transient git
	// failure, so the one case doctor could never recover from was the one it stayed silent
	// about.
	registered := make(map[string]bool)
	for _, repo := range allRepoContexts(cfg, projectRoot) {
		registered[repo.Alias] = true
		worktrees, err := listRepoWorktrees(repo)
		if err != nil {
			continue
		}
		listed[repo.Alias] = true
		for _, wt := range worktrees {
			if wt.Branch != "" {
				live[topicKey(repo.Alias, wt.Branch)] = struct{}{}
			}
		}
	}

	var checks []doctorCheck
	for _, t := range topics {
		for _, m := range t.Members {
			if !registered[m.Repo] {
				checks = append(checks, doctorCheck{
					ID: checkTopicUnknownRepo, Status: "fail", Fixable: true,
					Message: fmt.Sprintf(
						"topic %q records %s on branch %q, but no repository %q is registered; --fix detaches it",
						t.ID, m.Repo, m.Branch, m.Repo),
					Repo:     m.Repo,
					Topic:    t.ID,
					Branch:   m.Branch,
					Worktree: m.Repo + "@" + m.Branch,
				})
				continue
			}
			if !listed[m.Repo] {
				continue
			}
			if _, ok := live[topicKey(m.Repo, m.Branch)]; ok {
				continue
			}
			checks = append(checks, doctorCheck{
				// "fail", not "warn": hydra would otherwise report a topic
				// containing a worktree that does not exist — wrong output, not a
				// caution. It also matches doctor's existing invariant that only
				// "fail" checks are eligible for --fix.
				ID: checkTopicDanglingMember, Status: "fail", Fixable: true,
				Message: fmt.Sprintf(
					"topic %q still records %s on branch %q, but no such worktree exists; --fix detaches it",
					t.ID, m.Repo, m.Branch),
				Repo:     m.Repo,
				Topic:    t.ID,
				Branch:   m.Branch,
				Worktree: m.Repo + "@" + m.Branch,
			})
		}
	}

	// Every edge must name a live topic. Checked against the ids the store reports rather
	// than against the map keys, so identity that exists but has no members — which the GC
	// removes — counts as absent here too.
	liveTopic := make(map[string]bool, len(topics))
	for _, t := range topics {
		liveTopic[t.ID] = true
	}
	for _, t := range topics {
		for _, l := range t.Links {
			if liveTopic[l.To] {
				continue
			}
			checks = append(checks, doctorCheck{
				ID: checkTopicDanglingLink, Status: "fail", Fixable: true,
				Message: fmt.Sprintf(
					"topic %q records %s %q, but no topic %q exists; --fix drops the relationship",
					t.ID, l.Kind, l.To, l.To),
				Topic:    t.ID,
				LinkKind: l.Kind,
				LinkTo:   l.To,
			})
		}
	}
	return checks
}

func diagnoseWorktree(repo repoContext, bareRoot string, wt worktreeContext) []doctorCheck {
	var checks []doctorCheck
	label := wt.Qualified()

	if isPathInside(wt.Path, bareRoot) {
		checks = append(checks, doctorCheck{
			ID: checkWorktreeInsideGitdir, Status: "fail",
			Message: "worktree lives inside the bare repository directory; recreate it as a sibling under the group",
			Repo:    repo.Alias, Worktree: label, Fixable: false,
		})
	} else {
		checks = append(checks, doctorCheck{
			ID: checkWorktreeInsideGitdir, Status: "ok",
			Message: "worktree is outside the bare repository directory",
			Repo:    repo.Alias, Worktree: label, Fixable: false,
		})
	}

	// Split orphaned registration (path present) from missing registration (path absent):
	// the former is not safe to auto-delete; the latter is fixable with --fix prune.
	//
	//   path absent   -> prunable registration, --fix can clear it
	//   path present  -> a directory in the way, NOT safe to delete automatically
	_, statErr := os.Stat(wt.Path)
	switch {
	case statErr == nil && wt.Prunable:
		checks = append(checks, doctorCheck{
			ID: checkWorktreeOrphanedDir, Status: "fail", Branch: wt.Branch,
			Message: fmt.Sprintf(
				"%s is no longer a valid worktree but the directory still exists; move or delete %s, then run \"hydra add %s %s\"",
				label, wt.Path, repo.Alias, wt.Branch),
			Repo: repo.Alias, Worktree: label, Fixable: false,
		})
	case statErr != nil || wt.Prunable:
		checks = append(checks, doctorCheck{
			ID: checkWorktreeMissingOnDisk, Status: "fail", Branch: wt.Branch,
			Message: fmt.Sprintf(
				"worktree is registered but missing on disk; --fix clears the registration, then \"hydra add %s %s\" recreates it",
				repo.Alias, wt.Branch),
			Repo: repo.Alias, Worktree: label, Fixable: true,
		})
	default:
		checks = append(checks, doctorCheck{
			ID: checkWorktreeMissingOnDisk, Status: "ok", Branch: wt.Branch,
			Message: "worktree directory exists",
			Repo:    repo.Alias, Worktree: label, Fixable: true,
		})
	}

	if wt.Detached {
		checks = append(checks, doctorCheck{
			ID: checkWorktreeDetached, Status: "warn",
			Message: "worktree is in detached HEAD state",
			Repo:    repo.Alias, Worktree: label, Fixable: false,
		})
	} else {
		checks = append(checks, doctorCheck{
			ID: checkWorktreeDetached, Status: "ok",
			Message: "worktree is on a branch",
			Repo:    repo.Alias, Worktree: label, Fixable: false,
		})
	}

	if wt.Branch != "" && !wt.Detached {
		kind, err := git.ClassifyBranch(repo.BareRepo, wt.Branch)
		if err == nil && (kind == git.BranchBoth || kind == git.BranchRemote) {
			tracking, trackErr := git.WorktreeTracking(wt.Path)
			if trackErr == nil && tracking.Upstream == "" {
				checks = append(checks, doctorCheck{
					ID: checkBranchNoUpstream, Status: "fail",
					Message: "branch exists on origin but has no upstream configured",
					Repo:    repo.Alias, Worktree: label, Fixable: true,
				})
			} else {
				checks = append(checks, doctorCheck{
					ID: checkBranchNoUpstream, Status: "ok",
					Message: "branch upstream is configured",
					Repo:    repo.Alias, Worktree: label, Fixable: true,
				})
			}
		} else {
			checks = append(checks, doctorCheck{
				ID: checkBranchNoUpstream, Status: "ok",
				Message: "branch upstream check not applicable",
				Repo:    repo.Alias, Worktree: label, Fixable: true,
			})
		}
	}

	if states, err := git.InProgressGitState(wt.Path); err == nil && len(states) > 0 {
		checks = append(checks, doctorCheck{
			ID: checkStaleGitState, Status: "warn",
			Message: "in-progress git state: " + strings.Join(states, ", "),
			Repo:    repo.Alias, Worktree: label, Fixable: false,
		})
	} else {
		checks = append(checks, doctorCheck{
			ID: checkStaleGitState, Status: "ok",
			Message: "no in-progress rebase/merge/cherry-pick state",
			Repo:    repo.Alias, Worktree: label, Fixable: false,
		})
	}

	dirty, changes, err := git.HasUncommittedChanges(wt.Path)
	if err == nil && dirty {
		checks = append(checks, doctorCheck{
			ID: checkWorktreeDirty, Status: "warn",
			Message: fmt.Sprintf("%d uncommitted change(s)", changes),
			Repo:    repo.Alias, Worktree: label, Fixable: false,
		})
	} else {
		checks = append(checks, doctorCheck{
			ID: checkWorktreeDirty, Status: "ok",
			Message: "worktree is clean",
			Repo:    repo.Alias, Worktree: label, Fixable: false,
		})
	}
	return checks
}

func diagnoseGroupDirs(cfg *config.Config, projectRoot, bareRoot string) []doctorCheck {
	registered := map[string]struct{}{}
	for _, wt := range collectRegisteredPaths(cfg, projectRoot) {
		registered[wt] = struct{}{}
	}

	var checks []doctorCheck
	for _, group := range cfg.SortedGroups() {
		dir := groupDir(projectRoot, group)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			path := filepath.Join(dir, name)
			rel := filepath.ToSlash(filepath.Join(group, name))

			if entry.Type()&os.ModeSymlink != 0 {
				target, err := os.Readlink(path)
				if err != nil {
					continue
				}
				if strings.HasPrefix(filepath.Clean(target), bareRoot) || strings.Contains(target, cfg.Paths.BareDir) {
					checks = append(checks, doctorCheck{
						ID: checkLegacySymlink, Status: "fail",
						Message:  "legacy symlink points into the bare repository directory",
						Worktree: rel, Fixable: true,
					})
					continue
				}
			}

			if !entry.IsDir() {
				continue
			}
			if _, ok := registered[path]; ok {
				continue
			}
			gitDir := filepath.Join(path, ".git")
			if _, err := os.Stat(gitDir); err != nil {
				continue
			}
			checks = append(checks, doctorCheck{
				ID: checkWorktreeUnregistered, Status: "warn",
				Message:  "directory looks like a git worktree but is not registered with hydra",
				Worktree: rel, Fixable: false,
			})
		}
	}

	if len(checks) == 0 {
		checks = append(checks, doctorCheck{
			ID: checkLegacySymlink, Status: "ok",
			Message: "no legacy symlinks in group directories",
			Fixable: true,
		}, doctorCheck{
			ID: checkWorktreeUnregistered, Status: "ok",
			Message: "no unregistered worktree directories",
			Fixable: false,
		})
	}
	return checks
}

func collectRegisteredPaths(cfg *config.Config, projectRoot string) []string {
	worktrees, _ := collectWorktrees(cfg, projectRoot)
	paths := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		paths = append(paths, wt.Path)
	}
	return paths
}

func diagnoseRegistry() []doctorCheck {
	reg, err := registry.Load()
	if err != nil {
		return []doctorCheck{{
			ID: checkRegistryDangling, Status: "fail",
			Message: err.Error(), Fixable: false,
		}}
	}
	var dangling []string
	for name, root := range reg.Projects {
		if _, err := os.Stat(config.ManifestPath(root)); err != nil {
			dangling = append(dangling, name)
		}
	}
	if len(dangling) > 0 {
		return []doctorCheck{{
			ID: checkRegistryDangling, Status: "fail",
			Message: "registry entries without .hydra/config.yaml: " + strings.Join(dangling, ", "),
			Fixable: true,
		}}
	}
	return []doctorCheck{{
		ID: checkRegistryDangling, Status: "ok",
		Message: "registry entries point at valid workspaces",
		Fixable: true,
	}}
}

func fixPriority(id string) int {
	switch id {
	case checkMissingFetchRefspec, checkMissingOriginHead:
		return 0
	default:
		return 1
	}
}

func applyDoctorFixes(report *doctorReport, cfg *config.Config, projectRoot string) {
	reposByAlias := map[string]repoContext{}
	for _, repo := range allRepoContexts(cfg, projectRoot) {
		reposByAlias[repo.Alias] = repo
	}

	sort.SliceStable(report.Checks, func(i, j int) bool {
		return fixPriority(report.Checks[i].ID) < fixPriority(report.Checks[j].ID)
	})

	for i := range report.Checks {
		check := &report.Checks[i]
		if !check.Fixable || check.Status != "fail" {
			continue
		}
		repo, hasRepo := reposByAlias[check.Repo]
		// Every git-level fix needs a resolved repository, so a check naming one that is not
		// registered is normally skipped. topic_unknown_repo is the exception by definition:
		// the repo's ABSENCE is what it reports, and its fix only touches topic state — so
		// applying this guard to it meant the one drift doctor had just learned to detect was
		// also the one it could never repair.
		if check.Repo != "" && !hasRepo && check.ID != checkTopicUnknownRepo {
			continue
		}

		switch check.ID {
		case checkMissingFetchRefspec:
			if err := git.SetFetchRefspec(repo.BareRepo); err != nil {
				check.Message = err.Error()
				continue
			}
			if err := git.FetchBareRepo(repo.BareRepo); err != nil {
				check.Message = err.Error()
				continue
			}
			markDoctorFixed(check, "remote.origin.fetch restored")
		case checkMissingOriginHead:
			if err := git.SetOriginHead(repo.BareRepo); err != nil {
				check.Message = err.Error()
				continue
			}
			markDoctorFixed(check, "origin/HEAD restored")
		case checkBranchNoUpstream:
			wtPath := worktreePathFromCheck(projectRoot, check.Worktree)
			branch := branchFromWorktreeCheck(cfg, projectRoot, check.Worktree)
			if wtPath == "" || branch == "" {
				continue
			}
			if err := git.SetUpstream(wtPath, branch); err != nil {
				check.Message = err.Error()
				continue
			}
			markDoctorFixed(check, "upstream configured")
		case checkLegacySymlink:
			linkPath := filepath.Join(projectRoot, filepath.FromSlash(check.Worktree))
			if err := os.Remove(linkPath); err != nil {
				check.Message = err.Error()
				continue
			}
			markDoctorFixed(check, "legacy symlink removed")
		case checkWorktreeMissingOnDisk:
			if err := git.PruneWorktrees(repo.BareRepo); err != nil {
				check.Message = err.Error()
				continue
			}
			recreate := "stale worktree registration pruned"
			if check.Repo != "" && check.Branch != "" {
				// Only append a recreate hint when repo and branch are both known so the
				// suggested `hydra add` command is runnable.
				recreate += fmt.Sprintf("; run %q to recreate it",
					fmt.Sprintf("hydra add %s %s", check.Repo, check.Branch))
			}
			markDoctorFixed(check, recreate)

		case checkTopicDanglingMember, checkTopicUnknownRepo:
			// Detach only this member. Removing the whole topic would destroy
			// membership for worktrees that are still present and healthy.
			if err := topic.Open(projectRoot).Detach(check.Topic, check.Repo, check.Branch); err != nil {
				check.Message = err.Error()
				continue
			}
			markDoctorFixed(check, fmt.Sprintf("detached %s@%s from topic %q", check.Repo, check.Branch, check.Topic))

		case checkTopicDanglingLink:
			// Drop only this edge. The topic and every other relationship it declares are
			// fine; the target is what does not exist.
			link := topic.Link{Kind: check.LinkKind, To: check.LinkTo}
			if err := topic.Open(projectRoot).RemoveLink(check.Topic, link); err != nil {
				check.Message = err.Error()
				continue
			}
			markDoctorFixed(check, fmt.Sprintf("dropped %s %q from topic %q",
				check.LinkKind, check.LinkTo, check.Topic))
		}
	}
}

func applyRegistryFixes(report *doctorReport) {
	for i := range report.Checks {
		check := &report.Checks[i]
		if check.ID != checkRegistryDangling || !check.Fixable || check.Status != "fail" {
			continue
		}
		reg, err := registry.Load()
		if err != nil {
			check.Message = err.Error()
			continue
		}
		reg.Prune()
		if err := reg.Save(); err != nil {
			check.Message = err.Error()
			continue
		}
		markDoctorFixed(check, "dangling registry entries removed")
	}
}

func markDoctorFixed(check *doctorCheck, message string) {
	check.Status = "ok"
	check.Fixed = true
	check.Message = message
}

func worktreePathFromCheck(projectRoot, qualified string) string {
	if qualified == "" {
		return ""
	}
	return filepath.Join(projectRoot, filepath.FromSlash(qualified))
}

func branchFromWorktreeCheck(cfg *config.Config, projectRoot, qualified string) string {
	worktrees, _ := collectWorktrees(cfg, projectRoot)
	for _, wt := range worktrees {
		if wt.Qualified() == qualified {
			return wt.Branch
		}
	}
	return ""
}

func sortDoctorChecks(report *doctorReport) {
	sort.Slice(report.Checks, func(i, j int) bool {
		if report.Checks[i].ID == report.Checks[j].ID {
			return report.Checks[i].Worktree < report.Checks[j].Worktree
		}
		return report.Checks[i].ID < report.Checks[j].ID
	})
}

func recomputeDoctorSummary(report *doctorReport) {
	report.Summary = doctorSummary{}
	for _, check := range report.Checks {
		switch check.Status {
		case "ok":
			report.Summary.OK++
		case "warn":
			report.Summary.Warn++
		case "fail":
			report.Summary.Fail++
		}
		if check.Fixed {
			report.Summary.Fixed++
		}
	}
}

func countDoctorFailures(reports []doctorReport) int {
	total := 0
	for _, report := range reports {
		for _, check := range report.Checks {
			if check.Status == "fail" && !check.Fixed {
				total++
			}
		}
	}
	return total
}

func printDoctorText(reports []doctorReport) {
	okIcon := lipgloss.NewStyle().Foreground(styles.Green)
	warnIcon := lipgloss.NewStyle().Foreground(styles.Yellow)
	failIcon := lipgloss.NewStyle().Foreground(styles.Red)
	for _, report := range reports {
		fmt.Printf("%s\n", styles.Title.Render("Project: "+report.Project))
		fmt.Printf("%s\n\n", styles.Dimmed.Render(report.Root))
		for _, check := range report.Checks {
			icon := okIcon.Render("✓")
			switch check.Status {
			case "warn":
				icon = warnIcon.Render("!")
			case "fail":
				icon = failIcon.Render("✗")
			}
			target := check.ID
			if check.Worktree != "" {
				target += " (" + check.Worktree + ")"
			} else if check.Repo != "" {
				target += " (" + check.Repo + ")"
			}
			line := fmt.Sprintf("%s %s: %s", icon, target, check.Message)
			if check.Fixed {
				line += " " + styles.Dimmed.Render("[fixed]")
			}
			fmt.Println(line)
		}
		fmt.Printf("\n%s\n\n", styles.Dimmed.Render(fmt.Sprintf("ok=%d warn=%d fail=%d fixed=%d",
			report.Summary.OK, report.Summary.Warn, report.Summary.Fail, report.Summary.Fixed)))
	}
}

// bareOriginURL reads a bare repository's origin, so doctor can name the exact command
// that recovers it rather than leaving the caller to find the URL.
//
// A failure is not reported: the check is already a failure, and a missing origin only
// means the suggestion falls back to a placeholder.
func bareOriginURL(barePath string) string {
	return strings.TrimSpace(git.GetConfig(barePath, "remote.origin.url"))
}
