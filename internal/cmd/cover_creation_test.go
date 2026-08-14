package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func runCmdJSON(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"--output", "json"}, args...))
	err := rootCmd.Execute()
	return stdout, err
}

func decodeDataField(t *testing.T, stdout *bytes.Buffer, dest any) {
	t.Helper()
	decodeJSONData(t, stdout, dest)
}

func assertNeedsInputMissing(t *testing.T, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected needs_input error")
	}
	got := output.Classify(err)
	if got.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want %q", got.Code, output.CodeNeedsInput)
	}
	var missing []string
	switch items := got.Details["missing"].(type) {
	case []string:
		missing = items
	case []any:
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				t.Fatalf("details.missing = %#v, want %v", got.Details["missing"], want)
			}
			missing = append(missing, s)
		}
	default:
		t.Fatalf("details.missing = %#v, want %v", got.Details["missing"], want)
	}
	if len(missing) != len(want) {
		t.Fatalf("details.missing = %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Fatalf("details.missing[%d] = %q, want %q", i, missing[i], want[i])
		}
	}
}

func TestCreateAddConvergenceReportsSkipped(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	if _, err := runCmdJSON(t, "add", "api", "stage"); err != nil {
		t.Fatalf("first add: %v", err)
	}

	stdout, err := runCmdJSON(t, "add", "api", "stage")
	if err != nil {
		t.Fatalf("second add must exit 0, got: %v", err)
	}
	var payload addJSON
	decodeDataField(t, stdout, &payload)
	if payload.Disposition != "skipped" {
		t.Fatalf("disposition = %q, want skipped", payload.Disposition)
	}
}

func TestCreateAddAsNamingAndConflict(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage", "other")
	env.Chdir()

	if _, err := runCmdJSON(t, "add", "api", "stage", "--as", "short"); err != nil {
		t.Fatalf("add with --as: %v", err)
	}
	if !env.DirExists(env.GetWorktreePath("backend", "short")) {
		t.Fatal("worktree must live at backend/short when --as short is used")
	}

	err := runAddErr(t, "add", "api", "other", "--as", "short")
	if err == nil {
		t.Fatal("second branch claiming the same directory must be refused")
	}
	got := output.Classify(err)
	if got.Code != output.CodeWorktreeNameConflict {
		t.Fatalf("code = %q, want %q", got.Code, output.CodeWorktreeNameConflict)
	}
}

func runAddErr(t *testing.T, args ...string) error {
	t.Helper()
	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs(append([]string{"--output", "json"}, args...))
	return rootCmd.Execute()
}

func TestCreateAddMissingArguments(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	_, err := runCmdJSON(t, "add")
	assertNeedsInputMissing(t, err, "<alias>", "<branch>")

	_, err = runCmdJSON(t, "add", "api")
	assertNeedsInputMissing(t, err, "<branch>")
}

