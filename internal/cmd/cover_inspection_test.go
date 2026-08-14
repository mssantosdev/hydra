package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/skill"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func inspectEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	resetCommandState(t)
	resetSelectorState()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	return env
}

func inspectTwoRepoEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	resetCommandState(t)
	resetSelectorState()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.SetupRepo("frontend", "web", "main")
	env.Chdir()
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	return env
}

func syncInspectGlobals(env *testutil.TestEnv) {
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
}

func resetSelectorState() {
	topicFilter = ""
	reposFilter = nil
	groupFilter = ""
	stateFilter = nil
	againstRef = ""
}

func runInspectJSON(t *testing.T, args ...string) (map[string]any, *bytes.Buffer) {
	t.Helper()
	resetSelectorState()
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"--output", "json"}, args...))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	return envelope, stdout
}

func runInspectExpectCode(t *testing.T, want string, args ...string) {
	t.Helper()
	resetSelectorState()
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"--output", "json"}, args...))
	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("%v: expected error %s", args, want)
	}
	classified := output.Classify(err)
	if classified.Code != want {
		t.Fatalf("%v: code = %q, want %q", args, classified.Code, want)
	}
	_ = stdout
}

func runInspectExpectExit(t *testing.T, wantExit int, args ...string) {
	t.Helper()
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"--output", "json"}, args...))
	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("%v: expected failure", args)
	}
	if got := output.ExitFor(output.Classify(err).Code); got != wantExit {
		t.Fatalf("%v: exit = %d, want %d (code %s)", args, got, wantExit, output.Classify(err).Code)
	}
	_ = stdout
}

func inspectDoctor(t *testing.T, args ...string) doctorReport {
	t.Helper()
	resetDoctorFlags()
	buf, restore := withDoctorJSON(t)
	defer restore()
	rootCmd.SetArgs(append([]string{"doctor"}, args...))
	_ = rootCmd.Execute()
	return parseDoctorReport(t, buf.Bytes())
}

func inspectDoctorChecks(t *testing.T, id string) []doctorCheck {
	t.Helper()
	return checksByID(inspectDoctor(t))[id]
}

func requireDoctorCheck(t *testing.T, id, wantStatus string) doctorCheck {
	t.Helper()
	items := inspectDoctorChecks(t, id)
	if len(items) == 0 {
		t.Fatalf("expected check %s", id)
	}
	for _, item := range items {
		if item.Status == wantStatus {
			return item
		}
	}
	t.Fatalf("expected %s %s, got %+v", id, wantStatus, items)
	return doctorCheck{}
}

func runDoctorFix(t *testing.T) doctorReport {
	t.Helper()
	resetDoctorFlags()
	doctorFix = true
	buf, restore := withDoctorJSON(t)
	defer restore()
	rootCmd.SetArgs([]string{"doctor", "--fix"})
	_ = rootCmd.Execute()
	return parseDoctorReport(t, buf.Bytes())
}

func warningCodesFromEnvelope(envelope map[string]any) []string {
	raw, ok := envelope["warnings"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			if code, _ := m["code"].(string); code != "" {
				out = append(out, code)
			}
		}
	}
	return out
}

func TestInspectDoctor_HealthyChecksAreOK(t *testing.T) {
	inspectEnv(t)
	for _, id := range []string{
		checkWorktreeMissingOnDisk,
		checkWorktreeOrphanedDir,
		checkWorktreeInsideGitdir,
		checkWorktreeDetached,
		checkWorktreeDirty,
		checkRegistryDangling,
	} {
		for _, item := range inspectDoctorChecks(t, id) {
			if item.Status != "ok" && item.Status != "warn" {
				t.Fatalf("%s: want ok/warn, got %+v", id, item)
			}
		}
	}
}

func TestInspectDoctor_WorktreeMissingOnDiskFixable(t *testing.T) {
	env := inspectEnv(t)
	wtPath := env.GetWorktreePath("backend", "api")
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("remove worktree dir: %v", err)
	}

	check := requireDoctorCheck(t, checkWorktreeMissingOnDisk, "fail")
	if !check.Fixable {
		t.Fatal("worktree_missing_on_disk must be fixable")
	}

	report := runDoctorFix(t)
	items := checksByID(report)[checkWorktreeMissingOnDisk]
	if len(items) == 0 || items[0].Status != "ok" || !items[0].Fixed {
		t.Fatalf("expected fixed missing_on_disk, got %+v", items)
	}
	worktrees, err := git.ListWorktrees(env.GetBarePath("api"))
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	for _, wt := range worktrees {
		if wt.Path == wtPath {
			t.Fatalf("pruned worktree still registered: %+v", wt)
		}
	}
}

