package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
)

// applyWith runs apply against a stdin document, discarding the envelope warnings.
func applyWith(t *testing.T, doc string, args ...string) (applyJSON, error) {
	t.Helper()
	payload, _, err := applyReporting(t, doc, args...)
	return payload, err
}

// applyReporting also returns the envelope warnings, for the cases that assert on them.
func applyReporting(t *testing.T, doc string, args ...string) (applyJSON, []*output.Diagnostic, error) {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetIn(strings.NewReader(doc))
	rootCmd.SetArgs(append([]string{"apply", "-", "--output", "json"}, args...))
	err := rootCmd.Execute()

	var envelope struct {
		Data     applyJSON            `json:"data"`
		Warnings []*output.Diagnostic `json:"warnings"`
	}
	if stdout.Len() > 0 {
		if jsonErr := json.Unmarshal(stdout.Bytes(), &envelope); jsonErr != nil {
			t.Fatalf("apply emitted unparseable JSON: %v\n%s", jsonErr, stdout.String())
		}
	}
	return envelope.Data, envelope.Warnings, err
}

func applyEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()
	return env
}

// The round-trip the design rests on: what list emits, apply consumes.
func TestApply_RoundTripsWhatListEmits(t *testing.T) {
	resetCommandState(t)
	env := applyEnv(t)

	// Produce a real listing rather than a hand-written document, so the test breaks if
	// the two shapes ever diverge.
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"list", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	document := stdout.String()

	// Everything already exists, so a replay must be a pure no-op.
	payload, err := applyWith(t, document)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if payload.Created != 0 || payload.Failed != 0 {
		t.Errorf("replaying a listing must change nothing, got %+v", payload)
	}
	if payload.Skipped == 0 {
		t.Error("the existing worktrees should be reported as skipped")
	}
	_ = env
}

// A worktree the document asks for but which does not exist is created.
func TestApply_CreatesAMissingWorktree(t *testing.T) {
	resetCommandState(t)
	env := applyEnv(t)

	payload, err := applyWith(t, `[{"repo":"api","branch":"feat/from-apply"}]`)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if payload.Created != 1 {
		t.Fatalf("created = %d, want 1: %+v", payload.Created, payload.Results)
	}
	if !env.DirExists(env.GetWorktreePath("backend", "api-feat-from-apply")) {
		t.Error("the worktree was not created on disk")
	}
}

// Applying the same document twice is a no-op that exits 0 — the same convergence
// property clone and start have.
func TestApply_IsIdempotent(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)
	doc := `[{"repo":"api","branch":"feat/twice"}]`

	if _, err := applyWith(t, doc); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	payload, err := applyWith(t, doc)
	if err != nil {
		t.Fatalf("second apply must succeed: %v", err)
	}
	if payload.Created != 0 || payload.Skipped != 1 {
		t.Errorf("second apply = %+v, want 0 created and 1 skipped", payload)
	}
}