func TestCreateInitHonoursProjectNameAndManifest(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	ws := filepath.Join(env.RootDir, "workspace")
	if err := os.MkdirAll(ws, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := runCmdJSON(t, "init", "--project-name", "arvia", "--path", ws); err != nil {
		t.Fatalf("init: %v", err)
	}

	configPath := config.ManifestPath(ws)
	if !env.FileExists(configPath) {
		t.Fatalf("manifest not written at %s", configPath)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if cfg.Project != "arvia" {
		t.Fatalf("project = %q, want arvia", cfg.Project)
	}
}

func TestCreateInitTwiceReportsProjectExists(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.Chdir()

	if _, err := runCmdJSON(t, "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}

	resetCommandState(t)
	env.Chdir()
	resetCommandIO()
	rootCmd.SetArgs([]string{"init", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("second init must refuse an existing workspace")
	}
	got := output.Classify(err)
	if got.Code != output.CodeProjectExists {
		t.Fatalf("code = %q, want %q (must not be internal)", got.Code, output.CodeProjectExists)
	}
}

func TestCreateNewLocalAndRemote(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		resetCommandState(t)
		env := testutil.NewTestEnv(t)
		env.Chdir()

		if _, err := runCmdJSON(t, "new",
			"--project-path", "client/local-api",
			"--group", "backend",
			"--alias", "api",
			"--branch", "main",
			"--local",
		); err != nil {
			t.Fatalf("new --local: %v", err)
		}
		projectRoot := filepath.Join(env.RootDir, "client", "local-api")
		if !env.FileExists(config.ManifestPath(projectRoot)) {
			t.Fatal("local new must write a manifest")
		}
		if !env.DirExists(filepath.Join(projectRoot, "backend", "api")) {
			t.Fatal("local new must bootstrap the default-branch worktree")
		}
	})

	t.Run("remote", func(t *testing.T) {
		resetCommandState(t)
		env := testutil.NewTestEnv(t)
		remote := env.CreateRemoteRepo("new-origin", "main")
		env.Chdir()

		if _, err := runCmdJSON(t, "new",
			"--project-path", "client/remote-api",
			"--group", "backend",
			"--alias", "api",
			"--branch", "main",
			"--remote-url", remote,
		); err != nil {
			t.Fatalf("new --remote-url: %v", err)
		}
		projectRoot := filepath.Join(env.RootDir, "client", "remote-api")
		if !env.FileExists(config.ManifestPath(projectRoot)) {
			t.Fatal("remote new must write a manifest")
		}
		if !env.DirExists(filepath.Join(projectRoot, ".bare", "api.git")) {
			t.Fatal("remote new must clone into the bare repository")
		}
	})
}

func TestCreateNewInvalidNamesRefusedAsUsage(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.Chdir()

	_, err := runCmdJSON(t, "new",
		"--project-path", "client/bad",
		"--group", "../escape",
		"--alias", "api",
		"--branch", "main",
		"--local",
	)
	if err == nil {
		t.Fatal("invalid group must be refused")
	}
	if got := output.Classify(err).Code; got != output.CodeUsage {
		t.Fatalf("code = %q, want %q", got, output.CodeUsage)
	}

	_, err = runCmdJSON(t, "new",
		"--project-path", "client/bad2",
		"--group", "backend",
		"--alias", "bad/name",
		"--branch", "main",
		"--local",
	)
	if err == nil {
		t.Fatal("invalid alias must be refused")
	}
	if got := output.Classify(err).Code; got != output.CodeUsage {
		t.Fatalf("code = %q, want %q", got, output.CodeUsage)
	}
}

func TestCreateInitShellFlagsAndPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("conflicting completion flags", func(t *testing.T) {
		_, err := runCmdJSON(t, "init-shell", "bash", "--print", "--with-completion", "--without-completion")
		if err == nil {
			t.Fatal("conflicting flags must be usage")
		}
		if got := output.Classify(err).Code; got != output.CodeUsage {
			t.Fatalf("code = %q, want %q", got, output.CodeUsage)
		}
	})

	t.Run("unsupported shell", func(t *testing.T) {
		_, err := runCmdJSON(t, "init-shell", "powershell", "--print")
		if err == nil {
			t.Fatal("unsupported shell must be usage")
		}
		if got := output.Classify(err).Code; got != output.CodeUsage {
			t.Fatalf("code = %q, want %q", got, output.CodeUsage)
		}
	})

	t.Run("helper file location", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/bash")
		_, err := runCmdJSON(t, "init-shell", "bash", "--print", "--with-completion")
		if err != nil {
			t.Fatalf("init-shell: %v", err)
		}
		helperPath := filepath.Join(home, ".config", "hydra", "shell", "hydra-shell.bash")
		if _, statErr := os.Stat(helperPath); statErr != nil {
			t.Fatalf("helper file not written at %s: %v", helperPath, statErr)
		}
	})
}

func TestCreateCompletionScripts(t *testing.T) {
	for _, tc := range []struct {
		shell  string
		needle string
	}{
		{shell: "bash", needle: "complete"},
		{shell: "zsh", needle: "compdef"},
		{shell: "fish", needle: "complete -c hydra"},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			resetCommandState(t)
			stdout, _ := resetCommandIO()
			rootCmd.SetArgs([]string{"completion", tc.shell})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("completion %s: %v", tc.shell, err)
			}
			out := stdout.String()
			if strings.TrimSpace(out) == "" {
				t.Fatalf("completion %s emitted an empty script", tc.shell)
			}
			if !strings.Contains(out, tc.needle) {
				t.Fatalf("completion %s output missing %q", tc.shell, tc.needle)
			}
		})
	}

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"completion", "powershell"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("unsupported completion shell must be usage")
	}
	if got := output.Classify(err).Code; got != output.CodeUsage {
		t.Fatalf("code = %q, want %q", got, output.CodeUsage)
	}
}

