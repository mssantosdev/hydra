package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mssantosdev/hydra/internal/carry"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/hooks"
	"github.com/spf13/pflag"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
)

// repoContext is a repo registered in .hydra/config.yaml, resolved against a project root.
type repoContext struct {
	Group         string
	Alias         string
	Remote        string
	DefaultBranch string
	BareRepo      string
}

// worktreeContext is a real worktree as reported by git. Path and Branch always
// come from `git worktree list --porcelain`; neither is ever derived from the other.
type worktreeContext struct {
	RepoContext repoContext
	Branch      string // "" when detached
	Detached    bool
	DirName     string
	Path        string
	Head        string
	Locked      bool
	Prunable    bool
}

// Name is the stable short handle for a worktree: its real directory basename.
func (w worktreeContext) Name() string { return w.DirName }

// Qualified is the group-scoped handle, unique across the project.
func (w worktreeContext) Qualified() string {
	return w.RepoContext.Group + "/" + w.DirName
}

// BranchLabel renders the branch for humans, naming detachment explicitly.
func (w worktreeContext) BranchLabel() string {
	if w.Detached || w.Branch == "" {
		return "(detached)"
	}
	return w.Branch
}

type branchChoice struct {
	Name            string
	DisplayName     string
	HasWorktree     bool
	WorktreeName    string
	WorktreePath    string
	IsRemoteDefault bool
}

// againstJSON answers "where is this worktree relative to REF".
//
// The relationship is computed at query time from refs git already has, which is why
// hydra stores no integration edge: a stored edge would need reconciling and could go
// stale, while this cannot.
type againstJSON struct {
	Ref string `json:"ref"`
	// Merged is the question people actually ask. It is true when nothing on this
	// branch is missing from REF, which is exactly Ahead == 0.
	Merged bool `json:"merged"`
	Ahead  int  `json:"ahead"`
	Behind int  `json:"behind"`
}

// worktreeJSON is the serialized shape every command emits for a worktree.
type worktreeJSON struct {
	Group    string  `json:"group"`
	Repo     string  `json:"repo"`
	Name     string  `json:"name"`
	Branch   string  `json:"branch"`
	Path     string  `json:"path"`
	Detached bool    `json:"detached"`
	Head     string  `json:"head,omitempty"`
	Upstream *string `json:"upstream"`
	Ahead    int     `json:"ahead"`
	Behind   int     `json:"behind"`
	// UpstreamAsOf is when the remote refs this comparison used were last fetched, from
	// the bare repository's FETCH_HEAD. Ahead and Behind are computed against CACHED
	// refs — hydra never fetches to answer a query — so `behind: 0` means "not behind as
	// of this moment", not "up to date now". Reporting the count without the timestamp
	// stated a cached answer as a current one, which an agent correctly called dishonest.
	// Null means the remote has never been fetched in this workspace.
	UpstreamAsOf *string `json:"upstream_as_of"`
	Dirty        bool    `json:"dirty"`
	Changes      int     `json:"changes"`
	// DefaultBranch is the repository's default, and OnDefaultBranch says whether this
	// worktree is on it. Together they explain the directory name: the worktree on the
	// default branch gets the bare alias, every other gets `alias-<branch-slug>`. Two
	// agents independently concluded the suffix was decided by the ORDER of --branches,
	// because in every test the default happened to be listed first — their data could
	// not distinguish the two rules. These fields make it observable rather than inferred.
	DefaultBranch   string `json:"default_branch,omitempty"`
	OnDefaultBranch bool   `json:"on_default_branch"`
	// Topic is the recorded topic this worktree belongs to, nil when unassigned.
	// Unassigned is a permanent, first-class state — not a missing value.
	Topic *string `json:"topic"`
	// Against reports the comparison a caller asked for with --against REF, kept
	// SEPARATE from ahead/behind rather than overwriting them: "ahead of my upstream"
	// and "ahead of the release branch" are different questions, and a consumer that
	// asked one must still be able to see the other.
	Against  *againstJSON `json:"against,omitempty"`
	Locked   bool         `json:"locked,omitempty"`
	Prunable bool         `json:"prunable,omitempty"`
}

