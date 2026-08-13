package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mssantosdev/hydra/internal/branchresolve"
	"github.com/mssantosdev/hydra/internal/config"
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
	startParent   string
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
	startCmd.Flags().StringVar(&startParent, "parent", "",
		"Record this topic as contained by another (opt-in; without it the topic is flat)")
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
	Name        string             `json:"name"`
	Path        string             `json:"path"`
	Disposition string             `json:"disposition"`
	Reason      string             `json:"reason,omitempty"`
	Attached    bool               `json:"attached"`
	Error       *output.Diagnostic `json:"error,omitempty"`
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
		return errNotInProject()
	}

	positional := ""
	if len(args) == 1 {
		positional = strings.TrimSpace(args[0])
	}
	topicID := strings.TrimSpace(topicFilter)

	// The two axes resolve INDEPENDENTLY, and either one being unresolvable is
	// needs_input naming that specific flag. Resolving repos first means a missing
	// selector is reported before a subprocess is spawned for the branch provider.
	repos, existing, topicExisted, err := resolveStartRepos(topicID)
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
		return emitStartPreview(cmd, payload, targets, repos)
	}

	reposByAlias := make(map[string]repoContext, len(repos))
	for _, repo := range repos {
		reposByAlias[repo.Alias] = repo
	}

	// SerialPerRepo is TRUE: creation contends on the bare repo's config.lock, and
	// concurrent adds were measured to leave worktrees with no upstream at all.
	results := fanout.Run(context.Background(), targets, fanout.Config{
		SerialPerRepo: true,
		Hook: func(_ context.Context, t fanout.Target) ([]*output.Diagnostic, error) {
			repo := reposByAlias[t.Repo]
			result, hookErr := runHookEvent("post_add", hooksContextFor(repo, t.Branch, t.Path), t.Path)
			return result.Warnings, hookErr
		},
	}, func(_ context.Context, t fanout.Target) fanout.ItemResult {
		return startOne(reposByAlias[t.Repo], t)
	})

	var warnings []*output.Diagnostic
	for _, result := range results {
		warnings = append(warnings, result.HookWarnings...)
	}

	// Membership is recorded AFTER the worktree exists. Recording first and then
	// failing to create would leave a member with no worktree — the same dangling
	// state a detach-first removal produces, and just as invisible.
	attachWarnings := attachStartResults(topicID, results, &payload)
	warnings = append(warnings, attachWarnings...)

	// Containment and the once-per-topic event both come after membership: a parent recorded on a
	// topic with no members, or a "work started" notification for a topic that failed to create
	// anything, would both be announcing something that does not exist yet.
	if topicID != "" && startParent != "" {
		if err := topicStore().SetParent(topicID, startParent); err != nil {
			warnings = append(warnings, output.Warnf(output.CodeTopicConflict, "could not record parent %s: %v", startParent, err).
				WithSubject("topic", topicID).
				WithCause(err.Error()))
		}
	}
	if topicID != "" && !topicExisted && startCreatedCount(results) > 0 {
		// ONCE PER TOPIC. Once per worktree posts N times for one piece of work, which is how a
		// channel gets muted; once per INVOCATION announces a second start when a second
		// repository joins an existing topic, which double-creates whatever the hook creates on
		// the other side. So: this invocation, and only when it is the one that started the topic.
		//
		// The count comes from results rather than payload.Created, which is filled further down.
		// A guard reading a field populated after it can only ever be false.
		topicStart, topicErr := runHookEvent("post_topic_start", topicHookContext(topicID), projectRoot)
		warnings = append(warnings, topicStart.Warnings...)
		if topicErr != nil {
			warnings = append(warnings, output.Classify(topicErr))
		}
	}

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

	// Convergence: an existing worktree for THIS branch at THIS path is the desired state. A
	// directory held by a different branch, or this branch held under a different directory,
	// is a conflict.
	if err := checkWorktreeNameConflict(repo, projectRoot, dirName, t.Branch); err != nil {
		if worktreeAlreadyAtTarget(err, t.Path) {
			return fanout.ItemResult{Disposition: fanout.Skipped, Reason: "already present"}
		}
		return fanout.ItemResult{Disposition: fanout.Failed, Reason: output.Classify(err).Message, Err: err}
	}
	carried, err := createWorktreeForBranch(cfg, repo, t.Path, t.Branch, startFrom)
	if err != nil {
		return fanout.ItemResult{Disposition: fanout.Failed, Reason: output.Classify(err).Message, Err: err}
	}
	return fanout.ItemResult{Disposition: fanout.Created, Reason: "created", HookWarnings: carried.Warnings}
}

