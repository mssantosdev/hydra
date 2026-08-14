package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/hooks"
	"github.com/mssantosdev/hydra/internal/log"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

// Build metadata, injected via -ldflags.
var (
	version = "dev"
	commit  = ""
	builtAt = ""
)

var (
	cfgFile     string
	outputFlag  string
	projectFlag string
	verboseFlag bool
	noHooksFlag bool

	cfg               *config.Config
	projectRoot       string
	projectConfigPath string
	outMode           output.Mode

	// commandsWithoutProject do not need a manifest to run: they either create
	// one, manage the global registry, or are pure output.
	commandsWithoutProject = map[string]bool{
		"init":       true,
		"new":        true,
		"help":       true,
		"init-shell": true,
		"completion": true,
		"skill":      true,
		"project":    true,
		// commands describes hydra itself, so requiring a workspace would make the
		// surface undiscoverable from anywhere a caller has not set one up yet.
		"commands": true,
	}

	rootCmd = &cobra.Command{
		Use:   "hydra",
		Short: "Hydra - Git worktree manager",
		Long: `Hydra is a CLI tool for managing Git worktrees.

It organizes work as project -> group -> repo -> worktree: a bare repository holds
git data only, and every worktree is a real sibling directory under its group.

Every command emits a machine-readable JSON envelope with --output json (and by
default whenever stdout is not a terminal), so scripts and agents never scrape text.

Guide: https://mssantosdev.github.io/hydra/`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			mode, err := output.Resolve(outputFlag)
			if err != nil {
				return err
			}
			outMode = mode
			styles.SetColorEnabled(output.Color(os.Stdout))
			log.SetVerbose(verboseFlag)

			if skipsProject(cmd) {
				return nil
			}
			// `where` and `config` REPORT project resolution, so they attempt the load and
			// tolerate absence. Listing them as project-less would skip loading entirely and
			// make them answer "no workspace" while standing in one — and would discard
			// --project and --config, the flags whose whole purpose is choosing the manifest
			// these two commands then describe.
			if reportsProjectResolution(cmd) {
				// The error is kept, not dropped: a manifest that exists and cannot be parsed is
				// exactly what someone runs these commands to find out about.
				projectLoadErr = loadProject()
				return nil
			}
			return loadProject()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
)

// annotationRegistryFanout marks a command whose --all flag means "every
// registered project" rather than "everything in this project". Only those
// commands may run without a workspace under --all; sniffing --all generically
// would wrongly exempt `sync --all`, which means every repo in THIS project.
const annotationRegistryFanout = "hydra/registry-fanout"

// projectLoadErr holds why the active project failed to resolve, for the commands that REPORT
// resolution rather than requiring it.
var projectLoadErr error

// errNotInProject is the single not_in_project error.
//
// "no hydra project loaded" on its own cannot be acted on: it does not say where the
// search started, so a caller cannot tell a wrong working directory from a workspace
// that was never created. It also used to advise `hydra init`, which REFUSES when a
// manifest already exists — advice pointing at a command that cannot work is worse
// than none, so the hint is `project ls`, which always answers.
func errNotInProject() *output.Error {
	err := output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	if wd, wdErr := os.Getwd(); wdErr == nil {
		err = err.WithDetail("searched_from", wd)
	}
	return err.
		WithDetail("looked_for", filepath.Join(config.StateDir, config.ManifestName)).
		WithNext(output.Next{
			Argv: []string{"hydra", "project", "ls", "--output", "json"},
			Why:  "list registered workspaces and their paths",
		})
}

// reportsProjectResolution names the commands that DESCRIBE the active project rather than
// requiring one: they attempt the load, tolerate absence, and must still honour --project and
// --config, which choose the very manifest they report.
//
// The parent chain is walked because a subcommand carries its own name — `config show` is "show" —
// so matching the leaf alone would send it down the requires-a-project path and make global
// configuration unreadable outside a workspace.
func reportsProjectResolution(cmd *cobra.Command) bool {
	for c := cmd; c != nil && c.Parent() != nil; c = c.Parent() {
		if c.Name() == "where" || c.Name() == "config" {
			return true
		}
	}
	return false
}

func skipsProject(cmd *cobra.Command) bool {
	if cmd.Parent() == nil {
		return true
	}
	if cmd.Annotations[annotationRegistryFanout] == "all" {
		if flag := cmd.Flags().Lookup("all"); flag != nil && flag.Changed {
			return true
		}
	}
	for c := cmd; c != nil && c.Parent() != nil; c = c.Parent() {
		if commandsWithoutProject[c.Name()] {
			return true
		}
	}
	return false
}

// loadProject resolves the active project from --project, --config, or by walking
// up from the working directory.
//
// The globals are cleared first so a failed resolution can never leave a previous
// invocation's workspace behind — which would silently operate on the wrong project.
func loadProject() error {
	cfg, projectRoot, projectConfigPath = nil, "", ""

	if projectFlag != "" {
		reg, err := registry.Load()
		if err != nil {
			return output.Wrap(output.CodeIOFailed, err, "failed to read the project registry")
		}
		root, ok := reg.Resolve(projectFlag)
		if !ok {
			return output.Errorf(output.CodeProjectUnknown,
				"project %q is not registered; run \"hydra project ls\" to see registered projects", projectFlag).
				WithDetail("project", projectFlag).
				WithDetail("registry", registry.Path())
		}
		return loadProjectAt(root)
	}

	if cfgFile != "" {
		loaded, err := config.Load(cfgFile)
		if err != nil {
			return classifyConfigError(err)
		}
		cfg = loaded
		projectConfigPath = cfgFile
		projectRoot = absDir(cfgFile)
		return nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to resolve the working directory")
	}

	path, loaded, err := config.FindConfig(wd)
	if err != nil {
		return classifyConfigError(err)
	}
	cfg = loaded
	projectConfigPath = path
	projectRoot = absDir(path)
	return nil
}

func loadProjectAt(root string) error {
	path := config.ManifestPath(root)
	loaded, err := config.Load(path)
	if err != nil {
		return classifyConfigError(err)
	}
	cfg = loaded
	projectConfigPath = path
	projectRoot = root
	return nil
}

func classifyConfigError(err error) error {
	var unsupported *config.ErrUnsupportedVersion
	if errors.As(err, &unsupported) {
		return output.Errorf(output.CodeConfigVersionUnsupported, "%s", unsupported.Error()).
			WithDetail("path", unsupported.Path).
			WithDetail("found_version", unsupported.Version).
			WithDetail("required_version", config.SchemaVersion)
	}
	// A manifest that exists and is refused is not "no workspace here", and telling the caller to
	// run `hydra init` would be advice that overwrites nothing and fixes nothing.
	var invalid *config.ErrConfigInvalid
	if errors.As(err, &invalid) {
		return output.Errorf(output.CodeConfigInvalid, "%s", invalid.Error()).
			WithDetail("field", invalid.Field)
	}
	var unsafe *config.ErrUnsafePath
	if errors.As(err, &unsafe) {
		return output.Errorf(output.CodeConfigInvalid, "%s", unsafe.Error()).
			WithDetail("field", unsafe.Field).
			WithDetail("value", unsafe.Value)
	}
	// A manifest that exists and cannot be parsed is not "no workspace here" either.
	// It used to land on not_in_project, whose advice is `hydra init` — a command that
	// refuses when a manifest exists, so the caller was sent somewhere that cannot help.
	var malformed *config.ErrMalformed
	if errors.As(err, &malformed) {
		invalid := output.Errorf(output.CodeConfigInvalid, "%s", malformed.Error()).
			WithDetail("path", malformed.Path)
		if line := malformed.Line(); line > 0 {
			invalid = invalid.WithDetail("line", line)
		}
		return invalid
	}
	return output.Errorf(output.CodeNotInProject,
		"%v", err).
		WithNext(output.Next{
			Argv: []string{"hydra", "init"},
			Why:  "create a workspace in the current directory",
		}, output.Next{
			Argv: []string{"hydra", "project", "ls", "--output", "json"},
			Why:  "list workspaces already registered, to pass --project instead",
		})
}

// absDir resolves the workspace root that owns a manifest. See config.ProjectRoot:
// the root is the parent of .hydra/, not the manifest's own parent.
func absDir(configPath string) string { return config.ProjectRoot(configPath) }

// Execute runs the root command, returning the command path that ran so the
// caller can label the error envelope.
// executedCmd is the command cobra resolved, kept so a usage error can name it and quote
// its own usage line rather than the root's.
var executedCmd *cobra.Command

func Execute() (string, error) {
	executed, err := rootCmd.ExecuteC()
	executedCmd = executed
	name := ""
	if executed != nil {
		name = commandName(executed)
	}
	err = classifyUnknownCommand(err)
	if emitTextFailure(err) {
		return name, nil
	}
	return name, err
}

// classifyUnknownCommand turns cobra's unknown-command error into something a caller can
// act on.
//
// Cobra reports a typo as `internal`, with its "Did you mean this?" suggestion buried in
// the prose of the message. That is the error a zero-context agent is most likely to hit
// first, and the recovery — hydra's own published surface — was undiscoverable from it:
// the suggestion could not be read without parsing English, and nothing pointed at
// `hydra commands`.
func classifyUnknownCommand(err error) error {
	if err == nil {
		return err
	}
	if !strings.HasPrefix(err.Error(), "unknown command") {
		return withUsageGuidance(err)
	}

	var names []string
	for _, c := range rootCmd.Commands() {
		if c.IsAvailableCommand() {
			names = append(names, c.Name())
		}
	}
	sort.Strings(names)

	wrapped := output.Errorf(output.CodeUnknownCommand, "%s", firstLine(err.Error())).
		WithDetail("available", names)
	if guesses := suggestionsIn(err.Error()); len(guesses) > 0 {
		wrapped = wrapped.WithDetail("did_you_mean", guesses)
	}
	return wrapped.WithNext(output.Next{
		Argv: []string{"hydra", "commands", "--output", "json"},
		Why:  "list every command and its flags, plus the error-code table",
	})
}

// firstLine keeps cobra's one-line summary and drops the multi-line suggestion prose,
// which is carried as structured detail instead.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// suggestionsIn extracts the names cobra listed under "Did you mean this?".
func suggestionsIn(msg string) []string {
	_, tail, ok := strings.Cut(msg, "Did you mean this?")
	if !ok {
		return nil
	}
	var out []string
	for _, line := range strings.Split(tail, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// ErrorsAsJSON reports whether a failure should be rendered as a JSON envelope.
func ErrorsAsJSON() bool {
	return jsonMode()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "path to a .hydra/config.yaml (default: nearest one walking up)")
	rootCmd.PersistentFlags().StringVar(&projectFlag, "project", "", "registered project name to operate on")
	rootCmd.PersistentFlags().StringVar(&outputFlag, "output", "", "output mode: auto|text|json (auto emits JSON when stdout is not a terminal)")
	rootCmd.PersistentFlags().BoolVar(&verboseFlag, "verbose", false, "verbose logging on stderr")
	rootCmd.PersistentFlags().BoolVar(&noHooksFlag, "no-hooks", false, "skip every configured hook")

	rootCmd.Version = versionInfo()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.SetHelpTemplate(`{{with .Long}}{{.}}{{else}}{{.Short}}{{end}}

Version: {{.Version}}

Usage:
  {{.UseLine}}

{{if .HasAvailableSubCommands}}Commands:
{{range .Commands}}{{if .IsAvailableCommand}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}
Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}`)
}

// GetConfig returns the loaded project configuration.
func GetConfig() *config.Config { return cfg }

// RootCommand exposes the command tree for drift tests.
func RootCommand() *cobra.Command { return rootCmd }

// commandName is the envelope's "command" field: the command path without the
// binary name, so nested commands stay unambiguous ("project ls"). An alias is
// reported as invoked, so `hydra ls` says "ls".
func commandName(cmd *cobra.Command) string {
	if called := cmd.CalledAs(); called != "" && cmd.Parent() != nil && cmd.Parent().Parent() == nil {
		return called
	}
	return strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), rootCmd.Name()))
}