func (w worktreeContext) json() worktreeJSON {
	return worktreeJSON{
		Group:    w.RepoContext.Group,
		Repo:     w.RepoContext.Alias,
		Name:     w.DirName,
		Branch:   w.Branch,
		Path:     w.Path,
		Detached: w.Detached,
		Head:     w.Head,
		Locked:   w.Locked,
		Prunable: w.Prunable,

		DefaultBranch:   w.RepoContext.DefaultBranch,
		OnDefaultBranch: w.Branch != "" && w.Branch == w.RepoContext.DefaultBranch,
		UpstreamAsOf:    fetchedAt(w.RepoContext.BareRepo),
	}
}

// fetchedAt reports when a bare repository last fetched, from FETCH_HEAD's mtime.
//
// This is what makes `behind` honest. hydra never fetches to answer a query, so the count
// is computed against whatever remote refs are on disk; without a timestamp beside it,
// `behind: 0` reads as "up to date" when it means "not behind as of some earlier moment".
// Nil means this remote has never been fetched here.
func fetchedAt(bareRepo string) *string {
	if bareRepo == "" {
		return nil
	}
	info, err := os.Stat(filepath.Join(bareRepo, "FETCH_HEAD"))
	if err != nil {
		return nil
	}
	stamp := info.ModTime().UTC().Format(time.RFC3339)
	return &stamp
}

// withTracking fills the upstream/ahead/behind/dirty fields from git.
func (w worktreeContext) withTracking() (worktreeJSON, error) {
	item := w.json()
	status, err := git.CheckWorktreeStatus(w.Path)
	if err != nil {
		return item, err
	}
	if status.Upstream != "" {
		upstream := status.Upstream
		item.Upstream = &upstream
	}
	item.Ahead = status.CommitsAhead
	item.Behind = status.CommitsBehind
	item.Dirty = status.HasChanges
	item.Changes = status.ChangeCount
	return item, nil
}

// The selector flags, shared by every command that takes them so a flag cannot
// drift in name or meaning between commands.
var (
	topicFilter string
	reposFilter []string
	groupFilter string
	stateFilter []string

	// againstRef is registered only on the read commands. It is NOT part of
	// registerSelectorFlags because it does not narrow anything, and offering
	// "--against" on a creating command like start would be meaningless.
	againstRef string
)

// currentSelector snapshots the selector flags as parsed for this invocation.
func currentSelector() Selector {
	return Selector{
		Topic:   topicFilter,
		Repos:   reposFilter,
		Group:   groupFilter,
		Filter:  stateFilter,
		Against: againstRef,
	}
}

// registerSelectorFlags declares the selector surface on a command. It is one
// function so a command cannot accidentally offer a subset.
func registerSelectorFlags(flags *pflag.FlagSet) {
	flags.StringVar(&topicFilter, "topic", "", "Only worktrees recorded in this topic")
	flags.StringSliceVar(&reposFilter, "repos", nil, "Only these repositories (repeatable, comma-separated)")
	flags.StringVar(&groupFilter, "group", "", "Only worktrees in this group")
	flags.StringArrayVar(&stateFilter, "filter", nil,
		"Narrow by state: dirty, behind, or branch:<glob> (repeatable)")
}

// registerAgainstFlag adds --against to a read-only command.
func registerAgainstFlag(flags *pflag.FlagSet) {
	flags.StringVar(&againstRef, "against", "",
		"Also report ahead/behind against this ref, and whether the branch is merged into it")
}

// mustCollectWorktrees collects worktrees for an error detail, discarding warnings.
//
// Used only to populate an "available" list on a needs_input error: if collection
// partly failed, an incomplete list of valid values is still more useful to the caller
// than none, and the command is failing anyway.
func mustCollectWorktrees() []worktreeContext {
	items, _ := collectWorktrees(cfg, projectRoot)
	return items
}

// topicIndex is an in-memory (repo, branch) -> topic lookup.
//
// It exists so a listing reads and parses the state file ONCE instead of once per
// worktree. A nil index is valid and reports everything as unassigned, which is
// what makes membership decoration safe on commands that never load a project.
type topicIndex map[string]string

func topicKey(repo, branch string) string { return repo + "\x00" + branch }

