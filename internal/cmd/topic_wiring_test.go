package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
)

// attach records membership directly through the store. The topic command tree
// does not exist yet, so the tests exercise the wiring rather than a CLI verb.
func attach(t *testing.T, root, id, repo, branch string) {
	t.Helper()
	if err := topic.Open(root).Attach(id, topic.Member{Repo: repo, Branch: branch}); err != nil {
		t.Fatalf("attach %s/%s to %s: %v", repo, branch, id, err)
	}
}

func decodeList(t *testing.T, stdout *bytes.Buffer) listProjectPayload {
	t.Helper()
	var envelope struct {
		Data listProjectPayload `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode list envelope: %v\nstdout: %s", err, stdout.String())
	}
	return envelope.Data
}

// Membership must reach the JSON envelope, and an unassigned worktree must report
// topic: null rather than omitting the field — absent and unassigned are different
// answers to a consumer.
func TestList_ReportsTopicMembership(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	env.CreateWorktree("backend", "api", "feat/login", "api-login")
	env.CreateWorktree("backend", "api", "feat/loose", "api-loose")
	attach(t, env.RootDir, "2072958", "api", "feat/login")

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"list", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	if !strings.Contains(stdout.String(), `"topic"`) {
		t.Fatalf("the envelope must carry a topic field:\n%s", stdout.String())
	}

	data := decodeList(t, stdout)
	var assigned, unassigned int
	for _, wt := range data.Worktrees {
		switch wt.Branch {
		case "feat/login":
			if wt.Topic == nil || *wt.Topic != "2072958" {
				t.Errorf("feat/login topic = %v, want 2072958", wt.Topic)
			}
			assigned++
		case "feat/loose":
			if wt.Topic != nil {
				t.Errorf("feat/loose must be unassigned, got %q", *wt.Topic)
			}
			unassigned++
		}
	}
	if assigned != 1 || unassigned != 1 {
		t.Fatalf("expected one assigned and one unassigned worktree, got %d/%d", assigned, unassigned)
	}
}

// --topic narrows to exact recorded membership. A branch whose NAME contains the
// id but which was never attached must not appear: identity is what was recorded,
// never what a name looks like.
func TestList_TopicFilterUsesMembershipNotNames(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	env.CreateWorktree("backend", "api", "feat/2072958-login", "api-login")
	env.CreateWorktree("backend", "api", "hotfix/unrelated", "api-hotfix")
	// Only the name-mismatched worktree is a real member.
	attach(t, env.RootDir, "2072958", "api", "hotfix/unrelated")

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"list", "--topic", "2072958", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list --topic: %v", err)
	}

	data := decodeList(t, stdout)
	if len(data.Worktrees) != 1 {
		t.Fatalf("expected exactly the recorded member, got %d worktrees", len(data.Worktrees))
	}
	if got := data.Worktrees[0].Branch; got != "hotfix/unrelated" {
		t.Errorf("filter returned %q; membership must beat name matching", got)
	}
}

// An unknown topic is an error carrying the valid ids, never an empty list. An
// empty list is indistinguishable from "that topic has no work", which is how a
// caller concludes nothing exists and starts over.
func TestList_UnknownTopicIsAnErrorNotAnEmptyList(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()
	env.CreateWorktree("backend", "api", "feat/login", "api-login")
	attach(t, env.RootDir, "2072958", "api", "feat/login")

	resetCommandState(t)
	rootCmd.SetArgs([]string{"list", "--topic", "9999999", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("an unknown topic must fail")
	}

	classified := output.Classify(err)
	if classified.Code != output.CodeTopicUnknown {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeTopicUnknown)
	}
	known, ok := classified.Details["known"].([]string)
	if !ok {
		t.Fatalf("details.known must list valid ids, got %#v", classified.Details["known"])
	}
	if len(known) != 1 || known[0] != "2072958" {
		t.Errorf("details.known = %v, want [2072958]", known)
	}
}

// remove must detach the worktree it removed, so a later listing does not report a
// topic containing something that no longer exists.
func TestRemove_DetachesMembership(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	env.CreateWorktree("backend", "api", "feat/login", "api-login")
	attach(t, env.RootDir, "2072958", "api", "feat/login")

	resetCommandState(t)
	rootCmd.SetArgs([]string{"remove", "api", "feat/login", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got, ok, err := topic.Open(env.RootDir).Get("2072958")
	if err != nil {
		t.Fatalf("get topic: %v", err)
	}
	if ok && len(got.Members) != 0 {
		t.Errorf("membership survived removal: %+v", got.Members)
	}
}

// The ordering rule: detach happens AFTER the worktree is gone. A refused removal
// must leave membership intact, or the worktree becomes indistinguishable from
// genuinely unassigned work and nothing reports it.
func TestRemove_RefusedRemovalKeepsMembership(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	worktree := env.CreateWorktree("backend", "api", "feat/dirty", "api-dirty")
	attach(t, env.RootDir, "2072958", "api", "feat/dirty")
	if err := os.WriteFile(worktree+"/uncommitted.txt", []byte("work"), 0o644); err != nil {
		t.Fatalf("dirty the worktree: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"remove", "api", "feat/dirty", "--yes"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("removing a dirty worktree without --force must fail")
	}

	got, ok, err := topic.Open(env.RootDir).Get("2072958")
	if err != nil {
		t.Fatalf("get topic: %v", err)
	}
	if !ok || len(got.Members) != 1 {
		t.Fatalf("membership must survive a refused removal, got ok=%v members=%+v", ok, got.Members)
	}
}

// Membership that outlives its worktree is drift doctor must name and fix. This is
// the exact state an interrupted remove leaves behind.
func TestDoctor_DetectsAndFixesDanglingMember(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	env.CreateWorktree("backend", "api", "feat/login", "api-login")
	attach(t, env.RootDir, "2072958", "api", "feat/login")
	// A member with no worktree: recorded for a branch that has none.
	attach(t, env.RootDir, "2072958", "api", "feat/vanished")

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"doctor", "--output", "json"})
	_ = rootCmd.Execute() // a failing check is a non-nil error by design
	out := stdout.String()
	if !strings.Contains(out, checkTopicDanglingMember) {
		t.Fatalf("doctor must report %s:\n%s", checkTopicDanglingMember, out)
	}
	if !strings.Contains(out, "feat/vanished") {
		t.Errorf("the report must name the dangling branch:\n%s", out)
	}
	if strings.Count(out, checkTopicDanglingMember) != 1 {
		t.Errorf("the healthy member must not be reported:\n%s", out)
	}

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"doctor", "--fix", "--output", "json"})
	_ = rootCmd.Execute()

	got, _, err := topic.Open(env.RootDir).Get("2072958")
	if err != nil {
		t.Fatalf("get topic: %v", err)
	}
	if len(got.Members) != 1 || got.Members[0].Branch != "feat/login" {
		t.Fatalf("--fix must detach only the dangling member, got %+v", got.Members)
	}
}

// A workspace that has never used topics must list normally. Membership is an
// addition, not a precondition.
func TestList_WorksWithNoTopicState(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()
	env.CreateWorktree("backend", "api", "feat/login", "api-login")

	if _, err := os.Stat(topic.Path(env.RootDir)); !os.IsNotExist(err) {
		t.Fatalf("precondition: state must not exist yet, got %v", err)
	}

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"list", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list without topic state: %v", err)
	}

	data := decodeList(t, stdout)
	if len(data.Worktrees) == 0 {
		t.Fatal("listing must still return worktrees without any topic state")
	}
	for _, wt := range data.Worktrees {
		if wt.Topic != nil {
			t.Errorf("%s must be unassigned, got %q", wt.Branch, *wt.Topic)
		}
	}
	// Reading must not create state; only a mutation may.
	if _, err := os.Stat(topic.Path(env.RootDir)); !os.IsNotExist(err) {
		t.Errorf("a read-only command must not create topic state: %v", err)
	}
}
