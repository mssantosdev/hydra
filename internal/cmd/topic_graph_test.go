package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
)

// graphEnv seeds two topics with real worktrees, which is what every graph command needs: an
// edge may only name a live topic.
func graphEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("a", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("attach a: %v", err)
	}
	if err := store.Attach("b", topic.Member{Repo: "api", Branch: "feat/two"}); err != nil {
		t.Fatalf("attach b: %v", err)
	}
	return env
}

func TestTopicLink_RecordsAndConverges(t *testing.T) {
	graphEnv(t)

	stdout, err := topicCovExec(t, "link", "a", "depends_on", "b")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	var first topicLinkJSON
	decodeJSONData(t, stdout, &first)
	if !first.Recorded || first.Kind != topic.KindDependsOn || first.To != "b" {
		t.Fatalf("link payload = %+v", first)
	}

	// Re-recording is convergent: exit 0, recorded=false, nothing duplicated.
	stdout, err = topicCovExec(t, "link", "a", "depends_on", "b")
	if err != nil {
		t.Fatalf("second link must succeed: %v", err)
	}
	var second topicLinkJSON
	decodeJSONData(t, stdout, &second)
	if second.Recorded {
		t.Errorf("re-recording reported a change: %+v", second)
	}
}

// The kind rule is a usage error, not a broken invariant, and the message has to teach the rule
// — it is the only place a plugin author learns a custom kind needs a namespace.
func TestTopicLink_BareCustomKindIsUsage(t *testing.T) {
	graphEnv(t)

	_, err := topicCovExec(t, "link", "a", "blocks", "b")
	if err == nil {
		t.Fatal("a bare custom kind must be refused")
	}
	e := output.Classify(err)
	if e.Code != output.CodeUsage {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeUsage)
	}
	if !strings.Contains(e.Message, "dot") {
		t.Errorf("message must state the namespace rule, got %q", e.Message)
	}

	// And a namespaced kind is accepted, with no gating attached to it.
	if _, err := topicCovExec(t, "link", "a", "acme.tested-by", "b"); err != nil {
		t.Fatalf("namespaced kind: %v", err)
	}
}

// A cycle is a refused DEFAULT that names its own override. Both halves matter: the refusal
// carries the loop, and --force records it at exit 0.
func TestTopicLink_CycleRefusedThenForced(t *testing.T) {
	graphEnv(t)

	if _, err := topicCovExec(t, "link", "a", "depends_on", "b"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := topicCovExec(t, "link", "b", "depends_on", "a")
	if err == nil {
		t.Fatal("a cycle must be refused by default")
	}
	e := output.Classify(err)
	if e.Code != output.CodeTopicCycle {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeTopicCycle)
	}
	if e.Details["path"] == nil {
		t.Error("the refusal must name the loop it would close")
	}
	// The override is published on the error, so a caller never has to guess it.
	found := false
	for _, n := range e.Next {
		if len(n.Argv) > 0 && n.Argv[len(n.Argv)-1] == "--force" {
			found = true
		}
	}
	if !found {
		t.Errorf("the refusal must name its override in next[], got %+v", e.Next)
	}

	stdout, err := topicCovExec(t, "link", "b", "depends_on", "a", "--force")
	if err != nil {
		t.Fatalf("forced link must succeed: %v", err)
	}
	var forced topicLinkJSON
	decodeJSONData(t, stdout, &forced)
	if !forced.Recorded || !forced.Forced {
		t.Fatalf("forced payload = %+v", forced)
	}
}