// newTopicIndex reads recorded membership once for one project root.
//
// The root is explicit rather than taken from the global because `list`/`status`
// can span several registered projects in one invocation, and each has its own
// state file. A missing state file is not an error: a workspace that has never
// used topics reports every worktree unassigned, which is the correct answer.
func newTopicIndex(root string) (topicIndex, error) {
	all, err := topic.Open(root).List()
	if err != nil {
		return nil, classifyTopicErr(err)
	}
	idx := make(topicIndex)
	for _, t := range all {
		for _, m := range t.Members {
			idx[topicKey(m.Repo, m.Branch)] = t.ID
		}
	}
	return idx, nil
}

// decorate stamps recorded membership onto an item. Detached worktrees can never
// be members: membership is keyed by branch, and a detached HEAD has none.
func (idx topicIndex) decorate(item *worktreeJSON) {
	if idx == nil || item.Detached {
		return
	}
	if id, ok := idx[topicKey(item.Repo, item.Branch)]; ok {
		item.Topic = &id
	}
}

// requireTopicInTargets validates a topic id against every project being operated
// on, not just the active one.
//
// An empty id is a no-op: no filter was requested. Unreadable state in one project
// does not veto the others — it only matters if NO target has the id, in which case
// the read error is the more useful thing to report.
func requireTopicInTargets(targets []projectTarget, id string) error {
	if id == "" {
		return nil
	}
	var known []string
	var readErr error
	for _, target := range targets {
		names, err := topic.Open(target.Root).Names()
		if err != nil {
			if readErr == nil {
				readErr = err
			}
			continue
		}
		for _, name := range names {
			if name == id {
				return nil
			}
		}
		known = append(known, names...)
	}
	if readErr != nil && len(known) == 0 {
		return classifyTopicErr(readErr)
	}
	sort.Strings(known)
	return output.Errorf(output.CodeTopicUnknown,
		"topic %q is not known; run \"hydra topic list\" to see active topics", id).
		WithDetail("topic", id).
		WithDetail("known", known)
}

// repoContextFor builds a repoContext, resolving the effective default branch
// from config first and origin/HEAD second.
func repoContextFor(cfg *config.Config, projectRoot string, ref config.RepoRef) repoContext {
	repo := repoContext{
		Group:         ref.Group,
		Alias:         ref.Alias,
		Remote:        ref.Repo.Remote,
		DefaultBranch: ref.Repo.DefaultBranch,
		BareRepo:      cfg.BarePath(projectRoot, ref.Alias),
	}
	if repo.DefaultBranch == "" {
		if branch, err := git.GetRemoteDefaultBranch(repo.BareRepo); err == nil {
			repo.DefaultBranch = branch
		}
	}
	return repo
}

// resolveRepoByAlias looks an alias up across every group.
func resolveRepoByAlias(cfg *config.Config, projectRoot, alias string) (repoContext, error) {
	ref, ok := cfg.FindRepo(alias)
	if !ok {
		return repoContext{}, output.Errorf(output.CodeRepoUnknown,
			"unknown repo alias %q; run \"hydra list\" to see registered repos", alias).
			WithDetail("alias", alias)
	}
	repo := repoContextFor(cfg, projectRoot, ref)
	if _, err := os.Stat(repo.BareRepo); err != nil {
		return repoContext{}, output.Errorf(output.CodeBareMissing,
			"bare repository for %q not found at %s", alias, repo.BareRepo).
			WithDetail("alias", alias).
			WithDetail("bare_path", repo.BareRepo)
	}
	return repo, nil
}

// allRepoContexts returns every registered repo, ordered by group then alias.
func allRepoContexts(cfg *config.Config, projectRoot string) []repoContext {
	refs := cfg.Repos()
	repos := make([]repoContext, 0, len(refs))
	for _, ref := range refs {
		repos = append(repos, repoContextFor(cfg, projectRoot, ref))
	}
	return repos
}

// slugBranch renders a branch name usable as a single directory segment. Case is
// preserved; only path separators are folded.
func slugBranch(branch string) string {
	slug := strings.ReplaceAll(branch, "/", "-")
	slug = strings.ReplaceAll(slug, string(os.PathSeparator), "-")
	return strings.Trim(slug, "-")
}

// worktreeDirName derives the sibling directory name for a branch: the bare alias
// for the repo's default branch, alias-slug otherwise.
func worktreeDirName(repo repoContext, branch string) string {
	if branch == "" || branch == repo.DefaultBranch {
		return repo.Alias
	}
	if repo.DefaultBranch == "" && (branch == "main" || branch == "master") {
		return repo.Alias
	}
	return repo.Alias + "-" + slugBranch(branch)
}