// resolveStartRepos decides which repositories to target.
//
// It never silently means "every repository": creating a worktree in every repo of a
// workspace is not a plausible accident to allow, so with no selector and no topic
// members the caller is asked, naming all three flags that would do.
func resolveStartRepos(topicID string) ([]repoContext, topic.Topic, bool, error) {
	var (
		existing topic.Topic
		existed  bool
	)
	if topicID != "" {
		found, ok, err := topicStore().Get(topicID)
		if err != nil {
			return nil, existing, false, classifyTopicErr(err)
		}
		if ok {
			existing, existed = found, true
		}
	}

	selector := currentSelector()
	hasSelector := len(selector.Repos) > 0 || selector.Group != "" || startAll

	if hasSelector {
		repos, err := reposForSelector(selector)
		if err != nil {
			return nil, existing, existed, err
		}
		return repos, existing, existed, nil
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
		return repos, existing, existed, err
	}

	return nil, existing, existed, output.Errorf(output.CodeNeedsInput,
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
	request := startBranchRequest(positional, topicID, existing, repos)

	resolution, err := branchresolve.ResolveUnlessConverged(context.Background(), request, func(branch string) bool {
		return startReposConverged(repos, branch)
	})
	if err != nil {
		return "", "", classifyBranchErr(err, request)
	}
	return resolution.Branch, resolution.Source, nil
}

func startBranchRequest(positional, topicID string, existing topic.Topic, repos []repoContext) branchresolve.Request {
	memberBranches := make([]string, 0, len(existing.Members))
	for _, member := range existing.Members {
		memberBranches = append(memberBranches, member.Branch)
	}

	// The pattern and provider are per-repo, and a single start may span repos with
	// different ones. Resolving once against the FIRST target keeps one branch name
	// for the whole topic, which is the invariant a topic exists to hold.
	pattern, provider, strict, timeout := branchPolicyFor(repos)

	request := branchresolve.Request{
		Positional:     positional,
		Flag:           startBranch,
		MemberBranches: memberBranches,
		Pattern:        pattern,
		Provider:       provider,
		Strict:         strict,
		Timeout:        timeout,
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
	return request
}

// startReposConverged reports whether every selected repository already has a worktree
// at the branch start would converge to.
func startReposConverged(repos []repoContext, branch string) bool {
	for _, repo := range repos {
		dirName := worktreeDirName(repo, branch)
		path := worktreePath(projectRoot, repo.Group, dirName)
		if err := checkWorktreeNameConflict(repo, projectRoot, dirName, branch); err != nil {
			if worktreeAlreadyAtTarget(err, path) {
				continue
			}
			return false
		}
		return false
	}
	return len(repos) > 0
}

// branchPolicyFor returns the branch policy for a set of worktrees.
//
// It resolves through the level chain, so a group's `branch_pattern` applies to every repo in
// it without each repo restating it.
//
// The first repo decides. A policy is a naming convention, so one branch name is being built for
// all of them; disagreeing repos would need N names, which `start` does not model.
func branchPolicyFor(repos []repoContext) (pattern, provider string, strict bool, timeout time.Duration) {
	alias := ""
	if len(repos) > 0 {
		alias = repos[0].Alias
	}
	d := config.ResolveDefaults(cfg, alias)
	pattern, provider, timeout = d.BranchNamingPolicy()
	return pattern, provider, d.BranchPatternStrict, timeout
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
func attachStartResults(topicID string, results []fanout.ItemResult, payload *startJSON) []*output.Diagnostic {
	if topicID == "" || startNoAssign {
		return nil
	}

	var warnings []*output.Diagnostic
	attached := make(map[string]bool, len(results))
	for _, result := range results {
		if result.Disposition == fanout.Failed {
			continue
		}
		member := topic.Member{Repo: result.Target.Repo, Branch: result.Target.Branch}
		if err := topicStore().Attach(topicID, member); err != nil {
			// The worktree exists and is correct; only the record failed. Reporting it
			// as a failure would claim the git work did not happen.
			warnings = append(warnings, output.Warnf(output.CodeTopicConflict,
				"%s: worktree created but not recorded in topic %q: %v",
				result.Target.Repo, topicID, output.Classify(classifyTopicErr(err)).Message).
				WithSubject("topic", topicID).
				WithCause(err.Error()))
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

// startCreatedCount counts the worktrees this invocation actually created.
//
// It reads the fanout results rather than the JSON payload, so where the payload is filled cannot
// silence the once-per-topic event.
func startCreatedCount(results []fanout.ItemResult) int {
	n := 0
	for _, result := range results {
		if result.Disposition == fanout.Created {
			n++
		}
	}
	return n
}

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
			if result.Err != nil {
				entry.Error = output.Classify(result.Err)
			} else {
				entry.Error = output.Errorf(output.CodeGitFailed, "%s", result.Reason)
			}
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

// startPreviewDisposition predicts one target, mirroring startOne's convergence rule: the branch
// already at this path is the desired state, the branch somewhere else is a conflict.
func startPreviewDisposition(repo repoContext, t fanout.Target) string {
	dirName := worktreeDirName(repo, t.Branch)
	if err := checkWorktreeNameConflict(repo, projectRoot, dirName, t.Branch); err != nil {
		if worktreeAlreadyAtTarget(err, t.Path) {
			return "skipped"
		}
		return "failed"
	}
	return "would_create"
}

// emitStartPreview reports what a real run would do.
//
// The disposition is COMPUTED, using the same check startOne runs. Reporting "would_create" for
// every target promised to create worktrees that already exist, so a preflight before a real run
// always looked like work and never like a no-op.
func emitStartPreview(cmd *cobra.Command, payload startJSON, targets []fanout.Target, repos []repoContext) error {
	byAlias := make(map[string]repoContext, len(repos))
	for _, repo := range repos {
		byAlias[repo.Alias] = repo
	}
	for _, t := range targets {
		entry := startTargetJSON{
			Group: t.Group, Repo: t.Repo, Branch: t.Branch,
			Name: filepath.Base(t.Path), Path: t.Path,
			Disposition: "would_create",
		}
		if repo, ok := byAlias[t.Repo]; ok {
			entry.Disposition = startPreviewDisposition(repo, t)
		}
		switch entry.Disposition {
		case "skipped":
			payload.Skipped = append(payload.Skipped, entry)
		case "failed":
			payload.Failed = append(payload.Failed, entry)
		default:
			payload.Created = append(payload.Created, entry)
		}
	}
	summary := fmt.Sprintf("dry run: branch %s across %d repo(s)", payload.Branch, len(targets))

	// Every bucket is printed, not just Created: once the preview predicts skips and failures, a
	// text renderer that loops Created alone silently drops the lines it just learned to compute.
	//
	// The outcome rides the envelope too, so a predicted failure exits non-zero. A preflight that
	// reports success for a document the real run refuses is the thing --dry-run exists to prevent.
	previewErr := startPreviewError(payload)
	emitErr := emitResult(cmd, output.Result{
		Summary: summary,
		Data:    payload,
		Outcome: startPreviewOutcome(payload),
		Err:     previewErr,
	}, func() {
		fmt.Println()
		fmt.Println(styles.Title.Render(summary))
		fmt.Println()
		for _, target := range payload.Created {
			fmt.Printf("  %s %-18s %s\n", styles.Success.Render("new "), target.Repo, target.Path)
		}
		for _, target := range payload.Skipped {
			fmt.Printf("  %s %-18s %s\n", styles.Label.Render("keep"), target.Repo, target.Path)
		}
		for _, target := range payload.Failed {
			fmt.Printf("  %s %-18s %s\n", styles.Error.Render("fail"), target.Repo, target.Path)
		}
		fmt.Println()
	})
	if emitErr != nil {
		return emitErr
	}
	// The envelope is written first, then the error is returned to set the exit status: a predicted
	// failure has to be readable AND non-zero, the same way the real run reports one.
	if previewErr != nil {
		return previewErr
	}
	return nil
}

// startPreviewOutcome reports the outcome a real run would produce, so --dry-run's exit status
// matches it.
func startPreviewOutcome(payload startJSON) output.Outcome {
	switch {
	case len(payload.Failed) == 0:
		return output.OutcomeSuccess
	case len(payload.Created) == 0 && len(payload.Skipped) == 0:
		return output.OutcomeFailure
	default:
		return output.OutcomePartial
	}
}

// startPreviewError names why a predicted run would not fully succeed.
func startPreviewError(payload startJSON) *output.Error {
	if len(payload.Failed) == 0 {
		return nil
	}
	code := output.CodeWorktreeNameConflict
	if len(payload.Created) > 0 || len(payload.Skipped) > 0 {
		code = output.CodePartialFailure
	}
	return output.Errorf(code, "%d of %d worktree(s) would fail",
		len(payload.Failed), len(payload.Created)+len(payload.Skipped)+len(payload.Failed)).
		WithDetail("failed", len(payload.Failed))
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
