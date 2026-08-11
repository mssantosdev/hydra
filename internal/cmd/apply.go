package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var applyDryRun bool

var applyCmd = &cobra.Command{
	Use:   "apply -",
	Short: "Create the worktrees described by JSON on stdin",
	Long: `Apply a desired set of worktrees, read as JSON on stdin.

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

  # move a topic's worktrees into a fresh clone of the workspace
  $ hydra list --topic 2072958 --output json | hydra apply -

  # see what would happen
  $ hydra apply - --dry-run < work.json

EXIT CODES
  0  Success (including a fully converged no-op)
  1  git_failed, topic_conflict
  2  not_in_project
  4  partial_failure (some worktrees failed)
  7  needs_input (stdin was empty or unreadable)

SEE ALSO
  • hydra start - the fluent path, for one branch across repositories
  • hydra list  - produces the document apply consumes`,
	Args: cobra.MaximumNArgs(1),
	RunE: runApply,
}

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false, "Report what would happen and change nothing")
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
	Name        string `json:"name"`
	Path        string `json:"path"`
	Topic       string `json:"topic,omitempty"`
	Disposition string `json:"disposition"`
	Error       string `json:"error,omitempty"`
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
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}
	// "-" means stdin, and stays required by convention rather than parsed: it makes "reads
	// stdin" visible in the command line, the way `kubectl apply -f -` does, instead of a
	// bare `hydra apply` that blocks with no explanation.
	//
	// A PATH is accepted too, because an agent that execs without a shell cannot redirect:
	// `< work.json` is shell composition, so stdin-only forced every non-shell caller to
	// plumb a pipe to pass a file it already had on disk. Same document, one more source.
	source := cmd.InOrStdin()
	from := "stdin"
	if len(args) == 1 && args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			return output.Wrap(output.CodeInternal, err,
				"apply could not read %q; pass a readable file or - for stdin", args[0]).
				WithDetail("argument", args[0])
		}
		defer func() { _ = f.Close() }()
		source, from = f, args[0]
	}

	items, warnings, err := readApplyItems(source)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return output.Errorf(output.CodeNeedsInput,
			"%s described no worktrees", from).
			WithDetail("missing", []string{from}).
			WithDetail("reason", "expected the shape \"hydra list --output json\" emits")
	}

	payload := applyJSON{DryRun: applyDryRun, Total: len(items)}
	for _, item := range items {
		result := applyResultJSON{Repo: item.Repo, Branch: item.Branch}
		if item.Topic != nil {
			result.Topic = *item.Topic
		}

		ref, ok := cfg.FindRepo(item.Repo)
		if !ok {
			result.Disposition, result.Error = "failed", fmt.Sprintf("repository %q is not registered", item.Repo)
			payload.Failed++
			payload.Results = append(payload.Results, result)
			continue
		}
		repo := repoContextFor(cfg, projectRoot, ref)
		dirName, nameErr := item.dirNameFor(repo)
		if nameErr != nil {
			result.Disposition, result.Error = "failed", output.Classify(nameErr).Message
			payload.Failed++
			payload.Results = append(payload.Results, result)
			continue
		}
		result.Group = repo.Group
		result.Name = dirName
		result.Path = worktreePath(projectRoot, repo.Group, dirName)

		if applyDryRun {
			result.Disposition = applyDryRunDisposition(repo, item)
			payload.Results = append(payload.Results, result)
			tallyApplyDisposition(&payload, result.Disposition)
			continue
		}

		disposition, warning, applyErr := applyOne(repo, item)
		result.Disposition = disposition
		if warning != "" {
			warnings = append(warnings, warning)
		}
		if disposition != "created" && disposition != "skipped" {
			result.Error = output.Classify(applyErr).Message
		}
		tallyApplyDisposition(&payload, disposition)
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

// readApplyItems accepts both the full envelope and a bare array.
//
// Supporting both is not laxity: `hydra list -o json` produces the envelope and
// `jq '.data.worktrees'` produces the array, and demanding one would force a caller to
// reshape a document hydra itself just emitted.
func readApplyItems(stdin io.Reader) ([]applyItem, []string, error) {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, nil, output.Wrap(output.CodeInternal, err, "failed to read stdin")
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil, output.Errorf(output.CodeNeedsInput, "stdin was empty").
			WithDetail("missing", []string{"stdin"})
	}

	if strings.HasPrefix(trimmed, "[") {
		var items []applyItem
		if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
			return nil, nil, invalidApplyInput(err)
		}
		return validateApplyItems(items)
	}

	var envelope struct {
		Data struct {
			Worktrees []applyItem `json:"worktrees"`
		} `json:"data"`
		// A caller may also hand back a project-scoped listing from --all.
		Projects []struct {
			Worktrees []applyItem `json:"worktrees"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return nil, nil, invalidApplyInput(err)
	}
	items := envelope.Data.Worktrees
	for _, project := range envelope.Projects {
		items = append(items, project.Worktrees...)
	}
	return validateApplyItems(items)
}

func invalidApplyInput(err error) error {
	return output.Wrap(output.CodeInternal, err,
		"stdin is not valid JSON in the shape \"hydra list --output json\" emits")
}

// validateApplyItems rejects an item that cannot describe a worktree, naming the index
// so a caller can find it in a document of any size.
func validateApplyItems(items []applyItem) ([]applyItem, []string, error) {
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
	var warnings []string
	if detached > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d detached worktree(s) skipped: a branchless worktree cannot be described by a branch",
			detached))
	}
	return out, warnings, nil
}

// applyOne converges one item, reusing the same helpers start does.
func applyOne(repo repoContext, item applyItem) (disposition, warning string, err error) {
	dirName, err := item.dirNameFor(repo)
	if err != nil {
		return "failed", "", err
	}
	target := worktreePath(projectRoot, repo.Group, dirName)

	if err := checkWorktreeNameConflict(repo, projectRoot, dirName, item.Branch); err != nil {
		if worktreeAlreadyAtTarget(err, target) {
			// Already correct. Membership is still reconciled below, because a document
			// may assign an existing worktree to a topic.
			if topicErr := applyTopic(item, repo); topicErr != nil {
				return "failed", "", topicErr
			}
			return "skipped", "", nil
		}
		return "failed", "", err
	}
	if _, err := createWorktreeForBranch(cfg, repo, target, item.Branch, ""); err != nil {
		return "failed", "", err
	}
	if topicErr := applyTopic(item, repo); topicErr != nil {
		// The worktree is correct; only the record failed, so calling this a creation failure
		// would claim the git work did not happen. It still rides the envelope as a warning:
		// the document asked for a topic assignment that is not in place.
		return "created", fmt.Sprintf("%s: worktree created but topic not recorded: %s",
			dirName, output.Classify(topicErr).Message), nil
	}
	return "created", "", nil
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
func applyDryRunDisposition(repo repoContext, item applyItem) string {
	dirName, err := item.dirNameFor(repo)
	if err != nil {
		return "failed"
	}
	target := worktreePath(projectRoot, repo.Group, dirName)
	if err := checkWorktreeNameConflict(repo, projectRoot, dirName, item.Branch); err != nil {
		if worktreeAlreadyAtTarget(err, target) {
			return "skipped"
		}
		return "failed"
	}
	return "would_create"
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
		if result.Error != "" {
			label = styles.Error.Render(result.Disposition)
		}
		_, _ = fmt.Fprintf(os.Stdout, "  %-14s %s/%s %s\n", label, result.Repo, result.Branch, result.Error)
	}
	_, _ = fmt.Fprintln(os.Stdout)
}
