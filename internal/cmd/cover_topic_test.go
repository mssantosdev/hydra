package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
)

func topicCovEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage", "feat/two")
	env.Chdir()
	env.CreateWorktree("backend", "api", "stage", "api-stage")
	env.CreateWorktree("backend", "api", "feat/two", "api-feat-two")
	return env
}

func topicCovExec(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"topic"}, append(args, "--output", "json")...))
	return stdout, rootCmd.Execute()
}

func topicCovStore(env *testutil.TestEnv) *topic.Store {
	return topic.Open(env.RootDir)
}

func topicCovAssertAbsent(t *testing.T, store *topic.Store, id string) {
	t.Helper()
	_, ok, err := store.Get(id)
	if err != nil {
		t.Fatalf("store.Get(%q): %v", id, err)
	}
	if ok {
		t.Fatalf("topic %q must be absent from the store after garbage collection", id)
	}
	names, err := store.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	for _, name := range names {
		if name == id {
			t.Fatalf("topic %q still listed in Names()", id)
		}
	}
}

func topicCovCaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stdout: %v", err)
	}
	return buf.String()
}

func decodeBlockers(raw any) ([]blocker, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var out []blocker
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func TestTopicCovList_EmptyWorkspace(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)

	stdout, err := topicCovExec(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var payload topicListJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Total != 0 || len(payload.Topics) != 0 {
		t.Fatalf("got %d topics, want none", payload.Total)
	}
	if names, err := store.Names(); err != nil || len(names) != 0 {
		t.Fatalf("store must be empty, names=%v err=%v", names, err)
	}
}

func TestTopicCovList_SeveralTopics(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	for _, spec := range []struct{ id, branch string }{{"alpha", "stage"}, {"beta", "feat/two"}, {"gamma", "main"}} {
		id, branch := spec.id, spec.branch
		if err := store.Attach(id, topic.Member{Repo: "api", Branch: branch}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	stdout, err := topicCovExec(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var payload topicListJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Total != 3 || len(payload.Topics) != 3 {
		t.Fatalf("list total=%d topics=%d, want 3", payload.Total, len(payload.Topics))
	}
	names, err := store.Names()
	if err != nil || len(names) != 3 {
		t.Fatalf("store names=%v err=%v, want three topics", names, err)
	}
}

func TestTopicCovList_ClosedAndReopenVisibility(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("open-one", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := store.Attach("closed-one", topic.Member{Repo: "api", Branch: "feat/two"}); err != nil {
		t.Fatalf("seed closed: %v", err)
	}
	if err := store.SetClosed("closed-one", true); err != nil {
		t.Fatalf("mark closed: %v", err)
	}

	stdout, err := topicCovExec(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed topicListJSON
	decodeJSONData(t, stdout, &listed)
	var sawOpen, sawClosed bool
	for _, item := range listed.Topics {
		switch item.ID {
		case "open-one":
			sawOpen = true
			if item.Closed {
				t.Error("open topic must not report closed in list JSON")
			}
		case "closed-one":
			sawClosed = true
			if !item.Closed {
				t.Error("closed topic must report closed:true in list JSON")
			}
		}
	}
	if !sawOpen || !sawClosed {
		t.Fatalf("list payload missing topics: %+v", listed.Topics)
	}

	got, ok, err := store.Get("closed-one")
	if err != nil || !ok || !got.Closed {
		t.Fatalf("store closed flag: ok=%v closed=%v err=%v", ok, got.Closed, err)
	}

	if _, err := topicCovExec(t, "close", "closed-one", "--reopen"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	reopened, ok, err := store.Get("closed-one")
	if err != nil || !ok || reopened.Closed {
		t.Fatalf("store must be reopened, ok=%v closed=%v err=%v", ok, reopened.Closed, err)
	}
}

func TestTopicCovShow_MembersJoinedToDisk(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("T100", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	wantPath := env.GetWorktreePath("backend", "api-stage")

	stdout, err := topicCovExec(t, "show", "T100")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var payload topicJSON
	decodeJSONData(t, stdout, &payload)
	if len(payload.Members) != 1 {
		t.Fatalf("members=%+v", payload.Members)
	}
	m := payload.Members[0]
	if m.Repo != "api" || m.Branch != "stage" || m.Path != wantPath || !m.Present {
		t.Fatalf("member=%+v, want api/stage at %s present", m, wantPath)
	}
	got, ok, err := store.Get("T100")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("store membership: ok=%v members=%+v err=%v", ok, got.Members, err)
	}
}

func TestTopicCovShow_UnknownListsKnown(t *testing.T) {
	env := topicCovEnv(t)
	if err := topicCovStore(env).Attach("real", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := topicCovExec(t, "show", "nope")
	if err == nil {
		t.Fatal("unknown id must fail")
	}
	he := output.Classify(err)
	if he.Code != output.CodeTopicUnknown {
		t.Fatalf("code=%s, want topic_unknown", he.Code)
	}
	known, ok := he.Details["known"].([]string)
	if !ok || len(known) != 1 || known[0] != "real" {
		t.Fatalf("details.known=%#v, want [real]", he.Details["known"])
	}
}

func TestTopicCovAttach_RecordsMembership(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)

	stdout, err := topicCovExec(t, "attach", "new-topic", "backend/api-stage")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	var payload topicMembershipJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Topic != "new-topic" || payload.Repo != "api" || payload.Branch != "stage" {
		t.Fatalf("payload=%+v", payload)
	}
	got, ok, err := store.Get("new-topic")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("store after attach: ok=%v members=%+v err=%v", ok, got.Members, err)
	}
}

func TestTopicCovAttach_SameTopicConverges(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("same", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := topicCovExec(t, "attach", "same", "backend/api-stage"); err != nil {
		t.Fatalf("re-attach same topic must succeed: %v", err)
	}
	got, ok, err := store.Get("same")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("membership must remain one member: ok=%v members=%+v err=%v", ok, got.Members, err)
	}
}

func TestTopicCovAttach_DifferentTopicConflicts(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("first", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := topicCovExec(t, "attach", "second", "backend/api-stage")
	if err == nil {
		t.Fatal("second topic attach must fail")
	}
	he := output.Classify(err)
	if he.Code != output.CodeTopicConflict {
		t.Fatalf("code=%s, want topic_conflict", he.Code)
	}
	if he.Details["existing_topic"] != "first" {
		t.Fatalf("existing_topic=%v, want first", he.Details["existing_topic"])
	}
	got, ok, err := store.Get("first")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("first topic membership must be unchanged: ok=%v members=%+v", ok, got.Members)
	}
	if _, ok, _ := store.Get("second"); ok {
		t.Fatal("conflicting topic must not be created")
	}
}

func TestTopicCovDetach_RemovesMemberAndCollectsTopic(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	for _, m := range []topic.Member{{Repo: "api", Branch: "stage"}, {Repo: "api", Branch: "feat/two"}} {
		if err := store.Attach("bundle", m); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if _, err := topicCovExec(t, "detach", "bundle", "backend/api-stage"); err != nil {
		t.Fatalf("detach one: %v", err)
	}
	got, ok, err := store.Get("bundle")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("one member must remain: ok=%v members=%+v err=%v", ok, got.Members, err)
	}
	if !env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Fatal("detach must not remove the worktree directory")
	}

	if _, err := topicCovExec(t, "detach", "bundle", "backend/api-feat-two"); err != nil {
		t.Fatalf("detach last: %v", err)
	}
	topicCovAssertAbsent(t, store, "bundle")
}

func TestTopicCovClose_ReportsEveryBlockerAtOnce(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("epic", topic.Member{Repo: "api", Branch: "main"}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := store.Attach("b-open", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed open child: %v", err)
	}
	if _, err := store.AddLink("b-open", topic.Link{Kind: topic.KindPartOf, To: "epic"}, false); err != nil {
		t.Fatalf("link open child: %v", err)
	}
	if err := store.Attach("a-stranded", topic.Member{Repo: "web", Branch: "feat/y"}); err != nil {
		t.Fatalf("seed stranded child: %v", err)
	}
	if _, err := store.AddLink("a-stranded", topic.Link{Kind: topic.KindPartOf, To: "epic"}, false); err != nil {
		t.Fatalf("link stranded child: %v", err)
	}
	if err := store.SetClosed("a-stranded", true); err != nil {
		t.Fatalf("close stranded child: %v", err)
	}

	_, err := topicCovExec(t, "close", "epic")
	if err == nil {
		t.Fatal("close must refuse while blockers remain")
	}
	he := output.Classify(err)
	if he.Code != output.CodeTopicNotCloseable {
		t.Fatalf("code=%s, want topic_not_closeable", he.Code)
	}
	raw, ok := he.Details["blocked_by"]
	if !ok {
		t.Fatalf("details.blocked_by missing: %#v", he.Details)
	}
	blockers, err := decodeBlockers(raw)
	if err != nil {
		t.Fatalf("decode blocked_by: %v", err)
	}
	if len(blockers) < 2 {
		t.Fatalf("want every blocker in one response, got %+v", blockers)
	}
	reasons := map[string]bool{}
	for _, b := range blockers {
		reasons[b.Reason] = true
	}
	if !reasons[reasonOpen] {
		t.Errorf("missing %q blocker, got %+v", reasonOpen, blockers)
	}
	if !reasons[reasonNoTarget] {
		t.Errorf("missing %q blocker, got %+v", reasonNoTarget, blockers)
	}
	epic, ok, err := store.Get("epic")
	if err != nil || !ok || epic.Closed {
		t.Fatalf("refused close must not mark closed in store: ok=%v closed=%v err=%v", ok, epic.Closed, err)
	}
}

func TestTopicCovClose_SucceedsWhenChildrenLanded(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("epic", topic.Member{Repo: "api", Branch: "main"}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := store.Attach("feat", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed child: %v", err)
	}
	if _, err := store.AddLink("feat", topic.Link{Kind: topic.KindPartOf, To: "epic"}, false); err != nil {
		t.Fatalf("parent link: %v", err)
	}
	if err := store.SetClosed("feat", true); err != nil {
		t.Fatalf("close child: %v", err)
	}

	if _, err := topicCovExec(t, "close", "epic"); err != nil {
		t.Fatalf("close parent: %v", err)
	}
	epic, ok, err := store.Get("epic")
	if err != nil || !ok || !epic.Closed {
		t.Fatalf("store must record closed parent: ok=%v closed=%v err=%v", ok, epic.Closed, err)
	}
}

func TestTopicCovClose_UnknownTopic(t *testing.T) {
	env := topicCovEnv(t)
	if err := topicCovStore(env).Attach("real", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := topicCovExec(t, "close", "missing")
	if err == nil {
		t.Fatal("close on unknown id must fail")
	}
	he := output.Classify(err)
	if he.Code != output.CodeTopicUnknown {
		t.Fatalf("code=%s, want topic_unknown", he.Code)
	}
	known, ok := he.Details["known"].([]string)
	if !ok || len(known) != 1 || known[0] != "real" {
		t.Fatalf("details.known=%#v, want [real]", he.Details["known"])
	}
}

func TestTopicCovRemove_WithoutWorktreesKeepsDisk(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("solo", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := topicCovExec(t, "remove", "solo", "--yes"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Fatal("worktree must survive membership-only remove")
	}
	topicCovAssertAbsent(t, store, "solo")
}

func TestTopicCovRemove_WithWorktreesRemovesDisk(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("gone", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := topicCovExec(t, "remove", "gone", "--with-worktrees", "--yes"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Fatal("worktree must be removed with --with-worktrees")
	}
	topicCovAssertAbsent(t, store, "gone")
}

func TestTopicCovRemove_NonTTYWithoutYesRefuses(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("keep", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := topicCovExec(t, "remove", "keep", "--with-worktrees")
	if err == nil {
		t.Fatal("non-TTY remove without --yes must refuse")
	}
	he := output.Classify(err)
	if he.Code != output.CodeNeedsInput {
		t.Fatalf("code=%s, want needs_input", he.Code)
	}
	missing, ok := he.Details["missing"].([]string)
	if !ok || len(missing) != 1 || missing[0] != "--yes" {
		t.Fatalf("details.missing=%#v, want [--yes]", he.Details["missing"])
	}
	if !env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Fatal("refused remove must not touch worktree")
	}
	got, ok, err := store.Get("keep")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("membership must survive refusal: ok=%v members=%+v err=%v", ok, got.Members, err)
	}
}

func TestTopicCovRemove_UnknownIsNotSilentSuccess(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)

	_, err := topicCovExec(t, "remove", "ghost", "--yes")
	if err == nil {
		t.Fatal("remove unknown topic must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeTopicUnknown {
		t.Fatalf("code=%s, want topic_unknown", code)
	}
	if names, err := store.Names(); err != nil || len(names) != 0 {
		t.Fatalf("store must remain empty: names=%v err=%v", names, err)
	}
}

func TestTopicCovRemove_PostRemoveFiresPerWorktree(t *testing.T) {
	env := topicCovEnv(t)
	logPath := filepath.Join(env.RootDir, "post-remove.log")
	cfg := env.LoadConfig()
	cfg.Hooks.PostRemove = []config.Hook{{
		Run: `sh -c 'echo "$HYDRA_REPO/$HYDRA_BRANCH" >> "` + logPath + `"'`,
	}}
	env.SaveConfig(cfg)
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	if err := loadProject(); err != nil {
		t.Fatalf("loadProject: %v", err)
	}
	trustCurrentWorkspace(t)

	store := topicCovStore(env)
	for _, branch := range []string{"main", "stage"} {
		if err := store.Attach("bundle", topic.Member{Repo: "api", Branch: branch}); err != nil {
			t.Fatalf("attach %s: %v", branch, err)
		}
	}

	if _, err := topicCovExec(t, "remove", "bundle", "--with-worktrees", "--yes"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(env.ReadFile(t, logPath)), "\n")
	if len(lines) != 2 {
		t.Fatalf("post_remove lines=%v, want one per worktree", lines)
	}
	combined := lines[0] + lines[1]
	if !strings.Contains(combined, "api/main") || !strings.Contains(combined, "api/stage") {
		t.Fatalf("post_remove context missing repo/branch: %v", lines)
	}
	topicCovAssertAbsent(t, store, "bundle")
}

func TestTopicCovRemove_DeleteBranchGateRefusesUnmerged(t *testing.T) {
	env := topicCovEnv(t)
	unmergedPath := env.CreateWorktree("backend", "api", "feature/unmerged", "api-unmerged")
	env.CreateCommit(unmergedPath, "unique work")
	store := topicCovStore(env)
	if err := store.Attach("branches", topic.Member{Repo: "api", Branch: "feature/unmerged"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := topicCovExec(t, "remove", "branches", "--with-worktrees", "--yes", "--delete-branch")
	if err == nil {
		t.Fatal("unmerged --delete-branch must refuse before any mutation")
	}
	if code := output.Classify(err).Code; code != output.CodeGitFailed {
		t.Fatalf("code=%s, want git_failed", code)
	}
	if !env.DirExists(unmergedPath) {
		t.Fatal("worktree must survive refused delete-branch gate")
	}
	got, ok, err := store.Get("branches")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("membership must survive refusal: ok=%v members=%+v err=%v", ok, got.Members, err)
	}
}

func TestTopicCovLinks_RecordedAndCycleRefused(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	for id, branch := range map[string]string{"a": "main", "b": "stage", "c": "feat/two"} {
		if err := store.Attach(id, topic.Member{Repo: "api", Branch: branch}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	if _, err := store.AddLink("b", topic.Link{Kind: topic.KindPartOf, To: "a"}, false); err != nil {
		t.Fatalf("b->a: %v", err)
	}
	if _, err := store.AddLink("c", topic.Link{Kind: topic.KindPartOf, To: "b"}, false); err != nil {
		t.Fatalf("c->b: %v", err)
	}
	_, err := store.AddLink("a", topic.Link{Kind: topic.KindPartOf, To: "c"}, false)
	var cycle *topic.ErrCycle
	if !errors.As(err, &cycle) {
		t.Fatalf("cycle must be refused, got %v", err)
	}
	a, ok, err := store.Get("a")
	if err != nil || !ok || len(a.Links) != 0 {
		t.Fatalf("refused cycle must not persist an edge on a: ok=%v links=%+v err=%v", ok, a.Links, err)
	}

	b, ok, err := store.Get("b")
	if err != nil || !ok || len(b.Links) != 1 || b.Links[0].To != "a" {
		t.Fatalf("edge not recorded: ok=%v links=%+v err=%v", ok, b.Links, err)
	}

	stdout, err := topicCovExec(t, "show", "b")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var payload topicJSON
	decodeJSONData(t, stdout, &payload)
	if len(payload.Links) != 1 || payload.Links[0].Kind != topic.KindPartOf || payload.Links[0].To != "a" {
		t.Fatalf("show links=%+v, want one part_of a", payload.Links)
	}
	// The reverse direction is derived and reported, so "what is inside a" is answerable
	// without listing every topic.
	stdout, err = topicCovExec(t, "show", "a")
	if err != nil {
		t.Fatalf("show a: %v", err)
	}
	var parent topicJSON
	decodeJSONData(t, stdout, &parent)
	if len(parent.LinkedFrom) != 1 || parent.LinkedFrom[0].From != "b" {
		t.Fatalf("show a linked_from=%+v, want one from b", parent.LinkedFrom)
	}
}

func TestTopicCovListAndShow_TextPrinters(t *testing.T) {
	env := topicCovEnv(t)
	if err := topicCovStore(env).Attach("txt", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	listText := topicCovCaptureStdout(t, func() {
		resetCommandState(t)
		resetCommandIO()
		env.Chdir()
		projectRoot = env.RootDir
		projectConfigPath = config.ManifestPath(env.RootDir)
		if err := loadProject(); err != nil {
			t.Fatalf("loadProject: %v", err)
		}
		rootCmd.SetArgs([]string{"--output", "text", "topic", "list"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("text list: %v", err)
		}
	})
	if !strings.Contains(listText, "txt") {
		t.Fatalf("text list missing topic id: %s", listText)
	}

	showText := topicCovCaptureStdout(t, func() {
		resetCommandState(t)
		resetCommandIO()
		env.Chdir()
		projectRoot = env.RootDir
		projectConfigPath = config.ManifestPath(env.RootDir)
		if err := loadProject(); err != nil {
			t.Fatalf("loadProject: %v", err)
		}
		rootCmd.SetArgs([]string{"--output", "text", "topic", "show", "txt"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("text show: %v", err)
		}
	})
	if !strings.Contains(showText, "api") || !strings.Contains(showText, "stage") {
		t.Fatalf("text show missing member: %s", showText)
	}

	payload := topicRemoveJSON{
		Topic: "txt",
		Targets: []topicRemoveTargetJSON{{
			Repo: "api", Branch: "stage", Detached: true, WorktreeGone: true,
		}},
		Removed: true,
	}
	removeText := topicCovCaptureStdout(t, func() {
		printTopicRemoveText(payload, topicRemoveSummary(payload, 0))
	})
	if !strings.Contains(removeText, "api") {
		t.Fatalf("remove text missing repo: %s", removeText)
	}
}

func TestTopicCovCompletionHelpers(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("comp", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()

	ids, _ := completeTopicIDs(topicShowCmd, nil, "")
	if len(ids) != 1 || ids[0] != "comp" {
		t.Fatalf("completeTopicIDs=%v, want [comp]", ids)
	}
	attachArgs, _ := completeTopicAttachArgs(topicAttachCmd, []string{"comp"}, "")
	if len(attachArgs) == 0 {
		t.Fatalf("completeTopicAttachArgs step2=%v, want worktree names", attachArgs)
	}
	if !strings.Contains(strings.Join(attachArgs, ","), "api") {
		t.Fatalf("completeTopicAttachArgs=%v", attachArgs)
	}
	detachArgs, _ := completeTopicDetachArgs(topicDetachCmd, []string{"comp"}, "")
	if len(detachArgs) != 1 || !strings.Contains(detachArgs[0], "api") {
		t.Fatalf("completeTopicDetachArgs=%v", detachArgs)
	}
	_ = store
}

func TestTopicCovRemove_DryRunPreview(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	if err := store.Attach("preview", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stdout, err := topicCovExec(t, "remove", "preview", "--with-worktrees", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	var payload topicRemoveJSON
	decodeJSONData(t, stdout, &payload)
	if !payload.DryRun || len(payload.Targets) != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	got, ok, err := store.Get("preview")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("dry-run must not change store: ok=%v members=%+v err=%v", ok, got.Members, err)
	}
}

func TestTopicCovClose_NotMergedBlocker(t *testing.T) {
	env := topicCovEnv(t)
	store := topicCovStore(env)
	unmergedPath := env.CreateWorktree("backend", "api", "feature/unmerged", "api-unmerged")
	env.CreateCommit(unmergedPath, "child work")
	if err := store.Attach("epic", topic.Member{Repo: "api", Branch: "main"}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := store.Attach("feat", topic.Member{Repo: "api", Branch: "feature/unmerged"}); err != nil {
		t.Fatalf("seed child: %v", err)
	}
	if _, err := store.AddLink("feat", topic.Link{Kind: topic.KindPartOf, To: "epic"}, false); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := store.SetClosed("feat", true); err != nil {
		t.Fatalf("close child: %v", err)
	}
	if git.IsBranchMerged(env.GetBarePath("api"), "feature/unmerged", "main") {
		t.Fatal("precondition: child branch must not be merged into parent")
	}

	_, err := topicCovExec(t, "close", "epic")
	if err == nil {
		t.Fatal("close must refuse unmerged child work")
	}
	he := output.Classify(err)
	if he.Code != output.CodeTopicNotCloseable {
		t.Fatalf("code=%s, want topic_not_closeable", he.Code)
	}
	blockers, err := decodeBlockers(he.Details["blocked_by"])
	if err != nil {
		t.Fatalf("blocked_by: %v", err)
	}
	for _, b := range blockers {
		if b.Reason == reasonNotMerged {
			return
		}
	}
	t.Fatalf("want not_merged blocker, got %+v", blockers)
}