func TestInspectDoctor_WorktreeOrphanedDirNotFixable(t *testing.T) {
	env := inspectEnv(t)
	wtPath := env.GetWorktreePath("backend", "api")
	if err := os.RemoveAll(wtPath); err != nil {
		t.Fatalf("remove worktree dir: %v", err)
	}
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("recreate empty dir: %v", err)
	}

	check := requireDoctorCheck(t, checkWorktreeOrphanedDir, "fail")
	if check.Fixable {
		t.Fatal("worktree_orphaned_dir must not be fixable")
	}
}

func TestInspectDoctor_BareUnregisteredNotFixable(t *testing.T) {
	env := inspectEnv(t)
	env.CreateBareRepo("orphan", "main")

	for _, item := range inspectDoctorChecks(t, checkBareUnregistered) {
		if item.Status != "fail" {
			t.Fatalf("expected fail, got %+v", item)
		}
		if item.Fixable {
			t.Fatal("bare_unregistered must not be fixable")
		}
	}
}

func TestInspectDoctor_RegistryDanglingFixable(t *testing.T) {
	env := inspectEnv(t)
	reg, err := registry.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	if err := reg.Add(filepath.Base(env.RootDir), env.RootDir); err != nil {
		t.Fatalf("registry add live: %v", err)
	}
	reg.Projects["ghost-project"] = filepath.Join(env.RootDir, "missing-root")
	if err := reg.Save(); err != nil {
		t.Fatalf("registry save: %v", err)
	}

	check := requireDoctorCheck(t, checkRegistryDangling, "fail")
	if !check.Fixable {
		t.Fatal("registry_dangling must be fixable")
	}

	report := runDoctorFix(t)
	items := checksByID(report)[checkRegistryDangling]
	if len(items) == 0 || items[0].Status != "ok" || !items[0].Fixed {
		t.Fatalf("expected registry fix, got %+v", items)
	}
	reg, err = registry.Load()
	if err != nil {
		t.Fatalf("registry reload: %v", err)
	}
	if _, ok := reg.Projects["ghost-project"]; ok {
		t.Fatal("dangling registry entry was not removed")
	}
}

func TestInspectDoctor_LegacySymlinkFixable(t *testing.T) {
	env := inspectEnv(t)
	barePath := env.GetBarePath("api")
	linkPath := env.GetWorktreePath("backend", "legacy-link")
	if err := os.Symlink(filepath.Join(barePath, "HEAD"), linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	check := requireDoctorCheck(t, checkLegacySymlink, "fail")
	if !check.Fixable {
		t.Fatal("legacy_symlink must be fixable")
	}

	report := runDoctorFix(t)
	items := checksByID(report)[checkLegacySymlink]
	if len(items) == 0 || items[0].Status != "ok" || !items[0].Fixed {
		t.Fatalf("expected legacy symlink fix, got %+v", items)
	}
	if env.FileExists(linkPath) {
		t.Fatal("legacy symlink still on disk")
	}
}

func TestInspectDoctor_WorktreeInsideGitdirNotFixable(t *testing.T) {
	env := inspectEnv(t)
	barePath := env.GetBarePath("api")
	inside := filepath.Join(barePath, "nested-wt")
	if err := git.AddWorktreeNewBranch(barePath, inside, "nested-branch", "main"); err != nil {
		t.Fatalf("add inside bare: %v", err)
	}

	check := requireDoctorCheck(t, checkWorktreeInsideGitdir, "fail")
	if check.Fixable {
		t.Fatal("worktree_inside_gitdir must not be fixable")
	}
}

func TestInspectDoctor_WorktreeDirtyWarnNotFixable(t *testing.T) {
	env := inspectEnv(t)
	env.MakeWorktreeDirty(env.GetWorktreePath("backend", "api"))

	check := requireDoctorCheck(t, checkWorktreeDirty, "warn")
	if check.Fixable {
		t.Fatal("worktree_dirty must not be fixable")
	}
}

func TestInspectDoctor_WorktreeDetachedWarnNotFixable(t *testing.T) {
	env := inspectEnv(t)
	mainPath := env.GetWorktreePath("backend", "api")
	cmd := exec.Command("git", "-C", mainPath, "checkout", "--detach")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detach: %v\n%s", err, out)
	}

	check := requireDoctorCheck(t, checkWorktreeDetached, "warn")
	if check.Fixable {
		t.Fatal("worktree_detached must not be fixable")
	}
}