// jsonMode reports whether the effective output mode for stdout is JSON.
func jsonMode() bool {
	return output.Effective(outMode, os.Stdout) == output.ModeJSON
}

// interactive reports whether prompts may be shown.
func interactive() bool {
	return output.Interactive(outMode)
}

// explicitJSON reports whether JSON was actually asked for, rather than inferred
// from stdout not being a terminal.
func explicitJSON() bool {
	return outMode == output.ModeJSON
}

// emitValue is the funnel for commands whose payload IS a single value the shell
// consumes (`hydra path`). Auto mode stays text so `cd "$(hydra path api)"` works;
// only an explicit --output json (or HYDRA_OUTPUT=json) produces an envelope.
func emitValue(cmd *cobra.Command, summary string, data any, warnings []*output.Diagnostic, text func()) error {
	if explicitJSON() {
		return output.EmitJSON(cmd.OutOrStdout(), commandName(cmd),
			output.Result{Summary: summary, Data: data, Warnings: warnings})
	}
	for _, warning := range warnings {
		_, _ = fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	if text != nil {
		text()
	}
	return nil
}

// emit is the single success funnel: JSON envelope, or the command's text renderer.
//
// summary is a required argument rather than an optional field, so the compiler —
// not a reviewer — makes every command state its one-line answer. That is the whole
// point of the field: a caller should never have to reconstruct "what happened" by
// walking data.
func emit(cmd *cobra.Command, summary string, data any, warnings []*output.Diagnostic, text func()) error {
	return emitResult(cmd, output.Result{Summary: summary, Data: data, Warnings: warnings}, text)
}

// envelopeEmitted records that a JSON envelope already reached stdout, so main does
// not append a second one for the same command.
var envelopeEmitted bool

// EnvelopeEmitted reports whether a command already wrote its envelope.
func EnvelopeEmitted() bool { return envelopeEmitted }

// emitResult is for commands that carry more than a summary — a partial outcome, or
// a next suggestion.
func emitResult(cmd *cobra.Command, result output.Result, text func()) error {
	if jsonMode() {
		envelopeEmitted = true
		return output.EmitJSON(cmd.OutOrStdout(), commandName(cmd), result)
	}
	for _, warning := range result.Warnings {
		_, _ = fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	if text != nil {
		text()
	}
	return nil
}

// projectTarget is one workspace a read-only command runs against.
type projectTarget struct {
	Name string
	Root string
	Cfg  *config.Config
}

// projectTargets resolves the workspaces a read-only command covers. With
// all=false it is the currently loaded project; with all=true it is every
// registered project, and unreadable entries become warnings instead of
// disappearing.
func projectTargets(all bool) ([]projectTarget, []*output.Diagnostic, error) {
	if !all {
		if cfg == nil {
			return nil, nil, output.Errorf(output.CodeNotInProject,
				"no hydra workspace found; run \"hydra init\" or pass --project <name>")
		}
		return []projectTarget{{Name: cfg.Project, Root: projectRoot, Cfg: cfg}}, nil, nil
	}

	reg, err := registry.Load()
	if err != nil {
		return nil, nil, output.Wrap(output.CodeIOFailed, err, "failed to read the project registry")
	}

	var targets []projectTarget
	var warnings []*output.Diagnostic
	for _, name := range reg.Names() {
		root, _ := reg.Resolve(name)
		loaded, loadErr := config.Load(config.ManifestPath(root))
		if loadErr != nil {
			warnings = append(warnings, output.Warnf(output.Classify(classifyConfigError(loadErr)).Code,
				"%s (%s): %v", name, root, loadErr).
				WithSubject("project", name))
			continue
		}
		targets = append(targets, projectTarget{Name: name, Root: root, Cfg: loaded})
	}
	if len(targets) == 0 && len(warnings) == 0 {
		warnings = append(warnings, output.Notef("", "no projects registered; run \"hydra init\" or \"hydra project add\""))
	}
	return targets, warnings, nil
}

// runHookEvent runs a configured hook chain for the active project unless
// --no-hooks was passed.
func runHookEvent(event string, hctx hooks.Context, cwd string) (hooks.Result, error) {
	return runHookEventForProject(cfg, projectRoot, event, hctx, cwd)
}

// runHookEventForProject is the explicit-project form, used by commands that
// operate on a target other than the ambient one.
//
// This is where the TRUST GATE lives, because this is the one function every hook execution
// funnels through. A hand-maintained list of gated commands was tried on paper and was wrong
// in both directions: it named `clone`, which is not a registered command, and omitted
// `repo add`, which is what actually reaches post_clone. Gating the funnel covers every
// present and future hook site, including a command that gains a hook later.
func runHookEventForProject(c *config.Config, root, event string, hctx hooks.Context, cwd string) (hooks.Result, error) {
	if noHooksFlag || c == nil {
		return hooks.Result{}, nil
	}
	// --no-hooks returned above, which is correct: it suppresses execution, and there is
	// nothing to approve if nothing runs. It does NOT bypass the branch_provider gate, which
	// is checked separately, because the two sets differ.
	if err := requireTrustedManifest(c, root); err != nil {
		return hooks.Result{}, err
	}
	// The repo in the context selects the chain: a hook set on that repo, or on its group, runs
	// after the workspace's. ResolveHooks walks the level chain; c.Hooks alone would skip both.
	chain, known := config.ResolveHooks(c, hctx.Group, hctx.Repo, event)
	if !known {
		return hooks.Result{}, output.Errorf(output.CodeInternal, "unknown hook event %q", event)
	}
	hctx.Event = event
	hctx.Project = c.Project
	hctx.ProjectRoot = root
	return hooks.Run(chain, hctx, cwd, os.Stderr)
}

// topicStore returns the active project's topic store.
//
// It is a function rather than a global because topic.Open performs no I/O — it
// only remembers the root — so there is no handle to hold, nothing to close, and
// no stale value to reset between commands or tests.
func topicStore() *topic.Store { return topic.Open(projectRoot) }

// classifyTopicErr maps the store's sentinel errors onto the output enum so no
// call site invents its own mapping.
func classifyTopicErr(err error) error {
	if err == nil {
		return nil
	}

	var claimed *topic.ErrClaimed
	if errors.As(err, &claimed) {
		return output.Errorf(output.CodeTopicConflict, "%s", claimed.Error()).
			WithDetail("repo", claimed.Repo).
			WithDetail("branch", claimed.Branch).
			WithDetail("existing_topic", claimed.Existing).
			WithDetail("requested_topic", claimed.Requested)
	}

	var ver *topic.ErrVersion
	if errors.As(err, &ver) {
		return output.Errorf(output.CodeStateVersionUnsupported, "%s", ver.Error()).
			WithDetail("path", ver.Path).
			WithDetail("found_version", ver.Found).
			WithDetail("supported_versions", []string{ver.Supported})
	}

	if topic.IsBusy(err) {
		return output.Wrap(output.CodeBusy, err,
			"another hydra process is writing topic state; retry").
			WithDetail("resource", "state").
			WithDetail("path", topic.LockPath(projectRoot))
	}

	return output.Wrap(output.CodeIOFailed, err, "failed to read or update topic state")
}

// versionInfo renders the version string.
//
// The ldflags vars are only set by `make build`. `go install pkg@version` applies
// no ldflags, so they stay at their defaults and the binary would report "dev"
// despite being a tagged release. Go records the real module version and VCS stamp
// in the embedded build info, so fall back to that.
func versionInfo() string {
	v := strings.TrimSpace(version)
	c := commit
	b := builtAt

	if v == "" || v == "dev" {
		if bv, bc, bt := buildInfoVersion(); bv != "" {
			v = bv
			if c == "" {
				c = bc
			}
			if b == "" {
				b = bt
			}
		}
	}
	if v == "" {
		v = "dev"
	}
	if !strings.HasPrefix(v, "v") && v != "dev" {
		v = "v" + v
	}

	parts := []string{v}
	if c != "" {
		parts = append(parts, c)
	}
	if b != "" {
		parts = append(parts, b)
	}
	return strings.Join(parts, " ")
}

// buildInfoVersion reads the module version, VCS revision, and build time that Go
// embeds in the binary. Version is empty for a plain `go build` from a source tree
// (Go reports "(devel)"), in which case the caller keeps "dev".
func buildInfoVersion() (version, commit, builtAt string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", ""
	}

	if v := info.Main.Version; v != "" && v != "(devel)" {
		version = v
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commit = setting.Value
			if len(commit) > 7 {
				commit = commit[:7]
			}
		case "vcs.time":
			builtAt = setting.Value
		}
	}
	return version, commit, builtAt
}

// withUsageGuidance attaches the offending command's usage line and a pointer at its help
// to cobra's argument and flag errors.
//
// These are `usage`: the caller mis-typed, nothing broke, and no state changed.
// They reported `internal` for two releases, which read as hydra breaking.
func withUsageGuidance(err error) error {
	msg := err.Error()
	usageish := strings.Contains(msg, "arg(s)") ||
		strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "unknown shorthand flag")
	if !usageish {
		return err
	}

	// executedCmd is set by Execute before the error surfaces, so the guidance names the
	// command the caller actually ran rather than the root.
	name, usage := "hydra", ""
	if executedCmd != nil {
		name = executedCmd.CommandPath()
		usage = executedCmd.UseLine()
	}
	// Errorf, not Wrap: the wrapped error IS msg, and wrapping doubled the text
	// ("unknown flag: --x: unknown flag: --x") in every envelope this built.
	wrapped := output.Errorf(output.CodeUsage, "%s", msg)
	if usage != "" {
		wrapped = wrapped.WithDetail("usage", usage)
	}
	return wrapped.WithNext(output.Next{
		Argv: append(strings.Fields(name), "--help"),
		Why:  "show this command's arguments and flags",
	})
}