// Both input shapes are accepted, because list produces one and jq produces the other.
func TestApply_AcceptsEnvelopeAndBareArray(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{name: "bare array", doc: `[{"repo":"api","branch":"feat/shape-a"}]`},
		{name: "full envelope", doc: `{"data":{"worktrees":[{"repo":"api","branch":"feat/shape-b"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetCommandState(t)
			applyEnv(t)

			payload, err := applyWith(t, tc.doc)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if payload.Created != 1 {
				t.Errorf("created = %d, want 1", payload.Created)
			}
		})
	}
}

// Topic membership is desired state and travels with the document.
func TestApply_RecordsTopicMembership(t *testing.T) {
	resetCommandState(t)
	env := applyEnv(t)

	payload, err := applyWith(t, `[{"repo":"api","branch":"feat/with-topic","topic":"7001"}]`)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if payload.Created != 1 {
		t.Fatalf("created = %d, want 1", payload.Created)
	}

	got, ok, err := topic.Open(env.RootDir).Get("7001")
	if err != nil || !ok {
		t.Fatalf("the topic must be recorded (ok=%v err=%v)", ok, err)
	}
	if len(got.Members) != 1 || got.Members[0].Branch != "feat/with-topic" {
		t.Errorf("members = %+v, want the applied worktree", got.Members)
	}
}

// Re-applying a document whose worktree is ALREADY in that topic is convergence, not a
// conflict; a different topic is still a conflict.
func TestApply_SameTopicConvergesDifferentTopicConflicts(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)
	doc := `[{"repo":"api","branch":"feat/topic-conv","topic":"7001"}]`

	if _, err := applyWith(t, doc); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if _, err := applyWith(t, doc); err != nil {
		t.Fatalf("re-applying the same topic must converge: %v", err)
	}

	other := `[{"repo":"api","branch":"feat/topic-conv","topic":"9999"}]`
	payload, err := applyWith(t, other)
	if err == nil {
		t.Fatal("claiming a worktree for a second topic must fail")
	}
	if payload.Failed != 1 {
		t.Errorf("failed = %d, want 1", payload.Failed)
	}
}

// Observed state is IGNORED rather than half-honoured: replaying a listing cannot make
// a branch two commits ahead, and pretending otherwise would misreport what apply did.
func TestApply_IgnoresObservedState(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)

	doc := `[{"repo":"api","branch":"feat/observed","ahead":7,"behind":3,"dirty":true,
	         "path":"/nonexistent/somewhere","upstream":"origin/nope"}]`
	payload, err := applyWith(t, doc)
	if err != nil {
		t.Fatalf("apply must ignore observed fields, not fail on them: %v", err)
	}
	if payload.Created != 1 {
		t.Fatalf("created = %d, want 1: %+v", payload.Created, payload.Results)
	}
	// The path in the document was nonsense; apply must derive its own.
	if strings.Contains(payload.Results[0].Repo, "/") {
		t.Errorf("apply used a path from the document: %+v", payload.Results[0])
	}
}

// A detached worktree has no branch and is skipped rather than rejected, so a round-trip
// does not fail because the source workspace had one — but it is WARNED about, because a
// caller asking for a replica is silently not getting one.
func TestApply_WarnsAboutBranchlessItemsRatherThanDroppingThem(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)

	payload, warnings, err := applyReporting(t,
		`[{"repo":"api","branch":""},{"repo":"api","branch":"feat/kept"}]`)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if payload.Total != 1 {
		t.Errorf("total = %d, want only the item with a branch", payload.Total)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "detached") {
		t.Errorf("warnings = %q, want one naming the skipped detached worktree", warnings)
	}
}

// The warning is absent when there is nothing to warn about: an empty list of warnings
// must not be manufactured on the happy path.
func TestApply_NoWarningWithoutBranchlessItems(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)

	_, warnings, err := applyReporting(t, `[{"repo":"api","branch":"feat/quiet"}]`)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
}

func TestApply_UnknownRepoIsReportedPerItem(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)

	payload, err := applyWith(t,
		`[{"repo":"api","branch":"feat/ok"},{"repo":"nope","branch":"feat/bad"}]`)
	if err == nil {
		t.Fatal("an unknown repo must be reported")
	}
	if code := output.Classify(err).Code; code != output.CodePartialFailure {
		t.Errorf("code = %q, want %q", code, output.CodePartialFailure)
	}
	// The good item still landed: a bad row must not abort the batch.
	if payload.Created != 1 || payload.Failed != 1 {
		t.Errorf("created/failed = %d/%d, want 1/1", payload.Created, payload.Failed)
	}
}

func TestApply_EmptyAndInvalidStdin(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		code string
	}{
		{name: "empty", doc: "", code: output.CodeNeedsInput},
		{name: "whitespace", doc: "   \n ", code: output.CodeNeedsInput},
		{name: "not json", doc: "not json at all", code: output.CodeInternal},
		{name: "empty array", doc: "[]", code: output.CodeNeedsInput},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetCommandState(t)
			applyEnv(t)

			_, err := applyWith(t, tc.doc)
			if err == nil {
				t.Fatalf("apply with %q must fail", tc.doc)
			}
			if code := output.Classify(err).Code; code != tc.code {
				t.Errorf("code = %q, want %q", code, tc.code)
			}
		})
	}
}

func TestApply_DryRunChangesNothing(t *testing.T) {
	resetCommandState(t)
	env := applyEnv(t)

	payload, err := applyWith(t, `[{"repo":"api","branch":"feat/dry"}]`, "--dry-run")
	if err != nil {
		t.Fatalf("apply --dry-run: %v", err)
	}
	if !payload.DryRun {
		t.Error("dry_run must be reported")
	}
	if env.DirExists(env.GetWorktreePath("backend", "api-feat-dry")) {
		t.Error("--dry-run created a worktree")
	}
}

// The - argument is required, so "reads stdin" is visible in the command line rather
// than a bare invocation blocking with no explanation.
func TestApply_RejectsANonDashArgument(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetIn(bytes.NewReader(nil))
	rootCmd.SetArgs([]string{"apply", "somefile.json", "--output", "json"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("apply must reject an argument other than -")
	}
}

// A captured worktree carries the directory it lives in, and `apply` exists to reproduce a
// workspace elsewhere, so a `--as` name has to come back as itself. Deriving the directory from
// the branch yields a name that does not exist beside a branch already checked out at the real one.
func TestApply_ReproducesAnExplicitWorktreeName(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	doc := filepath.Join(env.RootDir, "snap.json")
	if err := os.WriteFile(doc, []byte(
		`[{"repo":"api","branch":"stage","name":"mystage"}]`), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}

	rootCmd.SetArgs([]string{"apply", doc})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("apply the captured name: %v (code %q)", err, output.Classify(err).Code)
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, "backend", "mystage")); err != nil {
		t.Error("apply did not create the captured directory name")
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, "backend", "api-stage")); err == nil {
		t.Error("apply created the derived name instead of the captured one")
	}
}

// The document is untrusted input, and `name` becomes a path segment under the workspace root.
// A traversal attempt must fail that ITEM without escaping the workspace and without aborting the
// rest of the run.
func TestApply_RefusesAWorktreeNameThatEscapesTheWorkspace(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	outside := filepath.Join(env.RootDir, "escaped")
	doc := filepath.Join(env.RootDir, "evil.json")
	body := `[{"repo":"api","branch":"stage","name":"../escaped"},{"repo":"api","branch":"main"}]`
	if err := os.WriteFile(doc, []byte(body), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}

	rootCmd.SetArgs([]string{"apply", doc})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("apply accepted a name that leaves the workspace")
	}
	if got := output.Classify(err).Code; got != output.CodePartialFailure {
		t.Errorf("code: got %q, want %q so the valid item is still reported", got, output.CodePartialFailure)
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Error("a worktree was created outside the workspace root")
	}
	if _, statErr := os.Stat(filepath.Join(env.RootDir, "backend", "api")); statErr != nil {
		t.Error("the valid item was abandoned; one bad name must not stop the run")
	}
}

// Convergence is about the exit code as much as the tree: one end state must report one way. A
// topic the document cannot have is an unmet request whether this run created the worktree or
// found it already there, so the second run must not disagree with the first.
func TestApply_ExitCodeDoesNotDependOnWhichRunItIs(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	rootCmd.SetArgs([]string{"start", "stage", "--repos", "api", "--topic", "A"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("claim the worktree for topic A: %v", err)
	}

	doc := filepath.Join(env.RootDir, "doc.json")
	if err := os.WriteFile(doc, []byte(
		`[{"repo":"api","branch":"stage","name":"api-stage","topic":"B"}]`), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}

	codes := make([]string, 2)
	for i := range codes {
		resetCommandState(t)
		rootCmd.SetArgs([]string{"apply", doc})
		err := rootCmd.Execute()
		if err == nil {
			t.Fatalf("run %d: apply reported success for a topic the worktree cannot have", i+1)
		}
		codes[i] = string(output.Classify(err).Code)
	}
	if codes[0] != codes[1] {
		t.Errorf("run 1 reported %q and run 2 reported %q for one end state", codes[0], codes[1])
	}
}

// `list --all` nests its listing at data.projects[]. Reading `projects` as a sibling of `data`
// matched nothing hydra emits, so the pipeline `list` itself advertises applied zero worktrees.
func TestApply_ReadsTheProjectsShapeListAllEmits(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	doc := filepath.Join(env.RootDir, "all.json")
	body := `{"schema":3,"command":"list","outcome":"success","data":{"total":1,"projects":[` +
		`{"project":"p","root":"/x","worktrees":[{"repo":"api","branch":"stage"}]}]}}`
	if err := os.WriteFile(doc, []byte(body), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}

	rootCmd.SetArgs([]string{"apply", doc})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("apply must read data.projects[].worktrees, got %v (code %q)",
			err, output.Classify(err).Code)
	}
	if _, statErr := os.Stat(filepath.Join(env.RootDir, "backend", "api-stage")); statErr != nil {
		t.Error("no worktree was created from the --all shape")
	}
}

// A named file must be named in every message and in details.missing, which callers machine-read.
func TestApply_NamesTheFileItReadRatherThanStdin(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	empty := filepath.Join(env.RootDir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}

	rootCmd.SetArgs([]string{"apply", empty})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("an empty document was accepted")
	}
	e := output.Classify(err)
	if strings.Contains(e.Message, "stdin") {
		t.Errorf("message names stdin for a file argument: %q", e.Message)
	}
	missing, _ := e.Details["missing"].([]string)
	if len(missing) != 1 || missing[0] != empty {
		t.Errorf("details.missing = %#v, want the file that was read", e.Details["missing"])
	}
}

// An unreadable path is a caller naming a bad file, not a hydra bug. The command's own EXIT CODES
// block documents needs_input for it, and the empty-input half already returned that.
func TestApply_UnreadablePathIsNeedsInput(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	rootCmd.SetArgs([]string{"apply", filepath.Join(env.RootDir, "does-not-exist.json")})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a missing file was accepted")
	}
	if got := output.Classify(err).Code; got != output.CodeNeedsInput {
		t.Errorf("code: got %q, want %q", got, output.CodeNeedsInput)
	}
}

// The source argument is required, so a bare `hydra apply` fails fast instead of blocking on a
// terminal with nothing said.
func TestApply_RequiresASourceArgument(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	rootCmd.SetArgs([]string{"apply"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("a bare apply was accepted; it would block on a terminal")
	}
}

// --dry-run must predict the real run for the WHOLE item, not just the worktree half. A document
// asking for a worktree that already belongs to a different topic fails the real run, so a preflight
// reporting success for it is not a preflight.
func TestApply_DryRunPredictsATopicConflict(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	rootCmd.SetArgs([]string{"start", "stage", "--repos", "api", "--topic", "TOPIC-A"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("claim the worktree for TOPIC-A: %v", err)
	}

	doc := filepath.Join(env.RootDir, "doc.json")
	if err := os.WriteFile(doc, []byte(
		`[{"repo":"api","branch":"stage","topic":"TOPIC-B"}]`), 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"apply", doc, "--dry-run"})
	dryErr := rootCmd.Execute()

	resetCommandState(t)
	rootCmd.SetArgs([]string{"apply", doc})
	realErr := rootCmd.Execute()

	if realErr == nil {
		t.Fatal("the real run accepted a topic the worktree cannot have; the fixture is wrong")
	}
	if dryErr == nil {
		t.Error("--dry-run reported success for a document the real run refuses")
	}
}

func applyBareArrayDoc(n int, branchPrefix string) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"repo":"api","branch":"%s-%d"}`, branchPrefix, i)
	}
	b.WriteString("]")
	return b.String()
}

