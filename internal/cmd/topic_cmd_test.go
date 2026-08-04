package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
)

// topicEnv builds a workspace with two worktrees in one repo.
func topicEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()
	env.CreateWorktree("backend", "api", "stage", "api-stage")
	env.CreateWorktree("backend", "api", "feat/two", "api-feat-two")
	return env
}

func runTopic(t *testing.T, args ...string) error {
	t.Helper()
	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs(append([]string{"topic"}, args...))
	return rootCmd.Execute()
}

// attach is one of only two commands that may bring a topic into existence, which is
// what lets ad-hoc work be promoted with no migration step.
func TestTopicAttach_CreatesTheTopic(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)

	if _, ok, _ := topic.Open(env.RootDir).Get("2072958"); ok {
		t.Fatal("precondition: the topic must not exist yet")
	}

	if err := runTopic(t, "attach", "2072958", "backend/api-stage", "--output", "json"); err != nil {
		t.Fatalf("attach: %v", err)
	}

	got, ok, err := topic.Open(env.RootDir).Get("2072958")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || len(got.Members) != 1 {
		t.Fatalf("attach must upsert the topic, got ok=%v members=%+v", ok, got.Members)
	}
}

// Every other verb is a CONSUMER: an unknown id is an error carrying the valid ids,
// never an auto-create and never a branch-name match.
func TestTopic_ConsumersRequireAnExistingTopic(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	if err := topic.Open(env.RootDir).Attach("real", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, args := range [][]string{
		{"show", "nope"},
		{"detach", "nope", "backend/api-stage"},
		{"remove", "nope", "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			err := runTopic(t, append(args, "--output", "json")...)
			if err == nil {
				t.Fatalf("topic %v must fail on an unknown id", args)
			}
			classified := output.Classify(err)
			if classified.Code != output.CodeTopicUnknown {
				t.Fatalf("code = %q, want %q", classified.Code, output.CodeTopicUnknown)
			}
			known, ok := classified.Details["known"].([]string)
			if !ok || len(known) != 1 || known[0] != "real" {
				t.Errorf("details.known = %#v, want [real]", classified.Details["known"])
			}
		})
	}
}

// The aggregate dirty gate is the whole reason a multi-target removal is not a loop
// over the single-worktree path: it must inspect EVERY target and refuse the whole
// operation, leaving nothing half-destroyed.
func TestTopicRemove_DirtyGateRefusesBeforeAnyMutation(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	store := topic.Open(env.RootDir)
	for _, m := range []topic.Member{{Repo: "api", Branch: "stage"}, {Repo: "api", Branch: "feat/two"}} {
		if err := store.Attach("2072958", m); err != nil {
			t.Fatalf("seed %s: %v", m.Branch, err)
		}
	}
	// Dirty the SECOND target, so a per-item implementation would already have
	// destroyed the first by the time it noticed.
	env.MakeWorktreeDirty(env.GetWorktreePath("backend", "api-feat-two"))

	err := runTopic(t, "remove", "2072958", "--with-worktrees", "--yes", "--output", "json")
	if err == nil {
		t.Fatal("a dirty target must refuse the whole removal")
	}
	if code := output.Classify(err).Code; code != output.CodeWorktreeDirty {
		t.Fatalf("code = %q, want %q", code, output.CodeWorktreeDirty)
	}

	// Nothing may have happened: both worktrees and all membership survive.
	for _, dir := range []string{"api-stage", "api-feat-two"} {
		if !env.DirExists(env.GetWorktreePath("backend", dir)) {
			t.Errorf("%s was removed despite the refusal", dir)
		}
	}
	got, _, err := store.Get("2072958")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Members) != 2 {
		t.Errorf("membership changed despite the refusal: %+v", got.Members)
	}
}

// --force is the documented escape from the dirty gate.
func TestTopicRemove_ForceProceedsThroughDirty(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	if err := topic.Open(env.RootDir).Attach("2072958", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	env.MakeWorktreeDirty(env.GetWorktreePath("backend", "api-stage"))

	if err := runTopic(t, "remove", "2072958", "--with-worktrees", "--force", "--yes", "--output", "json"); err != nil {
		t.Fatalf("--force must proceed: %v", err)
	}
	if env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Error("the worktree should be gone")
	}
}

// A destructive multi-worktree operation must never be implied. A prompt that cannot
// be shown is not consent, so a non-TTY invocation without --yes refuses.
func TestTopicRemove_NonTTYWithoutYesRefuses(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	if err := topic.Open(env.RootDir).Attach("2072958", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := runTopic(t, "remove", "2072958", "--with-worktrees", "--output", "json")
	if err == nil {
		t.Fatal("a non-TTY removal without --yes must refuse")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeNeedsInput)
	}
	missing, ok := classified.Details["missing"].([]string)
	if !ok || len(missing) != 1 || missing[0] != "--yes" {
		t.Errorf("details.missing = %#v, want [--yes]", classified.Details["missing"])
	}

	if !env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Error("nothing may be removed when confirmation was refused")
	}
}

func TestTopicRemove_DryRunChangesNothing(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	if err := topic.Open(env.RootDir).Attach("2072958", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := runTopic(t, "remove", "2072958", "--with-worktrees", "--dry-run", "--output", "json"); err != nil {
		t.Fatalf("dry run: %v", err)
	}

	if !env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Error("--dry-run removed a worktree")
	}
	got, ok, err := topic.Open(env.RootDir).Get("2072958")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || len(got.Members) != 1 {
		t.Error("--dry-run changed membership")
	}
}