// worktreePath is the only place a worktree path is composed, and it is used
// solely to decide where a NEW worktree goes. Existing worktrees always report
// their path through git.
func worktreePath(projectRoot, group, dirName string) string {
	return filepath.Join(projectRoot, group, dirName)
}

// listRepoWorktrees returns a repo's worktrees straight from git.
func listRepoWorktrees(repo repoContext) ([]worktreeContext, error) {
	infos, err := git.ListWorktrees(repo.BareRepo)
	if err != nil {
		return nil, output.Wrap(output.CodeGitFailed, err, "failed to list worktrees for %q", repo.Alias)
	}

	items := make([]worktreeContext, 0, len(infos))
	for _, info := range infos {
		if info.IsBare {
			continue
		}
		items = append(items, worktreeContext{
			RepoContext: repo,
			Branch:      info.Branch,
			Detached:    info.Detached || info.Branch == "",
			DirName:     filepath.Base(info.Path),
			Path:        info.Path,
			Head:        info.Head,
			Locked:      info.Locked,
			Prunable:    info.Prunable,
		})
	}
	return items, nil
}

// collectWorktrees gathers every worktree in a project. Per-repo failures become
// warnings rather than silently vanishing.
func collectWorktrees(cfg *config.Config, projectRoot string) ([]worktreeContext, []string) {
	var items []worktreeContext
	var warnings []string

	for _, repo := range allRepoContexts(cfg, projectRoot) {
		if _, err := os.Stat(repo.BareRepo); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %s/%s: bare repository missing at %s",
				output.CodeBareMissing, repo.Group, repo.Alias, repo.BareRepo))
			continue
		}
		worktrees, err := listRepoWorktrees(repo)
		if err != nil {
			// Prefix the hydra code. Raw git text reached callers verbatim and in the
			// system locale — `fatal: cannot change to '…': Arquivo ou diretório
			// inexistente` — which nothing downstream can match on. The git message is
			// kept because it names the real cause; it just no longer arrives uncoded.
			warnings = append(warnings, fmt.Sprintf("%s: %s/%s: %v",
				output.CodeGitFailed, repo.Group, repo.Alias, err))
			continue
		}
		items = append(items, worktrees...)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].RepoContext.Group != items[j].RepoContext.Group {
			return items[i].RepoContext.Group < items[j].RepoContext.Group
		}
		return items[i].DirName < items[j].DirName
	})

	return items, warnings
}

// resolveCurrentHydraContext identifies the worktree containing wd by matching it
// against the real paths git reports.
func resolveCurrentHydraContext(wd string, cfg *config.Config, projectRoot string) *worktreeContext {
	items, _ := collectWorktrees(cfg, projectRoot)

	var best *worktreeContext
	for i := range items {
		if !isWithinPath(wd, items[i].Path) {
			continue
		}
		// Prefer the deepest matching worktree.
		if best == nil || len(items[i].Path) > len(best.Path) {
			best = &items[i]
		}
	}
	return best
}

// findRepoWorktreeByBranch locates a repo's worktree for an exact branch.
func findRepoWorktreeByBranch(repo repoContext, branch string) (worktreeContext, bool) {
	items, err := listRepoWorktrees(repo)
	if err != nil {
		return worktreeContext{}, false
	}
	for _, item := range items {
		if item.Branch != "" && item.Branch == branch {
			return item, true
		}
	}
	return worktreeContext{}, false
}

func findSimilarWorktreesByName(cfg *config.Config, projectRoot, query string) []string {
	items, _ := collectWorktrees(cfg, projectRoot)

	needle := strings.ToLower(strings.TrimSpace(query))
	var matches []string
	for _, item := range items {
		if needle == "" || strings.Contains(strings.ToLower(item.DirName), needle) ||
			(item.Branch != "" && strings.Contains(strings.ToLower(item.Branch), needle)) {
			matches = append(matches, item.Qualified())
		}
	}
	if len(matches) > 5 {
		matches = matches[:5]
	}
	return matches
}