func TestCreateAdoptCheckout(t *testing.T) {
	t.Run("refuses non-git path", func(t *testing.T) {
		resetCommandState(t)
		env := testutil.NewTestEnv(t)
		env.InitConfig()
		notGit := filepath.Join(env.RootDir, "imports", "plain-dir")
		if err := os.MkdirAll(notGit, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		env.Chdir()

		_, err := runCmdJSON(t, "repo", "add", notGit, "--adopt", "--group", "backend")
		if err == nil {
			t.Fatal("non-git checkout must be refused")
		}
		if got := output.Classify(err).Code; got != output.CodeUsage {
			t.Fatalf("code = %q, want %q", got, output.CodeUsage)
		}
	})

	t.Run("registers existing checkout without re-cloning", func(t *testing.T) {
		resetCommandState(t)
		env := testutil.NewTestEnv(t)
		env.InitConfig()
		remote := env.CreateRemoteRepo("upstream", "main")
		checkout := filepath.Join(env.RootDir, "imports", "api-checkout")
		if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		clone := exec.Command("git", "clone", remote, checkout) //nolint:gosec // G204: test fixture
		if out, err := clone.CombinedOutput(); err != nil {
			t.Fatalf("clone checkout: %v\n%s", err, out)
		}
		env.Chdir()

		stdout, err := runCmdJSON(t, "repo", "add", checkout, "--adopt", "--group", "backend", "--as", "api")
		if err != nil {
			t.Fatalf("adopt: %v", err)
		}
		report := parseAdoptReport(t, stdout.Bytes())
		if report.Repo != "api" || report.Group != "backend" {
			t.Fatalf("unexpected adopt metadata: %+v", report)
		}
		barePath := env.GetBarePath("api")
		if !env.FileExists(barePath) {
			t.Fatalf("bare repo not created at %s", barePath)
		}
		cfg := env.LoadConfig()
		if _, ok := cfg.Groups["backend"].Repos["api"]; !ok {
			t.Fatal("alias not registered under group")
		}
		if len(report.Worktrees) == 0 {
			t.Fatal("expected at least one adopted worktree")
		}
		if _, statErr := os.Stat(checkout); statErr != nil {
			t.Fatalf("original checkout removed or missing: %v", statErr)
		}
	})
}

func TestCreateApplyUnknownRepoItemErrorIsDiagnostic(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)

	payload, err := applyWith(t,
		`[{"repo":"api","branch":"feat/ok"},{"repo":"nope","branch":"feat/bad"}]`)
	if err == nil {
		t.Fatal("unknown repo in one item must fail the batch")
	}
	if code := output.Classify(err).Code; code != output.CodePartialFailure {
		t.Fatalf("code = %q, want %q", code, output.CodePartialFailure)
	}
	if payload.Created != 1 || payload.Failed != 1 {
		t.Fatalf("created/failed = %d/%d, want 1/1", payload.Created, payload.Failed)
	}
	if len(payload.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(payload.Results))
	}
	failed := payload.Results[1]
	if failed.Disposition != "failed" {
		t.Fatalf("failed item disposition = %q, want failed", failed.Disposition)
	}
	if failed.Error == nil {
		t.Fatal("failed item error must be an object, not a string")
	}
	if failed.Error.Code != output.CodeRepoUnknown {
		t.Fatalf("failed item error.code = %q, want %q", failed.Error.Code, output.CodeRepoUnknown)
	}
}

func TestCreateStartConvergenceReportsSkipped(t *testing.T) {
	resetCommandState(t)
	startEnv(t)
	args := []string{"marcus/feat-login", "--repos", "api,web", "--output", "json"}

	if err := runStartCmd(t, args...); err != nil {
		t.Fatalf("first start: %v", err)
	}
	payload := startPayload(t, args...)
	if len(payload.Skipped) != 2 {
		t.Fatalf("skipped = %d, want 2 on a converged run", len(payload.Skipped))
	}
	for _, entry := range payload.Skipped {
		if entry.Disposition != "skipped" {
			t.Fatalf("disposition = %q, want skipped for %+v", entry.Disposition, entry)
		}
	}
}

func TestCreateApplyConvergenceReportsSkipped(t *testing.T) {
	resetCommandState(t)
	applyEnv(t)
	doc := `[{"repo":"api","branch":"feat/converge"}]`

	if _, err := applyWith(t, doc); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	payload, err := applyWith(t, doc)
	if err != nil {
		t.Fatalf("second apply must exit 0, got: %v", err)
	}
	if payload.Skipped != 1 || payload.Created != 0 {
		t.Fatalf("payload = %+v, want 1 skipped and 0 created", payload)
	}
	if len(payload.Results) != 1 || payload.Results[0].Disposition != "skipped" {
		t.Fatalf("results = %+v, want one skipped item", payload.Results)
	}
}

func TestCreateRepoAddSecondRunConverges(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")
	env.Chdir()

	repoArgs := []string{"repo", "add", remote, "--as", "api", "--group", "backend", "--branches", "main,stage"}

	if _, err := runCmdJSON(t, repoArgs...); err != nil {
		t.Fatalf("first repo add: %v", err)
	}
	firstMain := env.GetWorktreePath("backend", "api")
	if !env.DirExists(firstMain) {
		t.Fatalf("precondition: %s must exist after the first clone", firstMain)
	}

	stdout, err := runCmdJSON(t, repoArgs...)
	if err != nil {
		t.Fatalf("second repo add must exit 0, got: %v", err)
	}
	if !env.DirExists(firstMain) {
		t.Fatal("second repo add must not destroy existing worktrees")
	}

	var payload struct {
		Worktrees []worktreeJSON `json:"worktrees"`
	}
	decodeDataField(t, stdout, &payload)
	if len(payload.Worktrees) != 2 {
		t.Fatalf("converged repo add reported %d worktrees, want 2", len(payload.Worktrees))
	}
}
