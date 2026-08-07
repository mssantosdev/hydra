package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mssantosdev/hydra/internal/branchresolve"

	"github.com/mssantosdev/hydra/internal/fanout"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var (
	startBranch   string
	startSlug     string
	startKind     string
	startUser     string
	startFrom     string
	startAll      bool
	startDryRun   bool
	startNoAssign bool
)

var startCmd = &cobra.Command{
	Use:   "start [<branch>]",
	Short: "Create worktrees for a unit of work across repositories",
	Long: `Create a worktree per repository for one unit of work.

DESCRIPTION
  start is "add" for several repositories at once, with an optional topic recording
  which worktrees belong together. Without --topic it behaves exactly like add: the
  worktrees are unassigned, which is a permanent and first-class state.

  It is CONVERGENT. A worktree that already exists for the requested branch is
  reported as skipped, not as a failure, so re-running the same command to make sure
  it landed is safe and exits 0.

BRANCH NAME, highest precedence first
  1. the positional <branch>          — a LITERAL name; the pattern never runs
  2. --branch <name>
  3. the unanimous branch of --topic's existing members (extend needs no flags)
  4. repos.<alias>.branch_provider  / defaults.branch_provider
  5. repos.<alias>.branch_pattern   / defaults.branch_pattern

  There is no sixth level. With none of these available start returns needs_input
  naming --branch, because guessing a branch name is worse than asking for one.

  The surprising case, stated plainly: "hydra start 2072958" with a pattern
  configured treats 2072958 as a literal branch name. To generate from the pattern,
  pass --topic 2072958 --slug login with NO positional branch.

WHICH REPOSITORIES
  --repos, --group or --all select them. With --topic and no selector, the topic's
  existing members are used. With neither, start returns needs_input: it never
  silently targets every repository in the workspace.

EXAMPLES
  # one branch across two repos, recorded as a topic
  $ hydra start marcus/feat-login --repos api,web --topic 2072958

  # extend that topic to another repo, reusing the members' branch
  $ hydra start --topic 2072958 --repos billing

  # generate the name from defaults.branch_pattern
  $ hydra start --topic 2072958 --slug login --kind feat --repos api

  # exactly like add: one repo, no topic
  $ hydra start feat/spike --repos api

EXIT CODES
  0  Success (including a fully converged no-op)
  1  branch_unknown, git_failed, topic_conflict, branch_provider_failed
  2  not_in_project, state_version_unsupported
  4  partial_failure (some repositories failed)
  6  busy (state or git lock held; retry)
  7  needs_input (details.missing / details.one_of name the flags)

SEE ALSO
  • hydra add          - a single worktree in one repository
  • hydra topic show   - what a topic contains
  • hydra list --topic - the worktrees in a topic`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
	registerSelectorFlags(startCmd.Flags())
	startCmd.Flags().StringVar(&startBranch, "branch", "", "Branch name (a positional branch wins)")
	startCmd.Flags().StringVar(&startSlug, "slug", "", "Value for {slug} in branch_pattern")
	startCmd.Flags().StringVar(&startKind, "kind", "", "Value for {kind} in branch_pattern")
	startCmd.Flags().StringVar(&startUser, "user", "", "Override {user}; defaults to git config user.name")
	startCmd.Flags().StringVar(&startFrom, "from", "", "Base ref for a brand-new branch")
	startCmd.Flags().BoolVar(&startAll, "all", false, "Target every registered repository")
	startCmd.Flags().BoolVar(&startDryRun, "dry-run", false, "Report what would happen and change nothing")
	startCmd.Flags().BoolVar(&startNoAssign, "no-assign", false,
		"Create the worktrees without recording topic membership")
}