func TestInspectDoctor_TextOutput(t *testing.T) {
	env := inspectEnv(t)
	barePath := env.GetBarePath("api")
	linkPath := env.GetWorktreePath("backend", "legacy-text")
	_ = os.Symlink(filepath.Join(barePath, "HEAD"), linkPath)

	resetDoctorFlags()
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"doctor"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected partial failure")
	}
	if !strings.Contains(stdout.String(), "legacy_symlink") {
		t.Fatalf("text doctor must name the check, got:\n%s", stdout.String())
	}
}

func TestInspectDoctor_PartialFailureCode(t *testing.T) {
	env := inspectEnv(t)
	barePath := env.GetBarePath("api")
	linkPath := env.GetWorktreePath("backend", "legacy-partial")
	_ = os.Symlink(filepath.Join(barePath, "HEAD"), linkPath)

	resetDoctorFlags()
	buf, restore := withDoctorJSON(t)
	defer restore()
	rootCmd.SetArgs([]string{"doctor"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected partial_failure")
	}
	if code := output.Classify(err).Code; code != output.CodePartialFailure {
		t.Fatalf("code = %q, want %q", code, output.CodePartialFailure)
	}
	_ = buf
}

func TestInspectList_SelectorsIntersect(t *testing.T) {
	env := inspectTwoRepoEnv(t)
	env.MakeWorktreeDirty(env.GetWorktreePath("backend", "api"))

	envelope, _ := runInspectJSON(t, "list", "--repos", "api", "--group", "backend", "--filter", "dirty")
	worktrees := worktreesFromEnvelope(t, envelope)
	if len(worktrees) != 1 {
		t.Fatalf("expected 1 dirty api worktree, got %d", len(worktrees))
	}
	if name, _ := worktrees[0]["name"].(string); name != "api" {
		t.Fatalf("name = %q, want api", name)
	}
}

func TestInspectList_TopicSelector(t *testing.T) {
	env := inspectEnv(t)
	resetCommandState(t)
	resetSelectorState()
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	rootCmd.SetArgs([]string{"start", "stage", "--repos", "api", "--topic", "7001"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("start topic: %v", err)
	}

	envelope, _ := runInspectJSON(t, "list", "--topic", "7001")
	worktrees := worktreesFromEnvelope(t, envelope)
	if len(worktrees) != 1 {
		t.Fatalf("expected one topic worktree, got %d", len(worktrees))
	}
}

func TestInspectList_AllRegisteredProjects(t *testing.T) {
	env := inspectTwoRepoEnv(t)
	reg, err := registry.Load()
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	name := filepath.Base(env.RootDir)
	if err := reg.Add(name, env.RootDir); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	if err := reg.Save(); err != nil {
		t.Fatalf("registry save: %v", err)
	}

	envelope, _ := runInspectJSON(t, "list", "--all")
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data: %#v", envelope["data"])
	}
	projects, ok := data["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("projects: %#v", data["projects"])
	}
	payload := projects[0].(map[string]any)
	if total, _ := payload["total"].(float64); int(total) != 2 {
		t.Fatalf("expected two worktrees in registered project, got %v", total)
	}
}

func TestInspectList_ContradictorySelectorsNoteNotFailure(t *testing.T) {
	inspectTwoRepoEnv(t)
	envelope, _ := runInspectJSON(t, "list", "--repos", "api", "--group", "frontend")
	if len(worktreesFromEnvelope(t, envelope)) != 0 {
		t.Fatal("contradictory selectors must match nothing")
	}
	warnings := warningsFromEnvelope(t, envelope)
	if len(warnings) == 0 {
		t.Fatal("expected advisory note for empty selection")
	}
}

func TestInspectList_BadFilterIsUsage(t *testing.T) {
	inspectEnv(t)
	runInspectExpectCode(t, output.CodeUsage, "list", "--filter", "not-a-filter")
}

func TestInspectStatus_BadFilterIsUsage(t *testing.T) {
	inspectEnv(t)
	runInspectExpectCode(t, output.CodeUsage, "status", "--filter", "not-a-filter")
}

func TestInspectList_UnknownRefStillLists(t *testing.T) {
	resetCommandState(t)
	resetSelectorState()
	env := againstEnv(t)
	syncInspectGlobals(env)

	envelope, _ := runInspectJSON(t, "list", "--against", "no-such-ref")
	if len(worktreesFromEnvelope(t, envelope)) == 0 {
		t.Fatal("unknown ref must not prevent listing")
	}
	if len(warningCodesFromEnvelope(envelope)) == 0 {
		t.Fatal("unknown ref must surface as a warning note")
	}
}

func TestInspectList_AgainstResolvableRef(t *testing.T) {
	resetCommandState(t)
	resetSelectorState()
	env := againstEnv(t)
	syncInspectGlobals(env)

	envelope, _ := runInspectJSON(t, "list", "--against", "release")
	for _, wt := range worktreesFromEnvelope(t, envelope) {
		against, ok := wt["against"].(map[string]any)
		if !ok {
			t.Fatalf("%v missing against block", wt["name"])
		}
		if _, ok := against["merged"].(bool); !ok {
			t.Fatalf("against must include merged, got %#v", against)
		}
	}
}

func TestInspectList_TextOutput(t *testing.T) {
	inspectEnv(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"list"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list text: %v", err)
	}
	if !strings.Contains(stdout.String(), "api") {
		t.Fatalf("list text must name worktrees, got:\n%s", stdout.String())
	}
}

func TestInspectStatus_TextOutput(t *testing.T) {
	inspectEnv(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"status"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("status text: %v", err)
	}
	if !strings.Contains(stdout.String(), "api") {
		t.Fatalf("status text must summarize worktrees, got:\n%s", stdout.String())
	}
}

func TestInspectPath_UnknownHandle(t *testing.T) {
	inspectEnv(t)
	runInspectExpectCode(t, output.CodeWorktreeUnknown, "path", "nope")
}

func TestInspectPath_AmbiguousHandle(t *testing.T) {
	inspectTwoRepoEnv(t)
	runInspectExpectCode(t, output.CodeWorktreeNameConflict, "path", "main")
}

func TestInspectPath_HandleAndTopicIsUsage(t *testing.T) {
	inspectEnv(t)
	runInspectExpectCode(t, output.CodeUsage, "path", "api", "--topic", "7001")
}

func TestInspectSwitch_UnknownHandle(t *testing.T) {
	inspectEnv(t)
	runInspectExpectCode(t, output.CodeWorktreeUnknown, "switch", "nope")
}

func TestInspectSwitch_AmbiguousHandle(t *testing.T) {
	inspectTwoRepoEnv(t)
	runInspectExpectCode(t, output.CodeWorktreeNameConflict, "switch", "main")
}

func TestInspectSwitch_CDWithoutHelper(t *testing.T) {
	t.Setenv("HYDRA_SHELL_HELPER", "")
	t.Setenv("HYDRA_SWITCH_OUTPUT_FILE", "")
	inspectEnv(t)
	runInspectExpectExit(t, 3, "switch", "--cd", "api")
}

func TestInspectWhere_OutsideWorkspaceReportsResolution(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	envelope, _ := runInspectJSON(t, "where")
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data: %#v", envelope["data"])
	}
	if inProject, _ := data["in_project"].(bool); inProject {
		t.Fatal("in_project must be false outside a workspace")
	}
	if cwd, _ := data["cwd"].(string); cwd == "" {
		t.Fatal("cwd must be reported")
	}
}