// checkWorktreeNameConflict refuses to reuse a directory that belongs to another
// branch. Auto-suffixing would produce a surprise directory name, which is worse
// than a clear failure.
func checkWorktreeNameConflict(repo repoContext, projectRoot, dirName, branch string) error {
	target := worktreePath(projectRoot, repo.Group, dirName)

	existing, err := listRepoWorktrees(repo)
	if err != nil {
		return err
	}
	for _, item := range existing {
		if !samePath(item.Path, target) {
			continue
		}
		if item.Branch == branch {
			return output.Errorf(output.CodeWorktreeExists,
				"worktree for branch %q already exists at %s", branch, item.Path).
				WithDetail("branch", branch).
				WithDetail("path", item.Path)
		}
		return output.Errorf(output.CodeWorktreeNameConflict,
			"directory %s is already the worktree for branch %q, not %q; pass --as <name> to choose a different directory",
			target, item.BranchLabel(), branch).
			WithDetail("path", target).
			WithDetail("existing_branch", item.Branch).
			WithDetail("requested_branch", branch)
	}

	if _, statErr := os.Stat(target); statErr == nil {
		return output.Errorf(output.CodeWorktreeNameConflict,
			"directory %s already exists but is not a registered worktree; remove it or pass --as <name>", target).
			WithDetail("path", target).
			WithDetail("requested_branch", branch)
	}

	// Same branch already checked out under a different directory name.
	for _, item := range existing {
		if branch != "" && item.Branch == branch {
			return output.Errorf(output.CodeWorktreeExists,
				"branch %q is already checked out at %s", branch, item.Path).
				WithDetail("branch", branch).
				WithDetail("path", item.Path)
		}
	}

	return nil
}

// resolveAddBaseBranch picks the base ref for a brand-new branch, in order:
// --from, defaults.base_branch, the repo's default_branch, origin/HEAD.
func resolveAddBaseBranch(cfg *config.Config, repo repoContext, from string) (string, error) {
	for _, candidate := range []string{from, cfg.Defaults.BaseBranch, repo.DefaultBranch} {
		if candidate == "" {
			continue
		}
		ref, err := git.ResolveBaseRef(repo.BareRepo, candidate)
		if err == nil {
			return ref, nil
		}
		if candidate == from {
			return "", output.Errorf(output.CodeBranchUnknown,
				"base branch %q does not exist in %q", from, repo.Alias).
				WithDetail("branch", from).
				WithDetail("repo", repo.Alias)
		}
	}

	if branch, err := git.GetRemoteDefaultBranch(repo.BareRepo); err == nil {
		if ref, refErr := git.ResolveBaseRef(repo.BareRepo, branch); refErr == nil {
			return ref, nil
		}
	}

	return "", output.Errorf(output.CodeBranchUnknown,
		"cannot determine a base branch for %q; set defaults.base_branch or pass --from", repo.Alias).
		WithDetail("repo", repo.Alias)
}

// createWorktreeForBranch creates the worktree and places its carried files.
//
// It returns the carry warnings rather than stashing them in package state: start, apply and
// clone create worktrees CONCURRENTLY through fanout, so a shared collector would be a data
// race. Callers that report per-item warnings already have somewhere to put these.
func createWorktreeForBranch(cfg *config.Config, repo repoContext, targetPath, branch, from string) ([]string, error) {
	if err := createWorktreeOnly(cfg, repo, targetPath, branch, from); err != nil {
		return nil, err
	}
	// Carry runs here rather than in each of the six callers, and BEFORE post_add: a hook
	// that installs dependencies or starts a service can then rely on the configuration
	// being in place. A missing source is a warning, never a failure — see internal/carry.
	_, warnings := carry.Apply(
		config.ResolveCarry(cfg, repo.Alias),
		carry.Plan{
			WorktreePath:   targetPath,
			SourceWorktree: carrySourceWorktree(repo, from),
			WorkspaceRoot:  projectRoot,
		},
	)
	return warnings, nil
}

// carrySourceWorktree picks where bare-form entries copy from: the worktree of the branch
// --from named when it has one, otherwise the repo's default-branch worktree. Both are
// deterministic. It never searches other worktrees — searching is guessing, and the whole
// point of a declaration is that it does not guess.
func carrySourceWorktree(repo repoContext, from string) string {
	if from != "" {
		if wt, ok := findRepoWorktreeByBranch(repo, from); ok {
			return wt.Path
		}
	}
	if repo.DefaultBranch != "" {
		if wt, ok := findRepoWorktreeByBranch(repo, repo.DefaultBranch); ok {
			return wt.Path
		}
	}
	return ""
}