func TestApply_ExceedsMaxItemsRefusesBeforeWork(t *testing.T) {
	resetCommandState(t)
	env := applyEnv(t)

	doc := applyBareArrayDoc(defaultApplyMaxItems+1, "feat/max")
	_, err := applyWith(t, doc)
	if err == nil {
		t.Fatal("expected refusal when over --max-items")
	}
	e := output.Classify(err)
	if e.Code != output.CodeUsage {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeUsage)
	}
	if e.Details["flag"] != "--max-items" {
		t.Fatalf("details.flag = %v, want --max-items", e.Details["flag"])
	}
	for i := 0; i < 3; i++ {
		if env.DirExists(env.GetWorktreePath("backend", fmt.Sprintf("api-feat-max-%d", i))) {
			t.Fatalf("worktree %d exists after over-limit refusal", i)
		}
	}
}

func TestApply_ExceedsMaxSizeRefusesBeforeWork(t *testing.T) {
	resetCommandState(t)
	env := applyEnv(t)

	doc := applyBareArrayDoc(3, "feat/size")
	resetCommandState(t)
	_, err := applyWith(t, doc, "--max-size", fmt.Sprintf("%d", len(doc)-1))
	if err == nil {
		t.Fatal("expected refusal when over --max-size")
	}
	e := output.Classify(err)
	if e.Code != output.CodeUsage {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeUsage)
	}
	if e.Details["flag"] != "--max-size" {
		t.Fatalf("details.flag = %v, want --max-size", e.Details["flag"])
	}
	if env.DirExists(env.GetWorktreePath("backend", "api-feat-size-0")) {
		t.Error("worktree exists after over-size refusal")
	}
}