func TestInspectConfigShowOutsideWorkspaceSucceedsWithNote(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	envelope, _ := runInspectJSON(t, "config", "show")
	codes := warningCodesFromEnvelope(envelope)
	found := false
	for _, code := range codes {
		if code == output.CodeNotInProject {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected not_in_project note, warnings=%v", codes)
	}
}

func TestInspectConfigSetUnknownThemeIsUsage(t *testing.T) {
	resetCommandState(t)
	t.Setenv("HYDRA_CONFIG_DIR", t.TempDir())
	before, err := global.Load()
	if err != nil {
		t.Fatalf("load before: %v", err)
	}
	runInspectExpectCode(t, output.CodeUsage, "config", "set", "theme", "no-such-theme")
	loaded, err := global.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Theme.Name != before.Theme.Name {
		t.Fatalf("theme must not be stored on usage, got %q want %q", loaded.Theme.Name, before.Theme.Name)
	}
}

func TestInspectConfigSetUnknownKeyIsUsage(t *testing.T) {
	resetCommandState(t)
	t.Setenv("HYDRA_CONFIG_DIR", t.TempDir())
	runInspectExpectCode(t, output.CodeUsage, "config", "set", "not-a-key", "value")
}

func TestInspectCommands_PublishesSurfaceAndExitTable(t *testing.T) {
	resetCommandState(t)
	envelope, _ := runInspectJSON(t, "commands")
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data: %#v", envelope["data"])
	}
	cmds, ok := data["commands"].([]any)
	if !ok {
		t.Fatalf("commands: %#v", data["commands"])
	}
	surface := describeSurface()
	if len(cmds) != len(surface.Commands) {
		t.Fatalf("commands count = %d, want %d", len(cmds), len(surface.Commands))
	}
	codes, ok := data["error_codes"].([]any)
	if !ok {
		t.Fatalf("error_codes: %#v", data["error_codes"])
	}
	if len(codes) != len(output.Codes()) {
		t.Fatalf("error_codes count = %d, want %d", len(codes), len(output.Codes()))
	}
}

