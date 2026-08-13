package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	// defaultApplyMaxItems caps how many worktrees one document may request. Apply creates
	// one git worktree per item; realistic workspaces stay in the low hundreds, so five
	// hundred is a generous default and accidental megadocuments should be refused instead
	// of running unbounded git work.
	defaultApplyMaxItems = 500
	// defaultApplyMaxSize bounds bytes read from stdin or a file. A few hundred list-json
	// items are tens of kilobytes; one mebibyte leaves headroom for metadata while refusing
	// hostile multi-megabyte dumps before they are fully buffered.
	defaultApplyMaxSize = 1 << 20
)

var (
	applyDryRun   bool
	applyMaxItems int
	applyMaxSize  int
)

type applyLimits struct {
	maxItems int
	maxSize  int64
}

// applyWorktreeCache lists each repository's worktrees at most once per apply invocation.
// Without it, a large document would spawn one `git worktree list` per item and make
// --dry-run unusably slow even though no worktree is created.
type applyWorktreeCache struct {
	byRepo map[string][]worktreeContext
	errs   map[string]error
}

func (c *applyWorktreeCache) worktrees(repo repoContext) ([]worktreeContext, error) {
	key := repo.Group + "/" + repo.Alias
	if c.byRepo == nil {
		c.byRepo = make(map[string][]worktreeContext)
		c.errs = make(map[string]error)
	}
	// Only FAILURES are recorded in errs. Storing a nil error made the key present, so
	// the next lookup took the error path and returned an empty worktree list with no
	// error — every item after the first in a repo saw no existing worktrees, and a
	// converged worktree was then reported as an unregistered directory.
	if err, ok := c.errs[key]; ok {
		return nil, err
	}
	if wt, ok := c.byRepo[key]; ok {
		return wt, nil
	}
	wt, err := listRepoWorktrees(repo)
	if err != nil {
		c.errs[key] = err
		return nil, err
	}
	c.byRepo[key] = wt
	return wt, nil
}

var applyCmd = &cobra.Command{
	Use:   "apply [- | <file>]",
	Short: "Create the worktrees described by JSON on stdin or in a file",
	Long: `Apply a desired set of worktrees, read as JSON from stdin or a file.

DESCRIPTION
  The batch counterpart to the flags. "hydra start" is the fluent way to ask for one
  branch across some repositories; apply takes a LIST and converges to it, in exactly
  the shape "hydra list" emits — so a set can be captured, edited and replayed:

    hydra list --output json > work.json
    hydra list --output json | jq '[.data.worktrees[] | select(.topic=="2072958")]' | hydra apply -

  It accepts either shape, because both are things a caller naturally has:

    a full envelope       {"data": {"worktrees": [...]}}
    a bare array          [{"repo": "api", "branch": "feat/login"}, ...]

  Only repo, branch and topic are read. Everything else "list" emits — path, ahead,
  behind, dirty — is OBSERVED state, not desired state: replaying it cannot make a
  branch two commits ahead, and silently accepting fields it cannot honour would be a
  lie about what apply did.

  It is CONVERGENT, like start: a worktree that already exists is skipped, so applying
  the same document twice is a no-op that exits 0.

  There is no bespoke DSL. JSON is the contract, and the fluent flags are sugar over
  the same code path.

EXAMPLES
  # capture and replay
  $ hydra list --output json > work.json
  $ hydra apply - < work.json

  # the same document by path, for a caller that execs without a shell to redirect with
  $ hydra apply work.json

  # move a topic's worktrees into a fresh clone of the workspace
  $ hydra list --topic 2072958 --output json | hydra apply -

  # see what would happen
  $ hydra apply - --dry-run < work.json

EXIT CODES
  0  Success (including a fully converged no-op)
  1  git_failed, topic_conflict
  2  not_in_project, usage (document over --max-items or --max-size)
  4  partial_failure (some worktrees failed)
  7  needs_input (stdin was empty or unreadable)

SEE ALSO
  • hydra start - the fluent path, for one branch across repositories
  • hydra list  - produces the document apply consumes`,
	Args: cobra.ExactArgs(1),
	RunE: runApply,
}

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false, "Report what would happen and change nothing")
	applyCmd.Flags().IntVar(&applyMaxItems, "max-items", defaultApplyMaxItems,
		fmt.Sprintf("Maximum worktree entries per document (default %d; 0 means no limit)", defaultApplyMaxItems))
	applyCmd.Flags().IntVar(&applyMaxSize, "max-size", defaultApplyMaxSize,
		fmt.Sprintf("Maximum document size in bytes (default %d; 0 means no limit)", defaultApplyMaxSize))
}