// The topic disappears because its last member detached. That garbage collection is
// the only path that removes an identity, so there is no race between removing
// membership and removing the topic.
func TestTopicRemove_TopicVanishesWithItsLastMember(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	store := topic.Open(env.RootDir)
	for _, m := range []topic.Member{{Repo: "api", Branch: "stage"}, {Repo: "api", Branch: "feat/two"}} {
		if err := store.Attach("2072958", m); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := runTopic(t, "remove", "2072958", "--with-worktrees", "--yes", "--output", "json"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, ok, err := store.Get("2072958"); err != nil || ok {
		t.Errorf("the topic must be gone once its last member detached (ok=%v err=%v)", ok, err)
	}
}

// Without --with-worktrees the worktrees stay and only membership is dropped.
func TestTopicRemove_WithoutWorktreesKeepsThemOnDisk(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	if err := topic.Open(env.RootDir).Attach("2072958", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := runTopic(t, "remove", "2072958", "--yes", "--output", "json"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if !env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Error("the worktree must survive a membership-only removal")
	}
	if _, ok, _ := topic.Open(env.RootDir).Get("2072958"); ok {
		t.Error("the topic should be gone after its only member detached")
	}
}

// A member whose worktree is missing is detach-only, not an error. That is exactly
// the drift an interrupted removal leaves, and refusing here would make it
// unclearable through this command.
func TestTopicRemove_DanglingMemberIsDetachedNotFatal(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	store := topic.Open(env.RootDir)
	for _, m := range []topic.Member{{Repo: "api", Branch: "stage"}, {Repo: "api", Branch: "feat/vanished"}} {
		if err := store.Attach("2072958", m); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := runTopic(t, "remove", "2072958", "--with-worktrees", "--yes", "--output", "json"); err != nil {
		t.Fatalf("a member with no worktree must not fail the removal: %v", err)
	}
	if _, ok, _ := store.Get("2072958"); ok {
		t.Error("both members should have been detached")
	}
}

// detach resolves the handle against THIS topic's members only, so the error is "not
// a member" rather than a confusing failure after matching something unrelated.
func TestTopicDetach_RejectsANonMember(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	if err := topic.Open(env.RootDir).Attach("2072958", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := runTopic(t, "detach", "2072958", "backend/api-feat-two", "--output", "json")
	if err == nil {
		t.Fatal("detaching a non-member must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeWorktreeUnknown {
		t.Errorf("code = %q, want %q", code, output.CodeWorktreeUnknown)
	}

	// The real member is untouched.
	got, _, _ := topic.Open(env.RootDir).Get("2072958")
	if len(got.Members) != 1 {
		t.Errorf("membership changed: %+v", got.Members)
	}
}

func TestTopicDetach_DropsMembershipAndKeepsTheWorktree(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	if err := topic.Open(env.RootDir).Attach("2072958", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := runTopic(t, "detach", "2072958", "backend/api-stage", "--output", "json"); err != nil {
		t.Fatalf("detach: %v", err)
	}

	if !env.DirExists(env.GetWorktreePath("backend", "api-stage")) {
		t.Error("detach must not touch the worktree")
	}
	if _, ok, _ := topic.Open(env.RootDir).Get("2072958"); ok {
		t.Error("the topic should be collected once its last member left")
	}
}

// A worktree may belong to at most one topic; the second attach is a conflict naming
// both topics so a caller can decide.
func TestTopicAttach_SecondTopicIsAConflict(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	if err := topic.Open(env.RootDir).Attach("first", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := runTopic(t, "attach", "second", "backend/api-stage", "--output", "json")
	if err == nil {
		t.Fatal("a worktree must not belong to two topics")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodeTopicConflict {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeTopicConflict)
	}
	if classified.Details["existing_topic"] != "first" {
		t.Errorf("details.existing_topic = %v, want first", classified.Details["existing_topic"])
	}
}

// show joins recorded membership to disk, so drift is visible before a removal
// rather than discovered during one.
func TestTopicShow_ReportsAMissingWorktree(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)
	store := topic.Open(env.RootDir)
	for _, m := range []topic.Member{{Repo: "api", Branch: "stage"}, {Repo: "api", Branch: "feat/vanished"}} {
		if err := store.Attach("2072958", m); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"topic", "show", "2072958", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("show: %v", err)
	}

	var payload topicJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Dangling != 1 {
		t.Errorf("dangling = %d, want 1", payload.Dangling)
	}
	var present, missing int
	for _, member := range payload.Members {
		if member.Present {
			present++
		} else {
			missing++
		}
	}
	if present != 1 || missing != 1 {
		t.Errorf("present/missing = %d/%d, want 1/1", present, missing)
	}
}

// An empty workspace lists nothing rather than failing, and reading must not create
// state.
func TestTopicList_EmptyWorkspace(t *testing.T) {
	resetCommandState(t)
	env := topicEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"topic", "list", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	var payload topicListJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Total != 0 || len(payload.Topics) != 0 {
		t.Errorf("got %d topics, want none", payload.Total)
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, ".hydra", "state.yaml")); !os.IsNotExist(err) {
		t.Errorf("listing must not create state: %v", err)
	}
}