func TestApply_UnlimitedOverridesAcceptLargeDocument(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)

	doc := applyBareArrayDoc(defaultApplyMaxItems+1, "feat/unlim")
	_, err := applyWith(t, doc, "--max-items", "0", "--dry-run")
	if err != nil {
		t.Fatalf("apply with --max-items 0: %v", err)
	}
}

func TestApply_DefaultLimitsAppearInHelp(t *testing.T) {
	f := applyCmd.Flags().Lookup("max-items")
	if f == nil {
		t.Fatal("max-items flag missing")
	}
	if !strings.Contains(f.Usage, fmt.Sprintf("%d", defaultApplyMaxItems)) {
		t.Errorf("max-items usage %q must mention default %d", f.Usage, defaultApplyMaxItems)
	}
	f = applyCmd.Flags().Lookup("max-size")
	if f == nil {
		t.Fatal("max-size flag missing")
	}
	if !strings.Contains(f.Usage, fmt.Sprintf("%d", defaultApplyMaxSize)) {
		t.Errorf("max-size usage %q must mention default %d", f.Usage, defaultApplyMaxSize)
	}
}

func TestApply_DryRunLargeDocumentIsFastAndCreatesNothing(t *testing.T) {
	resetCommandState(t)
	env := applyEnv(t)

	const n = 3000
	doc := applyBareArrayDoc(n, "feat/bulk")
	start := time.Now()
	payload, err := applyWith(t, doc, "--max-items", "0", "--dry-run")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !payload.DryRun || payload.Total != n {
		t.Fatalf("dry_run=%v total=%d want %d", payload.DryRun, payload.Total, n)
	}
	// 3s, not 5s, and the difference matters: this took 14s in a real workspace while
	// passing here, because planApplyItems built a repoContext per ITEM and each one
	// asked git for origin/HEAD. The fixture's bare repo has no origin/HEAD, so that
	// spawn failed fast in-test and succeeded slowly in the field. The bound now sits
	// well inside what a per-item spawn costs.
	if elapsed > 3*time.Second {
		t.Fatalf("dry-run took %s; expected well under 3s for %d items", elapsed, n)
	}
	for i := 0; i < 3; i++ {
		if env.DirExists(env.GetWorktreePath("backend", fmt.Sprintf("api-feat-bulk-%d", i))) {
			t.Fatalf("dry-run created worktree %d", i)
		}
	}
}