type startTargetJSON struct {
	Group  string `json:"group"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	// Name is the worktree directory name, which is the HANDLE every other command
	// takes. Without it a caller has to take the basename of Path to address the
	// worktree it just created.
	Name        string `json:"name"`
	Path        string `json:"path"`
	Disposition string `json:"disposition"`
	Reason      string `json:"reason,omitempty"`
	Attached    bool   `json:"attached"`
	Error       string `json:"error,omitempty"`
}

type startJSON struct {
	Topic *string `json:"topic"`
	// Branch and BranchSource explain WHICH name was chosen and why, so a caller
	// never has to re-derive the precedence chain to understand the result.
	Branch       string            `json:"branch"`
	BranchSource string            `json:"branch_source"`
	DryRun       bool              `json:"dry_run"`
	Created      []startTargetJSON `json:"created"`
	Skipped      []startTargetJSON `json:"skipped"`
	Failed       []startTargetJSON `json:"failed"`
}

func runStart(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	positional := ""
	if len(args) == 1 {
		positional = strings.TrimSpace(args[0])
	}
	topicID := strings.TrimSpace(topicFilter)

	// The two axes resolve INDEPENDENTLY, and either one being unresolvable is
	// needs_input naming that specific flag. Resolving repos first means a missing
	// selector is reported before a subprocess is spawned for the branch provider.
	repos, existing, err := resolveStartRepos(topicID)
	if err != nil {
		return err
	}

	branch, source, err := resolveStartBranch(positional, topicID, existing, repos)
	if err != nil {
		return err
	}

	payload := startJSON{Branch: branch, BranchSource: string(source), DryRun: startDryRun}
	if topicID != "" && !startNoAssign {
		payload.Topic = &topicID
	}

	targets := make([]fanout.Target, 0, len(repos))
	for _, repo := range repos {
		targets = append(targets, fanout.Target{
			Group:    repo.Group,
			Repo:     repo.Alias,
			Branch:   branch,
			Path:     worktreePath(projectRoot, repo.Group, worktreeDirName(repo, branch)),
			BareRepo: repo.BareRepo,
		})
	}

	if startDryRun {
		return emitStartPreview(cmd, payload, targets)
	}

	reposByAlias := make(map[string]repoContext, len(repos))
	for _, repo := range repos {
		reposByAlias[repo.Alias] = repo
	}

	// SerialPerRepo is TRUE: creation contends on the bare repo's config.lock, and
	// concurrent adds were measured to leave worktrees with no upstream at all.
	results := fanout.Run(context.Background(), targets, fanout.Config{
		SerialPerRepo: true,
		Hook: func(_ context.Context, t fanout.Target) ([]string, error) {
			repo := reposByAlias[t.Repo]
			result, hookErr := runHookEvent("post_add", hooksContextFor(repo, t.Branch, t.Path), t.Path)
			return result.Warnings, hookErr
		},
	}, func(_ context.Context, t fanout.Target) fanout.ItemResult {
		return startOne(reposByAlias[t.Repo], t)
	})

	var warnings []string
	for _, result := range results {
		warnings = append(warnings, result.HookWarnings...)
	}

	// Membership is recorded AFTER the worktree exists. Recording first and then
	// failing to create would leave a member with no worktree — the same dangling
	// state a detach-first removal produces, and just as invisible.
	attachWarnings := attachStartResults(topicID, results, &payload)
	warnings = append(warnings, attachWarnings...)

	fillStartPayload(&payload, results)
	_, _, failed, _ := fanout.Summarize(results)

	summary := startSummary(payload, source)

	// start already distinguished total from partial failure; the error now rides the
	// same envelope as the created worktrees instead of a second one on stderr.
	var startErr *output.Error
	outcome := output.OutcomeSuccess
	switch {
	case len(failed) == 0:
	case len(payload.Created) == 0 && len(payload.Skipped) == 0:
		outcome = output.OutcomeFailure
		startErr = output.Errorf(output.CodeGitFailed,
			"no worktree could be created for %q", branch).
			WithDetail("branch", branch).
			WithDetail("failed", len(failed))
	default:
		outcome = output.OutcomePartial
		startErr = output.Errorf(output.CodePartialFailure,
			"%d of %d repositories failed", len(failed), len(results)).
			WithDetail("branch", branch).
			WithDetail("failed", len(failed))
	}

	if emitErr := emitResult(cmd, output.Result{
		Outcome:  outcome,
		Summary:  summary,
		Data:     payload,
		Next:     startNext(payload),
		Warnings: warnings,
		Err:      startErr,
	}, func() { printStartText(payload, summary) }); emitErr != nil {
		return emitErr
	}
	if startErr != nil {
		return startErr
	}
	return nil
}

// startOne converges one repository.
func startOne(repo repoContext, t fanout.Target) fanout.ItemResult {
	dirName := worktreeDirName(repo, t.Branch)

	// Convergence: an existing worktree for THIS branch is the desired state. Only a
	// directory held by a DIFFERENT branch is a conflict.
	if err := checkWorktreeNameConflict(repo, projectRoot, dirName, t.Branch); err != nil {
		if output.Classify(err).Code == output.CodeWorktreeExists {
			return fanout.ItemResult{Disposition: fanout.Skipped, Reason: "already present"}
		}
		return fanout.ItemResult{Disposition: fanout.Failed, Reason: output.Classify(err).Message, Err: err}
	}
	carried, err := createWorktreeForBranch(cfg, repo, t.Path, t.Branch, startFrom)
	if err != nil {
		return fanout.ItemResult{Disposition: fanout.Failed, Reason: output.Classify(err).Message, Err: err}
	}
	return fanout.ItemResult{Disposition: fanout.Created, Reason: "created", HookWarnings: carried}
}

// resolveStartRepos decides which repositories to target.
//
// It never silently means "every repository": creating a worktree in every repo of a
// workspace is not a plausible accident to allow, so with no selector and no topic
// members the caller is asked, naming all three flags that would do.
func resolveStartRepos(topicID string) ([]repoContext, topic.Topic, error) {
	var existing topic.Topic
	if topicID != "" {
		found, ok, err := topicStore().Get(topicID)
		if err != nil {
			return nil, existing, classifyTopicErr(err)
		}
		if ok {
			existing = found
		}
	}

	selector := currentSelector()
	hasSelector := len(selector.Repos) > 0 || selector.Group != "" || startAll

	if hasSelector {
		repos, err := reposForSelector(selector)
		if err != nil {
			return nil, existing, err
		}
		return repos, existing, nil
	}

	// No selector: fall back to the topic's existing members, which is what makes
	// "hydra start --topic X" extend a topic with no flags at all.
	if len(existing.Members) > 0 {
		aliases := make([]string, 0, len(existing.Members))
		seen := make(map[string]struct{}, len(existing.Members))
		for _, member := range existing.Members {
			if _, ok := seen[member.Repo]; ok {
				continue
			}
			seen[member.Repo] = struct{}{}
			aliases = append(aliases, member.Repo)
		}
		repos, err := reposForSelector(Selector{Repos: aliases})
		return repos, existing, err
	}

	return nil, existing, output.Errorf(output.CodeNeedsInput,
		"no repositories selected; pass --repos, --group or --all").
		WithDetail("one_of", []string{"--repos", "--group", "--all"}).
		WithDetail("reason", "a new topic has no members to infer repositories from")
}

// reposForSelector maps a selector onto repo contexts, validating its values so a
// typo is reported as a typo rather than as an empty result.
func reposForSelector(selector Selector) ([]repoContext, error) {
	session := currentSession()
	if err := validateRepos(session, selector.Repos); err != nil {
		return nil, err
	}
	if err := validateGroup(session, selector.Group); err != nil {
		return nil, err
	}

	wanted := lowerSet(selector.Repos)
	var out []repoContext
	for _, repo := range allRepoContexts(cfg, projectRoot) {
		if len(wanted) > 0 {
			if _, ok := wanted[strings.ToLower(repo.Alias)]; !ok {
				continue
			}
		}
		if selector.Group != "" && !strings.EqualFold(repo.Group, selector.Group) {
			continue
		}
		out = append(out, repo)
	}

	if len(out) == 0 {
		return nil, output.Errorf(output.CodeRepoUnknown,
			"the selector matched no registered repository").
			WithDetail("repos", selector.Repos).
			WithDetail("group", selector.Group)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Alias < out[j].Alias
	})
	return out, nil
}

// resolveStartBranch runs the precedence chain, mapping its errors onto the output
// enum so every call site reports the same codes.
func resolveStartBranch(positional, topicID string, existing topic.Topic, repos []repoContext) (string, branchresolve.Source, error) {
	memberBranches := make([]string, 0, len(existing.Members))
	for _, member := range existing.Members {
		memberBranches = append(memberBranches, member.Branch)
	}

	// The pattern and provider are per-repo, and a single start may span repos with
	// different ones. Resolving once against the FIRST target keeps one branch name
	// for the whole topic, which is the invariant a topic exists to hold.
	pattern, provider, strict := branchPolicyFor(repos)

	request := branchresolve.Request{
		Positional:     positional,
		Flag:           startBranch,
		MemberBranches: memberBranches,
		Pattern:        pattern,
		Provider:       provider,
		Strict:         strict,
		Topic:          topicID,
		Kind:           startKind,
		Slug:           startSlug,
		User:           resolveStartUser(repos),
		Project:        cfg.Project,
		ProjectRoot:    projectRoot,
	}
	if len(repos) > 0 {
		request.Repo = repos[0].Alias
		request.Group = repos[0].Group
	}

	resolution, err := branchresolve.Resolve(context.Background(), request)
	if err != nil {
		return "", "", classifyBranchErr(err, request)
	}
	return resolution.Branch, resolution.Source, nil
}

// branchPolicyFor returns the branch policy, preferring a repo-level override.
func branchPolicyFor(repos []repoContext) (pattern, provider string, strict bool) {
	pattern = cfg.Defaults.BranchPattern
	provider = cfg.Defaults.BranchProvider
	strict = cfg.Defaults.BranchPatternStrict

	if len(repos) == 0 {
		return pattern, provider, strict
	}
	if ref, ok := cfg.FindRepo(repos[0].Alias); ok {
		if ref.Repo.BranchPattern != "" {
			pattern = ref.Repo.BranchPattern
		}
		if ref.Repo.BranchProvider != "" {
			provider = ref.Repo.BranchProvider
		}
	}
	return pattern, provider, strict
}

// resolveStartUser fills {user} from git, honouring --user first.
func resolveStartUser(repos []repoContext) string {
	if strings.TrimSpace(startUser) != "" {
		return strings.TrimSpace(startUser)
	}
	dir := projectRoot
	if len(repos) > 0 {
		dir = repos[0].BareRepo
	}
	name, err := git.ConfigUserName(dir)
	if err != nil {
		return ""
	}
	return name
}

// classifyBranchErr maps branchresolve's errors onto the output enum.
func classifyBranchErr(err error, request branchresolve.Request) error {
	var needs *branchresolve.NeedsInputError
	if errors.As(err, &needs) {
		out := output.Errorf(output.CodeNeedsInput, "%s", needs.Error()).
			WithDetail("reason", needs.Reason)
		if len(needs.Missing) > 0 {
			out = out.WithDetail("missing", needs.Missing)
		}
		if len(needs.OneOf) > 0 {
			out = out.WithDetail("one_of", needs.OneOf)
		}
		return out
	}

	var provider *branchresolve.ProviderError
	if errors.As(err, &provider) {
		return output.Errorf(output.CodeBranchProviderFailed, "%s", provider.Error()).
			WithDetail("repo", provider.Repo).
			WithDetail("provider", provider.Provider).
			WithDetail("exit_code", provider.ExitCode).
			WithDetail("timed_out", provider.TimedOut).
			WithDetail("stderr", provider.Stderr)
	}

	var invalid *branchresolve.InvalidBranchError
	if errors.As(err, &invalid) {
		return output.Errorf(output.CodeBranchUnknown, "%s", invalid.Error()).
			WithDetail("branch", invalid.Branch).
			WithDetail("pattern", request.Pattern)
	}

	return output.Wrap(output.CodeInternal, err, "failed to resolve a branch name")
}

// attachStartResults records membership for every worktree that now exists.
func attachStartResults(topicID string, results []fanout.ItemResult, payload *startJSON) []string {
	if topicID == "" || startNoAssign {
		return nil
	}

	var warnings []string
	attached := make(map[string]bool, len(results))
	for _, result := range results {
		if result.Disposition == fanout.Failed {
			continue
		}
		member := topic.Member{Repo: result.Target.Repo, Branch: result.Target.Branch}
		if err := topicStore().Attach(topicID, member); err != nil {
			// The worktree exists and is correct; only the record failed. Reporting it
			// as a failure would claim the git work did not happen.
			warnings = append(warnings, fmt.Sprintf("%s: worktree created but not recorded in topic %q: %v",
				result.Target.Repo, topicID, output.Classify(classifyTopicErr(err)).Message))
			continue
		}
		attached[result.Target.Key()] = true
	}
	payload.Topic = &topicID
	startAttached = attached
	return warnings
}

// startAttached carries per-target attach outcomes into the payload without
// threading another parameter through the fan-out result loop.
var startAttached map[string]bool

func fillStartPayload(payload *startJSON, results []fanout.ItemResult) {
	for _, result := range results {
		entry := startTargetJSON{
			Group:       result.Target.Group,
			Repo:        result.Target.Repo,
			Branch:      result.Target.Branch,
			Name:        filepath.Base(result.Target.Path),
			Path:        result.Target.Path,
			Disposition: string(result.Disposition),
			Reason:      result.Reason,
			Attached:    startAttached[result.Target.Key()],
		}
		switch result.Disposition {
		case fanout.Created:
			payload.Created = append(payload.Created, entry)
		case fanout.Skipped:
			payload.Skipped = append(payload.Skipped, entry)
		case fanout.Failed:
			entry.Error = result.Reason
			payload.Failed = append(payload.Failed, entry)
		}
	}
	startAttached = nil
}

func startSummary(payload startJSON, source branchresolve.Source) string {
	parts := []string{fmt.Sprintf("branch %s (%s)", payload.Branch, source)}
	if len(payload.Created) > 0 {
		parts = append(parts, fmt.Sprintf("%d created", len(payload.Created)))
	}
	if len(payload.Skipped) > 0 {
		parts = append(parts, fmt.Sprintf("%d already present", len(payload.Skipped)))
	}
	if len(payload.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", len(payload.Failed)))
	}
	if payload.Topic != nil {
		parts = append(parts, "topic "+*payload.Topic)
	}
	return strings.Join(parts, ", ")
}

// startNext suggests the obvious follow-up. It is a suggestion only: hydra never
// acts on it.
func startNext(payload startJSON) []output.Next {
	if payload.Topic == nil {
		return nil
	}
	return []output.Next{{
		Argv: []string{"hydra", "status", "--topic", *payload.Topic},
		Why:  "see tracking and dirtiness for every worktree in this topic",
	}}
}

func emitStartPreview(cmd *cobra.Command, payload startJSON, targets []fanout.Target) error {
	for _, t := range targets {
		payload.Created = append(payload.Created, startTargetJSON{
			Group: t.Group, Repo: t.Repo, Branch: t.Branch,
			Name: filepath.Base(t.Path), Path: t.Path,
			Disposition: "would_create",
		})
	}
	summary := fmt.Sprintf("dry run: branch %s across %d repositor(y|ies)", payload.Branch, len(targets))

	return emit(cmd, summary, payload, nil, func() {
		fmt.Println()
		fmt.Println(styles.Title.Render(summary))
		fmt.Println()
		for _, target := range payload.Created {
			fmt.Printf("  %-20s %-28s %s\n", target.Repo, target.Branch, target.Path)
		}
		fmt.Println()
	})
}

func printStartText(payload startJSON, summary string) {
	fmt.Println()
	fmt.Println(styles.Success.Render("✓ " + summary))
	fmt.Println()
	for _, target := range payload.Created {
		fmt.Printf("  %s %-18s %s\n", styles.Success.Render("new "), target.Repo, target.Path)
	}
	for _, target := range payload.Skipped {
		fmt.Printf("  %s %-18s %s\n", styles.Label.Render("keep"), target.Repo, target.Path)
	}
	for _, target := range payload.Failed {
		fmt.Printf("  %s %-18s %s\n", styles.Error.Render("fail"), target.Repo, target.Error)
	}
	fmt.Println()
}