// An override that still fails the invocation has overridden nothing: the blockers ride the
// envelope as NOTES, which never degrade the outcome.
func TestTopicLink_ForcedOutcomeStaysSuccess(t *testing.T) {
	graphEnv(t)
	if _, err := topicCovExec(t, "link", "a", "depends_on", "b"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	output.ResetVerdict()
	if _, err := topicCovExec(t, "link", "b", "depends_on", "a", "--force"); err != nil {
		t.Fatalf("forced link: %v", err)
	}
	if outcome, code := output.EmittedVerdict(); outcome != output.OutcomeSuccess {
		t.Fatalf("forced link emitted %q/%q, want success — a partial exits non-zero and the "+
			"override would not unblock a script", outcome, code)
	}
}

func TestTopicUnlink_RemovesThenReportsUnknown(t *testing.T) {
	graphEnv(t)
	if _, err := topicCovExec(t, "link", "a", "depends_on", "b"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stdout, err := topicCovExec(t, "unlink", "a", "depends_on", "b")
	if err != nil {
		t.Fatalf("unlink: %v", err)
	}
	var removed topicLinkJSON
	decodeJSONData(t, stdout, &removed)
	if !removed.Removed {
		t.Fatalf("unlink payload = %+v", removed)
	}

	_, err = topicCovExec(t, "unlink", "a", "depends_on", "b")
	if err == nil {
		t.Fatal("unlinking an absent relationship must be an error, not a silent success")
	}
	if e := output.Classify(err); e.Code != output.CodeLinkUnknown {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeLinkUnknown)
	}
}

// The recorded list is what makes link_unknown actionable: the usual cause is a mistyped kind.
func TestTopicUnlink_ListsWhatIsRecorded(t *testing.T) {
	graphEnv(t)
	if _, err := topicCovExec(t, "link", "a", "depends_on", "b"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := topicCovExec(t, "unlink", "a", "part_of", "b")
	if err == nil {
		t.Fatal("a mistyped kind must be refused")
	}
	e := output.Classify(err)
	recorded, ok := e.Details["recorded"].([]string)
	if !ok || len(recorded) != 1 || !strings.HasPrefix(recorded[0], topic.KindDependsOn) {
		t.Fatalf("details.recorded = %#v, want the depends_on edge", e.Details["recorded"])
	}
}

func TestTopicUpdate_MetaFlags(t *testing.T) {
	graphEnv(t)

	stdout, err := topicCovExec(t, "update", "a", "--meta", "acme.pbi=2072958", "--meta", "ui.color=red")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	var set topicJSON
	decodeJSONData(t, stdout, &set)
	if set.Meta["acme.pbi"] != "2072958" || set.Meta["ui.color"] != "red" {
		t.Fatalf("meta = %+v", set.Meta)
	}

	stdout, err = topicCovExec(t, "update", "a", "--unset-meta", "ui.color")
	if err != nil {
		t.Fatalf("unset: %v", err)
	}
	var unset topicJSON
	decodeJSONData(t, stdout, &unset)
	if _, still := unset.Meta["ui.color"]; still {
		t.Errorf("unset left the key: %+v", unset.Meta)
	}
	if unset.Meta["acme.pbi"] != "2072958" {
		t.Errorf("unset took an unrelated key: %+v", unset.Meta)
	}

	// A value may contain the separator; only the first one splits.
	stdout, err = topicCovExec(t, "update", "a", "--meta", "url=https://x/y?a=b")
	if err != nil {
		t.Fatalf("update with = in value: %v", err)
	}
	var withEq topicJSON
	decodeJSONData(t, stdout, &withEq)
	if withEq.Meta["url"] != "https://x/y?a=b" {
		t.Errorf("value was truncated at the second separator: %q", withEq.Meta["url"])
	}
}

func TestTopicUpdate_MalformedMetaIsUsage(t *testing.T) {
	graphEnv(t)

	_, err := topicCovExec(t, "update", "a", "--meta", "novalue")
	if err == nil {
		t.Fatal("--meta without = must be refused")
	}
	if e := output.Classify(err); e.Code != output.CodeUsage {
		t.Fatalf("code = %q, want usage", e.Code)
	}
}

// A document replaces whole sections. That is what makes a checked-in file the source of truth
// rather than a patch with invented merge rules.
func TestTopicUpdate_DocumentReplacesWholesale(t *testing.T) {
	env := graphEnv(t)
	if _, err := topicCovExec(t, "link", "a", "acme.x", "b"); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	if _, err := topicCovExec(t, "update", "a", "--meta", "old=1"); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	doc := filepath.Join(env.RootDir, "doc.yaml")
	body := "links:\n  - kind: part_of\n    to: b\nmeta:\n  new: \"2\"\n"
	if err := os.WriteFile(doc, []byte(body), 0o600); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	stdout, err := topicCovExec(t, "update", "a", doc)
	if err != nil {
		t.Fatalf("update from document: %v", err)
	}
	var got topicJSON
	decodeJSONData(t, stdout, &got)
	if len(got.Links) != 1 || got.Links[0].Kind != topic.KindPartOf {
		t.Fatalf("links = %+v, want only part_of", got.Links)
	}
	if len(got.Meta) != 1 || got.Meta["new"] != "2" {
		t.Fatalf("meta = %+v, want only new=2", got.Meta)
	}
}

// JSON and YAML go through ONE decoder, so a caller may hand over either.
func TestTopicUpdate_AcceptsJSONDocument(t *testing.T) {
	graphEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetIn(strings.NewReader(`{"meta":{"from":"json"}}`))
	rootCmd.SetArgs([]string{"topic", "update", "a", "-", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update from JSON: %v", err)
	}
	var got topicJSON
	decodeJSONData(t, stdout, &got)
	if got.Meta["from"] != "json" {
		t.Fatalf("meta = %+v", got.Meta)
	}
}

// An absent section is not an empty one: a document with only `meta:` must leave links alone,
// or clearing one field would require restating the other.
func TestTopicUpdate_AbsentSectionIsUntouched(t *testing.T) {
	graphEnv(t)
	if _, err := topicCovExec(t, "link", "a", "depends_on", "b"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetIn(strings.NewReader("meta:\n  only: \"1\"\n"))
	rootCmd.SetArgs([]string{"topic", "update", "a", "-", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("update: %v", err)
	}
	var got topicJSON
	decodeJSONData(t, stdout, &got)
	if len(got.Links) != 1 {
		t.Fatalf("a document without links: must not clear them, got %+v", got.Links)
	}
	// And an explicitly empty section DOES clear.
	resetCommandState(t)
	stdout, _ = resetCommandIO()
	rootCmd.SetIn(strings.NewReader("links: []\n"))
	rootCmd.SetArgs([]string{"topic", "update", "a", "-", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("clear links: %v", err)
	}
	var cleared topicJSON
	decodeJSONData(t, stdout, &cleared)
	if len(cleared.Links) != 0 {
		t.Fatalf("links: [] must clear, got %+v", cleared.Links)
	}
}

func TestTopicUpdate_FlagsAndDocumentExclude(t *testing.T) {
	env := graphEnv(t)
	doc := filepath.Join(env.RootDir, "d.yaml")
	if err := os.WriteFile(doc, []byte("meta: {a: b}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := topicCovExec(t, "update", "a", doc, "--meta", "x=y")
	if err == nil {
		t.Fatal("flags and a document must not combine")
	}
	if e := output.Classify(err); e.Code != output.CodeUsage {
		t.Fatalf("code = %q, want usage", e.Code)
	}

	// Neither names what was expected instead of failing vaguely.
	_, err = topicCovExec(t, "update", "a")
	if err == nil {
		t.Fatal("an update with nothing to do must be refused")
	}
	e := output.Classify(err)
	if e.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want needs_input", e.Code)
	}
	if e.Details["missing"] == nil {
		t.Error("needs_input must name what is missing")
	}
}

func TestTopicUpdate_UnreadableDocumentAndBadSyntax(t *testing.T) {
	env := graphEnv(t)

	_, err := topicCovExec(t, "update", "a", filepath.Join(env.RootDir, "nope.yaml"))
	if err == nil {
		t.Fatal("an unreadable document must be refused")
	}
	if e := output.Classify(err); e.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want needs_input", e.Code)
	}

	bad := filepath.Join(env.RootDir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("links: [oops\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err = topicCovExec(t, "update", "a", bad)
	if err == nil {
		t.Fatal("an unparseable document must be refused")
	}
	if e := output.Classify(err); e.Code != output.CodeUsage {
		t.Fatalf("code = %q, want usage", e.Code)
	}

	empty := filepath.Join(env.RootDir, "empty.yaml")
	if err := os.WriteFile(empty, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err = topicCovExec(t, "update", "a", empty)
	if err == nil {
		t.Fatal("an empty document must be refused")
	}
	if e := output.Classify(err); e.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want needs_input", e.Code)
	}
}

// The size limit is a DEFAULT with an override, like every other limit in the tool.
func TestTopicUpdate_SizeLimitIsOverridable(t *testing.T) {
	env := graphEnv(t)
	doc := filepath.Join(env.RootDir, "big.yaml")
	body := "meta:\n  k: \"" + strings.Repeat("x", 200) + "\"\n"
	if err := os.WriteFile(doc, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := topicCovExec(t, "update", "a", doc, "--max-size", "50")
	if err == nil {
		t.Fatal("a document over the limit must be refused")
	}
	e := output.Classify(err)
	if e.Code != output.CodeUsage {
		t.Fatalf("code = %q, want usage", e.Code)
	}
	if !strings.Contains(e.Message, "--max-size") {
		t.Errorf("the refusal must name its own override, got %q", e.Message)
	}

	// 0 removes the limit entirely.
	if _, err := topicCovExec(t, "update", "a", doc, "--max-size", "0"); err != nil {
		t.Fatalf("--max-size 0 must lift the limit: %v", err)
	}
}

// A document may only name live topics, and --force does not override referential integrity:
// there is no user intent an edge to a nonexistent topic could be expressing.
func TestTopicUpdate_DocumentRefusesUnknownTarget(t *testing.T) {
	env := graphEnv(t)
	doc := filepath.Join(env.RootDir, "ghost.yaml")
	if err := os.WriteFile(doc, []byte("links:\n  - kind: part_of\n    to: ghost\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	for _, args := range [][]string{
		{"update", "a", doc},
		{"update", "a", doc, "--force"},
	} {
		_, err := topicCovExec(t, args...)
		if err == nil {
			t.Fatalf("%v must be refused", args)
		}
		if e := output.Classify(err); e.Code != output.CodeTopicUnknown {
			t.Errorf("%v code = %q, want topic_unknown", args, e.Code)
		}
	}
}

// The text renderers are the human half of the contract and had no coverage at all.
func TestTopicGraph_TextRenderers(t *testing.T) {
	graphEnv(t)
	if _, err := topicCovExec(t, "link", "a", "depends_on", "b"); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	if _, err := topicCovExec(t, "update", "a", "--meta", "acme.pbi=2072958"); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	showText := topicCovCaptureStdout(t, func() {
		resetCommandState(t)
		rootCmd.SetArgs([]string{"topic", "show", "a", "--output", "text"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show text: %v", err)
		}
	})
	for _, want := range []string{"Links", "depends_on", "Meta", "acme.pbi", "2072958"} {
		if !strings.Contains(showText, want) {
			t.Errorf("show text is missing %q:\n%s", want, showText)
		}
	}

	// The inbound direction is drawn on the other side.
	inbound := topicCovCaptureStdout(t, func() {
		resetCommandState(t)
		rootCmd.SetArgs([]string{"topic", "show", "b", "--output", "text"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("show b text: %v", err)
		}
	})
	if !strings.Contains(inbound, "Linked from") {
		t.Errorf("show text omits the inbound section:\n%s", inbound)
	}

	listText := topicCovCaptureStdout(t, func() {
		resetCommandState(t)
		rootCmd.SetArgs([]string{"topic", "list", "--output", "text"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("list text: %v", err)
		}
	})
	// The list line carries a COUNT; the graph itself belongs to show.
	if !strings.Contains(listText, "link(s)") {
		t.Errorf("list text omits the link count:\n%s", listText)
	}

	linkText := topicCovCaptureStdout(t, func() {
		resetCommandState(t)
		rootCmd.SetArgs([]string{"topic", "unlink", "a", "depends_on", "b", "--output", "text"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unlink text: %v", err)
		}
	})
	if !strings.Contains(linkText, "removed") {
		t.Errorf("unlink text does not say what happened:\n%s", linkText)
	}
}

// A document declaring NO section is a convergent no-op — a template that renders `{}` must
// exit 0 saying so, not fail. An empty FILE is a different thing and stays needs_input.
func TestTopicUpdate_EmptyDocumentIsANoOp(t *testing.T) {
	graphEnv(t)
	if _, err := topicCovExec(t, "link", "a", "depends_on", "b"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	output.ResetVerdict()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetIn(strings.NewReader("{}\n"))
	rootCmd.SetArgs([]string{"topic", "update", "a", "-", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("an empty document must converge, got %v", err)
	}
	if outcome, _ := output.EmittedVerdict(); outcome != output.OutcomeSuccess {
		t.Fatalf("outcome = %q, want success", outcome)
	}

	var envelope struct {
		Summary string    `json:"summary"`
		Data    topicJSON `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope: %v\n%s", err, stdout.String())
	}
	if envelope.Summary != "nothing to update" {
		t.Errorf("summary = %q, want it to say nothing was done", envelope.Summary)
	}
	// And it changed nothing: the seeded edge survives.
	if len(envelope.Data.Links) != 1 {
		t.Errorf("a no-op document altered the graph: %+v", envelope.Data.Links)
	}
}

// A cycle refusal must name the invocation THAT CALLER can re-run. `topic link` recovers by
// appending one edge; `topic update` recovers by re-applying the whole document. Advertising the
// link form to a document caller would hand them a command that builds a different graph.
func TestCycleRefusalNamesTheCallersOwnRecovery(t *testing.T) {
	env := graphEnv(t)
	if _, err := topicCovExec(t, "link", "a", "depends_on", "b"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The link path.
	_, err := topicCovExec(t, "link", "b", "depends_on", "a")
	if err == nil {
		t.Fatal("cycle must be refused")
	}
	linkNext := forceArgv(t, err)
	want := "hydra topic link b depends_on a --force"
	if linkNext != want {
		t.Errorf("link recovery = %q, want %q", linkNext, want)
	}

	// The document path: same store error, different recovery.
	doc := filepath.Join(env.RootDir, "cycle.yaml")
	if err := os.WriteFile(doc, []byte("links:\n  - kind: depends_on\n    to: a\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err = topicCovExec(t, "update", "b", doc)
	if err == nil {
		t.Fatal("cycle in a document must be refused")
	}
	docNext := forceArgv(t, err)
	want = "hydra topic update b " + doc + " --force"
	if docNext != want {
		t.Errorf("document recovery = %q, want %q", docNext, want)
	}
	// Re-running exactly what was advertised must succeed, or the suggestion is a lie.
	if _, err := topicCovExec(t, "update", "b", doc, "--force"); err != nil {
		t.Fatalf("the advertised recovery failed: %v", err)
	}
}

// forceArgv returns the single --force recovery an error advertises.
func forceArgv(t *testing.T, err error) string {
	t.Helper()
	e := output.Classify(err)
	if e.Code != output.CodeTopicCycle {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeTopicCycle)
	}
	if len(e.Next) != 1 {
		t.Fatalf("next = %+v, want exactly one recovery", e.Next)
	}
	return strings.Join(e.Next[0].Argv, " ")
}

// Completion is a user-facing surface: a wrong kind list sends someone down a path the command
// will then refuse.
func TestCompleteTopicLinkArgs(t *testing.T) {
	graphEnv(t)
	// Completion reads the resolved project, which only a real invocation populates.
	if _, err := topicCovExec(t, "list"); err != nil {
		t.Fatalf("load project: %v", err)
	}
	kinds, _ := completeTopicLinkArgs(rootCmd, []string{"a"}, "")
	if len(kinds) != 2 || kinds[0] != topic.KindPartOf || kinds[1] != topic.KindDependsOn {
		t.Fatalf("kind completion = %v, want the two reserved kinds", kinds)
	}
	ids, _ := completeTopicLinkArgs(rootCmd, nil, "")
	if len(ids) == 0 {
		t.Error("the first position must complete topic ids")
	}
	targets, _ := completeTopicLinkArgs(rootCmd, []string{"a", topic.KindPartOf}, "")
	if len(targets) == 0 {
		t.Error("the target position must complete topic ids")
	}
	if extra, _ := completeTopicLinkArgs(rootCmd, []string{"a", "b", "c"}, ""); extra != nil {
		t.Errorf("a fourth argument has nothing to complete, got %v", extra)
	}
}

// doctor's dangling-link detection and repair, driven through the command so the check reaches
// the envelope a caller actually reads.
func TestDoctorRepairsDanglingLink(t *testing.T) {
	env := graphEnv(t)
	// Only hand-edited state can produce this: the CLI sweeps inbound edges on delete.
	state := filepath.Join(env.RootDir, ".hydra", "state.yaml")
	body := "version: \"2\"\ntopics:\n  a:\n    members:\n      - repo: api\n        branch: stage\n" +
		"    links:\n      - kind: depends_on\n        to: never-existed\n"
	if err := os.WriteFile(state, []byte(body), 0o600); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	found := danglingLinkCheck(t)
	if found == nil {
		t.Fatal("doctor did not report the dangling relationship")
	}
	if found["status"] != "fail" || found["fixable"] != true {
		t.Fatalf("check = %#v, want a fixable failure", found)
	}
	// Typed fields, so --fix never parses the message to learn which edge to drop.
	if found["link_kind"] != topic.KindDependsOn || found["link_to"] != "never-existed" {
		t.Fatalf("check must identify the edge in typed fields, got %#v", found)
	}

	resetCommandState(t)
	_, _ = resetCommandIO()
	rootCmd.SetArgs([]string{"doctor", "--fix", "--output", "json"})
	_ = rootCmd.Execute() // doctor exits non-zero while findings remain; the repair is the subject

	after, _, err := topicCovStore(env).Get("a")
	if err != nil {
		t.Fatalf("get after fix: %v", err)
	}
	if len(after.Links) != 0 {
		t.Fatalf("the edge survived the fix: %+v", after.Links)
	}
	if again := danglingLinkCheck(t); again != nil {
		t.Fatalf("the check fired again after --fix: %#v", again)
	}
}

// danglingLinkCheck runs doctor and returns its dangling-link check, if any.
func danglingLinkCheck(t *testing.T) map[string]any {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"doctor", "--output", "json"})
	_ = rootCmd.Execute() // a failing check is the point; the envelope still lands

	var envelope struct {
		Data struct {
			Checks []map[string]any `json:"checks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("doctor envelope: %v\n%s", err, stdout.String())
	}
	for _, c := range envelope.Data.Checks {
		if c["id"] == checkTopicDanglingLink {
			return c
		}
	}
	return nil
}

// --output yaml must reach the command layer, not just the output package.
func TestTopicShow_YAMLOutput(t *testing.T) {
	graphEnv(t)

	var buf *bytes.Buffer
	resetCommandState(t)
	buf, _ = resetCommandIO()
	rootCmd.SetArgs([]string{"topic", "show", "a", "--output", "yaml"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("show yaml: %v", err)
	}
	body := buf.String()
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatalf("yaml mode emitted JSON:\n%s", body)
	}
	for _, want := range []string{"command: topic show", "schema: 3", "id: a"} {
		if !strings.Contains(body, want) {
			t.Errorf("yaml envelope missing %q:\n%s", want, body)
		}
	}
}