func createWorktreeOnly(cfg *config.Config, repo repoContext, targetPath, branch, from string) error {
	kind, err := git.ClassifyBranch(repo.BareRepo, branch)
	if err != nil {
		return output.Wrap(output.CodeGitFailed, err, "failed to classify branch %q", branch)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0750); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to create group directory")
	}

	switch kind {
	case git.BranchRemote, git.BranchBoth:
		if err := git.AddWorktreeTracking(repo.BareRepo, targetPath, branch); err != nil {
			return output.Wrap(output.CodeGitFailed, err, "failed to create tracking worktree for %q", branch)
		}
	case git.BranchLocal:
		if err := git.AddWorktreeExistingLocal(repo.BareRepo, targetPath, branch); err != nil {
			return output.Wrap(output.CodeGitFailed, err, "failed to create worktree for local branch %q", branch)
		}
	default:
		baseRef, baseErr := resolveAddBaseBranch(cfg, repo, from)
		if baseErr != nil {
			return baseErr
		}
		if err := git.AddWorktreeNewBranch(repo.BareRepo, targetPath, branch, baseRef); err != nil {
			return output.Wrap(output.CodeGitFailed, err, "failed to create worktree for new branch %q from %s", branch, baseRef)
		}
	}
	return nil
}

// hooksContextFor builds the hook environment for a worktree operation.
func hooksContextFor(repo repoContext, branch, worktreePath string) hooks.Context {
	return hooks.Context{
		Group:        repo.Group,
		Repo:         repo.Alias,
		Branch:       branch,
		WorktreePath: worktreePath,
		BarePath:     repo.BareRepo,
	}
}

func branchChoicesForRepo(repo repoContext) ([]branchChoice, string, error) {
	branches, err := git.GetRemoteBranchesFromBare(repo.BareRepo)
	if err != nil {
		return nil, "", output.Wrap(output.CodeGitFailed, err, "failed to list branches for %q", repo.Alias)
	}

	worktrees, err := listRepoWorktrees(repo)
	if err != nil {
		return nil, "", err
	}

	worktreeByBranch := make(map[string]worktreeContext, len(worktrees))
	for _, wt := range worktrees {
		if wt.Branch != "" {
			worktreeByBranch[wt.Branch] = wt
		}
	}

	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = git.GetDefaultBranch(branches)
	}

	choices := make([]branchChoice, 0, len(branches))
	seen := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		if _, ok := seen[branch.Name]; ok {
			continue
		}
		seen[branch.Name] = struct{}{}

		choice := branchChoice{
			Name:            branch.Name,
			DisplayName:     branch.Name,
			IsRemoteDefault: branch.Name == defaultBranch,
		}
		if wt, ok := worktreeByBranch[branch.Name]; ok {
			choice.HasWorktree = true
			choice.WorktreeName = wt.DirName
			choice.WorktreePath = wt.Path
			choice.DisplayName = fmt.Sprintf("%s (worktree: %s)", branch.Name, wt.DirName)
		}
		if choice.IsRemoteDefault {
			choice.DisplayName += " (default)"
		}
		choices = append(choices, choice)
	}

	sort.Slice(choices, func(i, j int) bool {
		if choices[i].IsRemoteDefault != choices[j].IsRemoteDefault {
			return choices[i].IsRemoteDefault
		}
		return choices[i].Name < choices[j].Name
	})

	return choices, defaultBranch, nil
}

func navigationHints(wd string, wt worktreeContext) (string, string) {
	cdPath := wt.Path
	if rel, err := filepath.Rel(wd, wt.Path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
		cdPath = rel
	}
	return "cd " + cdPath, "hydra switch " + wt.DirName
}

func isWithinPath(wd, root string) bool {
	wd = filepath.Clean(wd)
	root = filepath.Clean(root)
	if wd == root {
		return true
	}
	return strings.HasPrefix(wd, root+string(os.PathSeparator))
}

// isPathInside is the child/parent-ordered alias used by the lifecycle commands.
func isPathInside(child, parent string) bool {
	return isWithinPath(child, parent)
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	// Resolve symlinked temp dirs (/tmp -> /private/tmp on macOS) before giving up.
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(ra) == filepath.Clean(rb)
}
