package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/trust"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

// trustAcceptEnv is the unattended form of --accept.
//
// It exists because an agent cannot answer a prompt, and "trust everything in CI" is not a
// gate. The expected fingerprint belongs in CI configuration — a file the team controls —
// rather than in the repository being checked out, so a hostile branch cannot approve itself
// by editing its own pinned hash.
const trustAcceptEnv = "HYDRA_TRUST_ACCEPT"

var (
	trustShow   bool
	trustRevoke bool
	trustAccept string
)

type trustJSON struct {
	Workspace   string `json:"workspace"`
	Trusted     bool   `json:"trusted"`
	Fingerprint string `json:"fingerprint"`
	// Approved is the fingerprint on record. Absent when nothing was ever approved.
	Approved   string `json:"approved,omitempty"`
	ApprovedAt string `json:"approved_at,omitempty"`
	Reason     string `json:"reason,omitempty"`
	// Changed names the manifest paths whose executable value differs from what was
	// approved. Never their values: a hook line is where a credential ends up.
	Changed []string `json:"changed,omitempty"`
	// Executable counts the manifest values that can cause execution. Zero means this
	// workspace never needs trusting.
	Executable int  `json:"executable"`
	Revoked    bool `json:"revoked,omitempty"`
}

var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Approve this workspace's manifest to execute hooks and branch providers",
	Long: `Approve the executable content of this workspace's manifest.

DESCRIPTION
  .hydra/config.yaml is meant to be shared: it is committed, "repo restore" rebuilds a
  workspace from it, and a team edits it together. So it arrives the way any source file
  does — you pull a branch. Its "hooks" and any "branch_provider" with a "run:" are shell
  commands hydra executes as you.

  Trust is ABSENT BY DEFAULT. Until this workspace is approved, every command that would
  execute manifest content refuses with "manifest_untrusted" (exit 2). Read-only commands —
  list, status, doctor, where, path, config show, hooks ls — always work, because inspecting
  a workspace is how you decide whether to trust it.

  Approval covers the EXECUTABLE surface only, so adding a repository, editing base_branch,
  reformatting or adding a comment costs nothing. Changing, adding, reordering or removing a
  hook, or a runnable branch_provider, invalidates it — and hydra then names which manifest
  paths changed, never their contents.

  A workspace whose manifest has no hooks and no runnable branch_provider never needs this:
  there is nothing to approve, and the gate is skipped entirely.

UNATTENDED USE
  hydra trust --accept sha256:<expected>     approves only if the fingerprint matches
  HYDRA_TRUST_ACCEPT=sha256:<expected>       the same, for CI

  Keep the expected value in your CI configuration, NOT in the repository being checked
  out — otherwise a branch can approve itself. Get it once with "hydra trust --show",
  review the hooks, and commit it. When someone changes a hook, CI fails until a human
  re-reviews and updates the pin.

EXAMPLES
  hydra trust                      # approve this workspace as it stands
  hydra trust --show               # what is approved, and what changed since
  hydra trust --revoke             # forget this workspace
  hydra trust --accept sha256:ab…  # approve only if it matches (CI)

SEE ALSO
  • hydra hooks ls  - what this manifest would run
  • hydra prune     - drops trust entries whose workspace is gone`,
	Args: cobra.NoArgs,
	RunE: runTrust,
}

func init() {
	trustCmd.Flags().BoolVar(&trustShow, "show", false, "Print the current trust state without changing it")
	trustCmd.Flags().BoolVar(&trustRevoke, "revoke", false, "Forget this workspace")
	trustCmd.Flags().StringVar(&trustAccept, "accept", "",
		"Approve only if the fingerprint equals this value (or set "+trustAcceptEnv+")")
	rootCmd.AddCommand(trustCmd)
}

func runTrust(cmd *cobra.Command, _ []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	if trustShow && trustRevoke {
		return output.Errorf(output.CodeUsage, "--show and --revoke are mutually exclusive").
			WithDetail("valid", []string{"--show", "--revoke"})
	}

	configDir := global.GetConfigDir()
	surface := config.ExecutableSurface(cfg)
	payload := trustJSON{Workspace: projectRoot, Executable: len(surface)}

	switch {
	case trustRevoke:
		removed, err := trust.Revoke(configDir, projectRoot)
		if err != nil {
			return classifyTrustErr(err)
		}
		payload.Revoked = removed
		payload.Trusted = false
		payload.Fingerprint = trust.Fingerprint(cfg)
		summary := "trust revoked"
		if !removed {
			summary = "this workspace was not trusted"
		}
		return emit(cmd, summary, payload, nil, func() {
			fmt.Println(styles.Success.Render("✓ " + summary))
		})

	case trustShow:
		status, err := trust.Check(configDir, projectRoot, cfg)
		if err != nil {
			return classifyTrustErr(err)
		}
		fillTrustPayload(&payload, status, configDir)
		return emit(cmd, trustShowSummary(payload), payload, nil, func() { printTrustText(payload) })
	}

	expected := trust.NormalizeExpected(firstNonEmptyString(trustAccept, os.Getenv(trustAcceptEnv)))
	status, err := trust.Approve(configDir, projectRoot, cfg, expected)
	if err != nil {
		return classifyTrustErr(err)
	}
	if !status.Trusted && status.Reason == trust.ReasonMismatch {
		// Nothing was written. The pinned value disagreeing with reality is the exact
		// condition the pin exists to catch, so it is a refusal rather than a prompt.
		return untrustedError(status, nil, projectRoot).
			WithDetail("expected", expected).
			WithDetail("observed", status.Fingerprint)
	}
	fillTrustPayload(&payload, status, configDir)
	summary := fmt.Sprintf("workspace trusted (%d executable manifest %s)",
		payload.Executable, plural(payload.Executable, "value", "values"))
	if payload.Executable == 0 {
		summary = "nothing to trust: this manifest executes nothing"
	}
	return emit(cmd, summary, payload, nil, func() {
		fmt.Println(styles.Success.Render("✓ " + summary))
		fmt.Printf("  Fingerprint: %s\n", payload.Fingerprint)
	})
}