// Two items in ONE repo must both converge. The worktree cache recorded a nil error
// under the repo's key, which made the key present, so every item after the first took
// the error path and saw an empty worktree list — a converged worktree was then reported
// as an unregistered directory and the round trip `list | apply -` exited 4.
func TestApply_SecondItemInSameRepoStillConverges(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)

	// Two worktrees in ONE repo is the shape that broke: the cache is keyed per repo.
	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add stage: %v", err)
	}

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"list", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	listed := stdout.String()

	resetCommandState(t)
	stdout, _ = resetCommandIO()
	rootCmd.SetIn(strings.NewReader(listed))
	rootCmd.SetArgs([]string{"apply", "-", "--dry-run", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("replaying what list emitted must converge, not fail: %v\n%s", err, stdout.String())
	}

	var envelope struct {
		Data struct {
			Total   int `json:"total"`
			Failed  int `json:"failed"`
			Results []struct {
				Branch      string `json:"branch"`
				Disposition string `json:"disposition"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout.String())
	}
	if envelope.Data.Total < 2 {
		t.Fatalf("fixture needs two worktrees in one repo, got %d", envelope.Data.Total)
	}
	if envelope.Data.Failed != 0 {
		t.Errorf("failed = %d, want 0: %s", envelope.Data.Failed, stdout.String())
	}
	for _, r := range envelope.Data.Results {
		if r.Disposition != "skipped" {
			t.Errorf("branch %s disposition = %q, want skipped", r.Branch, r.Disposition)
		}
	}
}