// applyItem is the subset of a worktree that describes DESIRED state.
type applyItem struct {
	Repo   string  `json:"repo"`
	Branch string  `json:"branch"`
	Topic  *string `json:"topic"`

	// Name is the worktree DIRECTORY, carried so a captured workspace reproduces as itself.
	// Deriving it from the branch instead cannot reproduce a worktree created with `--as`: the
	// derived directory does not exist, and the branch is already checked out at the real one.
	// Optional, so a hand-written document may still omit it and take the derived name.
	Name string `json:"name,omitempty"`
}

// dirNameFor returns the directory an item asks for, falling back to the name derived from the
// branch when the document does not say.
//
// The document is untrusted — stdin or a caller-named file — and this value becomes a path segment
// under the workspace root, so a name containing `..` or a separator must be refused rather than
// allowed to place a worktree outside the workspace. `add` applies the same rule to `--as`.
func (i applyItem) dirNameFor(repo repoContext) (string, error) {
	if i.Name == "" {
		return worktreeDirName(repo, i.Branch), nil
	}
	if err := validatePathSegment("name", i.Name); err != nil {
		return "", output.Wrap(output.CodeInternal, err, "invalid worktree name in document")
	}
	return i.Name, nil
}

type applyResultJSON struct {
	Group  string `json:"group"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	// Name and Path were both absent: apply reported that it had created a worktree
	// without saying where, so the one command whose purpose is reproducing a
	// workspace elsewhere could not tell a caller what it had built.
	Name        string             `json:"name"`
	Path        string             `json:"path"`
	Topic       string             `json:"topic,omitempty"`
	Disposition string             `json:"disposition"`
	Error       *output.Diagnostic `json:"error,omitempty"`
}

type applyJSON struct {
	DryRun  bool              `json:"dry_run"`
	Total   int               `json:"total"`
	Results []applyResultJSON `json:"results"`
	Created int               `json:"created"`
	Skipped int               `json:"skipped"`
	Failed  int               `json:"failed"`
}

func runApply(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	// "-" names stdin and a path names a file; ONE of them is required, enforced by ExactArgs
	// rather than asserted in prose. A bare `hydra apply` would block on a terminal with nothing
	// said, which is what naming the source in the command line exists to prevent.
	//
	// A PATH is accepted too, because an agent that execs without a shell cannot redirect:
	// `< work.json` is shell composition, so stdin-only forced every non-shell caller to
	// plumb a pipe to pass a file it already had on disk. Same document, one more source.
	source := cmd.InOrStdin()
	from := "stdin"
	if len(args) == 1 && args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			// needs_input, not internal: internal is hydra's bucket for its own bugs, and the
			// command's documented EXIT CODES already promise needs_input for an unreadable
			// input — the empty-file half of this same failure mode already returned it.
			return output.Wrap(output.CodeNeedsInput, err,
				"apply could not read %q; pass a readable file or - for stdin", args[0]).
				WithDetail("argument", args[0]).
				WithDetail("missing", []string{args[0]})
		}
		defer func() { _ = f.Close() }()
		source, from = f, args[0]
	}

	limits := applyLimits{maxItems: applyMaxItems, maxSize: int64(applyMaxSize)}
	items, warnings, err := readApplyItems(source, from, limits)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return output.Errorf(output.CodeNeedsInput,
			"%s described no worktrees", from).
			WithDetail("missing", []string{from}).
			WithDetail("reason", "expected the shape \"hydra list --output json\" emits")
	}

	// Resolve every item against the manifest before any git or filesystem work.
	plans := planApplyItems(items)

	payload := applyJSON{DryRun: applyDryRun, Total: len(plans)}
	cache := &applyWorktreeCache{}
	for _, plan := range plans {
		result := plan.result
		if plan.hardFail {
			payload.Failed++
			payload.Results = append(payload.Results, result)
			continue
		}

		if applyDryRun {
			result.Disposition = applyDryRunDisposition(plan.repo, plan.item, cache)
			payload.Results = append(payload.Results, result)
			tallyApplyDisposition(&payload, result.Disposition)
			continue
		}

		disposition, applyErr := applyOne(plan.repo, plan.item, cache)
		result.Disposition = disposition
		if applyErr != nil {
			// The item did not reach its desired state, whatever became of the worktree. Counting
			// by disposition alone would let a created worktree with an unrecorded topic exit 0
			// and the same state on a later run exit 1.
			result.Error = output.Classify(applyErr)
			payload.Failed++
		} else {
			tallyApplyDisposition(&payload, disposition)
		}
		payload.Results = append(payload.Results, result)
	}

	summary := applySummary(payload)

	// The error rides the same envelope as the data: a caller must see the worktrees
	// that landed and the reason the rest did not, without merging two streams.
	var applyErr *output.Error
	outcome := output.OutcomeSuccess
	switch payload.Failed {
	case 0:
	case payload.Total:
		outcome = output.OutcomeFailure
		applyErr = output.Errorf(output.CodeGitFailed,
			"every worktree in the document failed").
			WithDetail("failed", payload.Failed)
	default:
		outcome = output.OutcomePartial
		applyErr = output.Errorf(output.CodePartialFailure,
			"%d of %d worktree(s) failed", payload.Failed, payload.Total).
			WithDetail("failed", payload.Failed)
	}

	if emitErr := emitResult(cmd, output.Result{
		Outcome:  outcome,
		Summary:  summary,
		Data:     payload,
		Warnings: warnings,
		Err:      applyErr,
	}, func() { printApplyText(payload, summary) }); emitErr != nil {
		return emitErr
	}
	if applyErr != nil {
		return applyErr
	}
	return nil
}

type applyItemPlan struct {
	item     applyItem
	repo     repoContext
	result   applyResultJSON
	hardFail bool
}

func planApplyItems(items []applyItem) []applyItemPlan {
	plans := make([]applyItemPlan, 0, len(items))
	// One repoContext per distinct alias, not per item. Building it asks git for
	// origin/HEAD whenever the manifest omits default_branch, so a 3000-item document
	// spawned 3000 `git symbolic-ref` processes — about 5ms each, which was the entire
	// 14s a --dry-run took while doing no filesystem work at all.
	contexts := make(map[string]repoContext, len(items))
	for _, item := range items {
		result := applyResultJSON{Repo: item.Repo, Branch: item.Branch}
		if item.Topic != nil {
			result.Topic = *item.Topic
		}
		plan := applyItemPlan{item: item, result: result}

		ref, ok := cfg.FindRepo(item.Repo)
		if !ok {
			result.Disposition = "failed"
			result.Error = output.Errorf(output.CodeRepoUnknown, "repository %q is not registered", item.Repo)
			plan.result = result
			plan.hardFail = true
			plans = append(plans, plan)
			continue
		}
		repo, cached := contexts[ref.Alias]
		if !cached {
			repo = repoContextFor(cfg, projectRoot, ref)
			contexts[ref.Alias] = repo
		}
		dirName, nameErr := item.dirNameFor(repo)
		if nameErr != nil {
			result.Disposition = "failed"
			result.Error = output.Classify(nameErr)
			plan.result = result
			plan.hardFail = true
			plans = append(plans, plan)
			continue
		}
		result.Group = repo.Group
		result.Name = dirName
		result.Path = worktreePath(projectRoot, repo.Group, dirName)
		plan.repo = repo
		plan.result = result
		plans = append(plans, plan)
	}
	return plans
}

func applyOverItemLimit(observed, limit int) error {
	return output.Errorf(output.CodeUsage,
		"apply document has %d worktree entries, above --max-items %d", observed, limit).
		WithDetail("limit", limit).
		WithDetail("observed", observed).
		WithDetail("flag", "--max-items")
}

func applyOverSizeLimit(observed, limit int64) error {
	return output.Errorf(output.CodeUsage,
		"apply document is %d bytes, above --max-size %d", observed, limit).
		WithDetail("limit", limit).
		WithDetail("observed", observed).
		WithDetail("flag", "--max-size")
}

func readApplyBoundedSource(source io.Reader, limits applyLimits) ([]byte, error) {
	if limits.maxSize > 0 {
		source = io.LimitReader(source, limits.maxSize+1)
	}
	return io.ReadAll(source)
}

// readApplyItems accepts both the full envelope and a bare array.
//
// Supporting both is not laxity: `hydra list -o json` produces the envelope and
// `jq '.data.worktrees'` produces the array, and demanding one would force a caller to
// reshape a document hydra itself just emitted.
// from names the source in every message and in details.missing, which a caller machine-reads.
// Hardcoding "stdin" made a named file report a source the caller never used.
//
// The document is fully read and validated before apply touches git or the filesystem.
// JSON is a YAML subset; yaml.v3 decodes both without a format flag.
func readApplyItems(source io.Reader, from string, limits applyLimits) ([]applyItem, []*output.Diagnostic, error) {
	data, err := readApplyBoundedSource(source, limits)
	if err != nil {
		return nil, nil, output.Wrap(output.CodeNeedsInput, err, "failed to read %s", from)
	}
	if limits.maxSize > 0 && int64(len(data)) > limits.maxSize {
		return nil, nil, applyOverSizeLimit(int64(len(data)), limits.maxSize)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil, output.Errorf(output.CodeNeedsInput, "%s was empty", from).
			WithDetail("missing", []string{from})
	}

	if strings.HasPrefix(trimmed, "[") {
		return readApplyBareArray([]byte(trimmed), from, limits)
	}
	return readApplyEnvelope([]byte(trimmed), from, limits)
}

func readApplyBareArray(data []byte, from string, limits applyLimits) ([]applyItem, []*output.Diagnostic, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, invalidApplyInput(err, from)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode {
		return nil, nil, invalidApplyInput(fmt.Errorf("expected a sequence"), from)
	}
	seq := doc.Content[0]
	items := make([]applyItem, 0, len(seq.Content))
	for i, node := range seq.Content {
		var item applyItem
		if err := node.Decode(&item); err != nil {
			return nil, nil, invalidApplyInput(err, from)
		}
		items = append(items, item)
		if limits.maxItems > 0 && len(items) > limits.maxItems {
			return nil, nil, applyOverItemLimit(len(items), limits.maxItems)
		}
		_ = i
	}
	return validateApplyItems(items)
}

func readApplyEnvelope(data []byte, from string, limits applyLimits) ([]applyItem, []*output.Diagnostic, error) {
	// `list --all` nests its per-project listing at data.projects[], so that is where this reads
	// it. Declared as a sibling of data, the field matched nothing hydra emits: the documented
	// `list --all | apply -` pipeline parsed clean and applied zero of the worktrees it was given.
	var envelope struct {
		Data struct {
			Worktrees []applyItem `json:"worktrees"`
			Projects  []struct {
				Worktrees []applyItem `json:"worktrees"`
			} `json:"projects"`
		} `json:"data"`
	}
	if err := yaml.Unmarshal(data, &envelope); err != nil {
		return nil, nil, invalidApplyInput(err, from)
	}
	items := envelope.Data.Worktrees
	for _, project := range envelope.Data.Projects {
		items = append(items, project.Worktrees...)
	}
	if limits.maxItems > 0 && len(items) > limits.maxItems {
		return nil, nil, applyOverItemLimit(len(items), limits.maxItems)
	}
	return validateApplyItems(items)
}

// A malformed document is a WRONG value, not an absent one, so it stays `internal` — the same
// split the error table draws between `details.valid` and `details.missing`. Only empty and
// unreadable input are `needs_input`, which is what apply's EXIT CODES block promises.
func invalidApplyInput(err error, from string) error {
	return output.Wrap(output.CodeInternal, err,
		"%s is not valid JSON in the shape \"hydra list --output json\" emits", from)
}

// validateApplyItems rejects an item that cannot describe a worktree, naming the index
// so a caller can find it in a document of any size.
func validateApplyItems(items []applyItem) ([]applyItem, []*output.Diagnostic, error) {
	out := make([]applyItem, 0, len(items))
	var detached int
	for i, item := range items {
		item.Repo = strings.TrimSpace(item.Repo)
		item.Branch = strings.TrimSpace(item.Branch)
		switch {
		case item.Repo == "":
			return nil, nil, output.Errorf(output.CodeInternal,
				"item %d has no repo", i).WithDetail("index", i)
		case item.Branch == "":
			// A detached worktree has no branch, so `list` can legitimately emit one.
			// It is skipped rather than rejected: a round-trip must not fail because
			// the source workspace happened to contain a detached HEAD. It is WARNED
			// about rather than dropped in silence, because the caller asked for a
			// replica and is not getting one.
			detached++
			continue
		}
		out = append(out, item)
	}
	var warnings []*output.Diagnostic
	if detached > 0 {
		warnings = append(warnings, output.Notef("", "%d detached worktree(s) skipped: a branchless worktree cannot be described by a branch", detached))
	}
	return out, warnings, nil
}

func applyCheckWorktreeNameConflict(existing []worktreeContext, repo repoContext, projectRoot, dirName, branch string) error {
	target := worktreePath(projectRoot, repo.Group, dirName)

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

// applyOne converges one item, reusing the same helpers start does.
//
// The disposition describes what happened to the WORKTREE; the error describes whether the item's
// desired state was reached. They are separate because a worktree can land while the topic the
// document asked for cannot be recorded — reporting that as `failed` would deny the git work, and
// reporting it as success would deny the unmet request.
//
// Both paths must answer alike. A topic failure on the created path and the same failure on the
// converged path describe one end state, so treating one as a warning and the other as an error
// made the exit code depend on whether this was the first run.
func applyOne(repo repoContext, item applyItem, cache *applyWorktreeCache) (disposition string, err error) {
	dirName, err := item.dirNameFor(repo)
	if err != nil {
		return "failed", err
	}
	target := worktreePath(projectRoot, repo.Group, dirName)

	existing, listErr := cache.worktrees(repo)
	if listErr != nil {
		return "failed", listErr
	}
	if conflict := applyCheckWorktreeNameConflict(existing, repo, projectRoot, dirName, item.Branch); conflict != nil {
		if !worktreeAlreadyAtTarget(conflict, target) {
			return "failed", conflict
		}
		// Already correct. Membership is still reconciled, because a document may assign an
		// existing worktree to a topic.
		return "skipped", applyTopic(item, repo)
	}
	if _, err := createWorktreeForBranch(cfg, repo, target, item.Branch, ""); err != nil {
		return "failed", err
	}
	return "created", applyTopic(item, repo)
}

// applyTopic records membership when the document asks for it.
func applyTopic(item applyItem, repo repoContext) error {
	if item.Topic == nil || *item.Topic == "" {
		return nil
	}
	err := topicStore().Attach(*item.Topic, topic.Member{Repo: repo.Alias, Branch: item.Branch})
	if err == nil {
		return nil
	}
	// Already in THAT topic is convergence, not a conflict — replaying a document must
	// be a no-op. Belonging to a DIFFERENT topic is still a real conflict.
	var claimed *topic.ErrClaimed
	if errors.As(err, &claimed) && claimed.Existing == *item.Topic {
		return nil
	}
	return classifyTopicErr(err)
}

// tallyApplyDisposition counts one result. The dry run and the real run share it so a predicted
// failure is counted as a failure in both; the outcome and exit code then follow from
// payload.Failed, which is what makes --dry-run a usable preflight rather than a report that
// always succeeds.
func tallyApplyDisposition(p *applyJSON, disposition string) {
	switch disposition {
	case "created", "would_create":
		p.Created++
	case "skipped":
		p.Skipped++
	default:
		p.Failed++
	}
}

// applyDryRunDisposition predicts what a real run would report, so it must split the same three
// ways applyOne does: converged, conflicting, absent. Reporting `would_create` for a directory
// that a real run refuses would make --dry-run useless exactly where it matters most.
func applyDryRunDisposition(repo repoContext, item applyItem, cache *applyWorktreeCache) string {
	dirName, err := item.dirNameFor(repo)
	if err != nil {
		return "failed"
	}
	target := worktreePath(projectRoot, repo.Group, dirName)
	converged := false
	existing, listErr := cache.worktrees(repo)
	if listErr != nil {
		return "failed"
	}
	if err := applyCheckWorktreeNameConflict(existing, repo, projectRoot, dirName, item.Branch); err != nil {
		if !worktreeAlreadyAtTarget(err, target) {
			return "failed"
		}
		converged = true
	}
	// Membership is predicted too. The worktree half is only half the item: a document asking for a
	// worktree that already belongs to a DIFFERENT topic fails the real run, and a preflight that
	// reports success for it is not a preflight.
	if topicConflictFor(item, repo) {
		return "failed"
	}
	if converged {
		return "skipped"
	}
	return "would_create"
}

// topicConflictFor reports whether the document asks for a topic the worktree cannot have. It
// mirrors applyTopic's rule without writing: belonging to the REQUESTED topic is convergence,
// belonging to another is the conflict.
func topicConflictFor(item applyItem, repo repoContext) bool {
	if item.Topic == nil || *item.Topic == "" {
		return false
	}
	current, ok, err := topicStore().TopicOf(repo.Alias, item.Branch)
	if err != nil || !ok {
		// Unreadable state is reported by the real run, which is where the error belongs; an
		// unclaimed worktree is free to join.
		return false
	}
	return current != *item.Topic
}

func applySummary(payload applyJSON) string {
	verb := ""
	if payload.DryRun {
		verb = "dry run: "
	}
	parts := []string{}
	if payload.Created > 0 {
		parts = append(parts, fmt.Sprintf("%d created", payload.Created))
	}
	if payload.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d already present", payload.Skipped))
	}
	if payload.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", payload.Failed))
	}
	if len(parts) == 0 {
		return verb + "nothing to do"
	}
	return verb + strings.Join(parts, ", ")
}

func printApplyText(payload applyJSON, summary string) {
	_, _ = fmt.Fprintln(os.Stdout)
	if payload.Failed == 0 {
		_, _ = fmt.Fprintln(os.Stdout, styles.Success.Render("✓ "+summary))
	} else {
		_, _ = fmt.Fprintln(os.Stdout, styles.Error.Render("✗ "+summary))
	}
	_, _ = fmt.Fprintln(os.Stdout)
	for _, result := range payload.Results {
		label := styles.Label.Render(result.Disposition)
		if result.Error != nil {
			label = styles.Error.Render(result.Disposition)
		}
		_, _ = fmt.Fprintf(os.Stdout, "  %-14s %s/%s %s\n", label, result.Repo, result.Branch, result.Error.Message)
	}
	_, _ = fmt.Fprintln(os.Stdout)
}