func TestInspectHooksLs_JSONListsNamesAndLevels(t *testing.T) {
	env := inspectEnv(t)
	remote := env.LoadConfig().Groups["backend"].Repos["api"].Remote
	writeHooksConfig(t, env, remote, config.Hooks{
		PostAdd: []config.Hook{{Name: "install deps", Run: "true"}},
	})
	syncInspectGlobals(env)

	_, stdout := runInspectJSON(t, "hooks", "ls")
	var payload hooksLsPayload
	decodeJSONData(t, stdout, &payload)

	found := false
	for _, event := range payload.Events {
		for _, hook := range event.Hooks {
			if hook.Name == "install deps" && hook.Path != "" {
				found = true
			}
			if hook.Name != "" && event.Workspace+event.Groups+event.Repos == 0 && len(event.Hooks) > 0 {
				t.Fatalf("event %s must report level counts", event.Event)
			}
		}
	}
	if !found {
		t.Fatal("expected hook name and manifest path in hooks ls JSON")
	}
}

func TestInspectSkill_EmitsEmbeddedContract(t *testing.T) {
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"skill"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("skill: %v", err)
	}
	if stdout.String() != skill.Content() {
		t.Fatal("skill stdout must match embedded contract")
	}
}

func TestInspectRepoList_RegisteredRepositories(t *testing.T) {
	inspectEnv(t)
	_, stdout := runInspectJSON(t, "repo", "list")
	var payload repoListJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Total != 1 || len(payload.Repos) != 1 {
		t.Fatalf("repos = %+v, want one registered repo", payload)
	}
	if payload.Repos[0].Alias != "api" {
		t.Fatalf("alias = %q, want api", payload.Repos[0].Alias)
	}
}

func TestInspectRepoBranches_LocalRemote(t *testing.T) {
	env := inspectEnv(t)
	remote := env.LoadConfig().Groups["backend"].Repos["api"].Remote

	_, stdout := runInspectJSON(t, "repo", "branches", "api")
	var payload repoBranchesJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Remote != remote {
		t.Fatalf("remote = %q, want %q", payload.Remote, remote)
	}
	if len(payload.Branches) == 0 {
		t.Fatal("expected at least one branch")
	}
}

func TestInspectGlossary_TerminologyAvailable(t *testing.T) {
	hasGlossary := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "glossary" {
			hasGlossary = true
			break
		}
	}
	if hasGlossary {
		envelope, _ := runInspectJSON(t, "glossary")
		data, ok := envelope["data"].(map[string]any)
		if !ok {
			t.Fatalf("glossary data: %#v", envelope["data"])
		}
		terms, ok := data["terms"].([]any)
		if !ok || len(terms) == 0 {
			t.Fatalf("glossary terms: %#v", data["terms"])
		}
		return
	}
	content := skill.Content()
	for _, term := range []string{"worktree", "topic", "upstream"} {
		if !strings.Contains(content, term) {
			t.Fatalf("skill contract missing term %q", term)
		}
	}
}

var _ = errors.New