func fillTrustPayload(payload *trustJSON, status trust.Status, configDir string) {
	payload.Trusted = status.Trusted
	payload.Fingerprint = status.Fingerprint
	payload.Approved = status.Approved
	payload.ApprovedAt = status.ApprovedAt
	payload.Reason = status.Reason
	if status.Reason == trust.ReasonChanged {
		if entries, err := trust.Load(configDir); err == nil {
			payload.Changed = trust.ChangedPaths(cfg, entries[projectRoot])
		}
	}
}

func trustShowSummary(payload trustJSON) string {
	switch {
	case payload.Executable == 0:
		return "this manifest executes nothing; no trust needed"
	case payload.Trusted:
		return "workspace trusted, approved " + payload.ApprovedAt
	case payload.Reason == trust.ReasonChanged:
		return fmt.Sprintf("trust is stale: %d executable %s changed since approval",
			len(payload.Changed), plural(len(payload.Changed), "entry", "entries"))
	default:
		return "workspace not trusted"
	}
}

func printTrustText(payload trustJSON) {
	fmt.Println()
	fmt.Printf("  Workspace:   %s\n", payload.Workspace)
	fmt.Printf("  Executable:  %d manifest %s\n",
		payload.Executable, plural(payload.Executable, "value", "values"))
	fmt.Printf("  Fingerprint: %s\n", payload.Fingerprint)
	if payload.Trusted {
		fmt.Println("  " + styles.Success.Render("trusted") + " since " + payload.ApprovedAt)
	} else if payload.Executable > 0 {
		fmt.Println("  " + styles.Error.Render("not trusted") + " — run \"hydra trust\"")
	}
	for _, path := range payload.Changed {
		fmt.Printf("    changed: %s\n", path)
	}
	fmt.Println()
}

// requireTrustedManifest is the gate. It is called from the one function every hook execution
// funnels through, and separately where a runnable branch_provider is invoked.
func requireTrustedManifest(c *config.Config, root string) error {
	// A manifest that executes nothing has nothing to approve, so a workspace created by
	// `hydra init` and never given a hook never sees this feature. Derived from the same
	// list the fingerprint uses, so the two cannot disagree.
	if !config.HasExecutableSurface(c) {
		return nil
	}
	configDir := global.GetConfigDir()
	status, err := trust.Check(configDir, root, c)
	if err != nil {
		return classifyTrustErr(err)
	}
	if status.Trusted {
		return nil
	}
	var changed []string
	if status.Reason == trust.ReasonChanged {
		if entries, loadErr := trust.Load(configDir); loadErr == nil {
			changed = trust.ChangedPaths(c, entries[root])
		}
	}
	return untrustedError(status, changed, root)
}

// untrustedError builds the one envelope shape all three refusal reasons share. An agent
// forced to special-case three variants of one error will special-case two and mishandle the
// third.
func untrustedError(status trust.Status, changed []string, root string) *output.Error {
	err := output.Errorf(output.CodeManifestUntrusted,
		"this workspace's manifest can execute commands and is not trusted; run \"hydra trust\"").
		WithSubject("workspace", root).
		WithDetail("workspace", root).
		WithDetail("reason", status.Reason).
		WithDetail("fingerprint", status.Fingerprint).
		WithDetail("changed", changed).
		WithNext(output.Next{
			Argv: []string{"hydra", "hooks", "ls", "--output", "json"},
			Why:  "see what this manifest would run before approving it",
		}, output.Next{
			Argv: []string{"hydra", "trust"},
			Why:  "approve this workspace's executable manifest content",
		})
	if status.Reason == trust.ReasonChanged {
		err.Message = "this workspace's executable manifest content changed since it was trusted; " +
			"review the diff, then run \"hydra trust\""
	}
	return err
}

// classifyTrustErr maps a store problem onto the enum. A store hydra refuses to honour is a
// config_invalid: something on this machine has to be fixed by hand, and it is not the
// manifest.
func classifyTrustErr(err error) error {
	if unsafe, ok := trust.AsUnsafe(err); ok {
		return output.Errorf(output.CodeConfigInvalid, "%s", unsafe.Error()).
			WithDetail("path", unsafe.Path).
			WithDetail("reason", unsafe.Reason)
	}
	return output.Wrap(output.CodeInternal, err, "failed to read the trust store")
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
