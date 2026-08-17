// Package carry places the files a new worktree needs that git will not bring.
//
// A fresh worktree has every tracked file and no `.env`, so it cannot run. The workaround —
// copy it by hand, per worktree, per repo — is learned in week one and paid forever, which is
// why it never appears in an issue tracker.
//
// This is materialisation, not a side effect, so it is not a hook: hydra already owns layout
// and already knows the source worktree, where a hook would have to rebuild a path `--as` can
// override. It runs BEFORE post_add, so a hook that installs dependencies can rely on the
// configuration being present.
package carry

import (
	"errors"
	"fmt"
	"github.com/mssantosdev/hydra/internal/output"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
)

// Result is one entry's outcome. Every entry produces one, including the ones that did
// nothing, because "why is my .env missing" has to be answerable from the envelope.
type Result struct {
	Dest string `json:"dest"`
	// From is the resolved absolute source, empty when there was none to resolve.
	From string `json:"from,omitempty"`
	Mode string `json:"mode"`
	// Disposition: placed, skipped (already there), or missing (no source).
	Disposition string `json:"disposition"`
	Reason      string `json:"reason,omitempty"`
}

const (
	Placed  = "placed"
	Skipped = "skipped"
	Missing = "missing"
)

// Plan is where an entry's source comes from. The caller resolves the source worktree,
// because only it knows whether --from named a branch that has one.
type Plan struct {
	// WorktreePath is the new worktree, the destination root.
	WorktreePath string
	// SourceWorktree is the worktree bare-form entries copy from. Empty on a fresh clone,
	// where nothing has been checked out yet — every bare entry then reports missing.
	SourceWorktree string
	// WorkspaceRoot resolves `from:` entries, which are the only ones that work without a
	// prior worktree.
	WorkspaceRoot string
	// OutsideAllowed permits entries whose source reaches outside the workspace (absolute or
	// ~/). Set by the caller only when the manifest is trusted; carry never reads the trust
	// store itself — the store is a cmd-layer concern, and this package stays testable
	// without one.
	OutsideAllowed bool
}

// Apply places every entry and returns one Result each, plus warnings for anything it could
// not do. It NEVER returns a fatal error for a missing source: a `.env` that does not exist
// yet is the normal state of a fresh machine, and failing the worktree creation over it would
// make `repo restore` unusable on exactly the machine that needs it most.
func Apply(entries []config.CarryEntry, plan Plan) ([]Result, []*output.Diagnostic) {
	if len(entries) == 0 {
		return nil, nil
	}
	results := make([]Result, 0, len(entries))
	var warnings []*output.Diagnostic

	// Every write goes through a root opened on the worktree, so containment is enforced by
	// the kernel rather than by a check we have to remember. os.Root refuses any path that
	// escapes it, INCLUDING through a symlink swapped mid-operation — the TOCTOU that a
	// filepath.Join plus a string check cannot close. This matters because a manifest is
	// designed to be handed between people: "copy John's manifest" must never be able to mean
	// "write to my ~/.ssh on the next add".
	root, err := os.OpenRoot(plan.WorktreePath)
	if err != nil {
		return nil, []*output.Diagnostic{
			output.Warnf(output.CodeIOFailed, "carry: cannot open %s", plan.WorktreePath).
				WithSubject("worktree", plan.WorktreePath).
				WithCause(err.Error()),
		}
	}
	defer func() { _ = root.Close() }()

	for _, entry := range entries {
		res := Result{Dest: entry.Dest(), Mode: mode(entry)}

		src, reason, code := source(entry, plan)
		if src == "" {
			res.Disposition = Missing
			res.Reason = reason
			results = append(results, res)
			// A code means hydra REFUSED or the declared file is absent: the caller asked for a
			// file and did not get it, so this is a warning and the envelope degrades to partial.
			// No code means the one expected outcome — a bare entry on a fresh clone, which has
			// no source worktree — and that stays a note, because replaying structure without
			// secrets is the documented limit rather than a fault.
			diag := output.Notef("", "carry %s: %s", res.Dest, reason)
			if code != "" {
				diag = output.Warnf(code, "carry %s: %s", res.Dest, reason)
			}
			warnings = append(warnings, diag.
				WithSubject("worktree", filepath.Base(plan.WorktreePath)))
			continue
		}
		res.From = src

		// Relative to the root. The `..` and absolute cases are already refused when the
		// manifest is parsed; this second check turns what would be an opaque syscall error
		// into a reason a reader can act on.
		// Cleaned ONCE, here, so every later check sees the same path. The Lstat below and
		// the OpenFile in copyFile are two independent answers to "does it exist", and an
		// uncleaned `to: "sub/"` made them disagree: Lstat failed on the trailing slash while
		// OpenFile found the file, which was then reported as a write that never happened.
		rel := filepath.Clean(filepath.FromSlash(entry.Dest()))
		if !relWithin(rel) {
			res.Disposition = Missing
			res.Reason = "destination escapes the worktree"
			results = append(results, res)
			warnings = append(warnings, output.Warnf(output.CodeConfigInvalid,
				"carry %s: destination escapes the worktree; refused", res.Dest).
				WithSubject("worktree", filepath.Base(plan.WorktreePath)))
			continue
		}

		// Never clobber. A file already in the new worktree was either carried by an earlier
		// run — so this is the convergent no-op the invariant promises — or written by
		// someone since, and overwriting that would destroy work no one asked us to touch.
		if _, err := root.Lstat(rel); err == nil {
			res.Disposition = Skipped
			res.Reason = "already present"
			results = append(results, res)
			continue
		}

		if err := place(root, src, rel, res.Mode, entry.OutsideWorkspace()); err != nil {
			// The Lstat above is a check, not a lock: something can appear between it and
			// the write. Reporting that as Placed would claim a write that never happened.
			if errors.Is(err, errAlreadyPresent) {
				res.Disposition = Skipped
				res.Reason = "already present"
				results = append(results, res)
				continue
			}
			res.Disposition = Missing
			res.Reason = err.Error()
			results = append(results, res)
			warnings = append(warnings, output.Warnf(output.CodeIOFailed, "carry %s: %v", res.Dest, err).
				WithSubject("worktree", filepath.Base(plan.WorktreePath)).
				WithCause(err.Error()))
			continue
		}
		res.Disposition = Placed
		results = append(results, res)
	}
	return results, warnings
}

func mode(e config.CarryEntry) string {
	if e.Mode == config.CarryLink {
		return config.CarryLink
	}
	return config.CarryCopy
}

// errAlreadyPresent means the destination appeared between the Lstat check and the write. It is
// not a failure and it is not a placement: the caller reports it as Skipped, so a count of
// carried files never includes one that was already there.
var errAlreadyPresent = errors.New("already present")

// source resolves an entry to an absolute path, or returns why it could not and how severe that
// is: a non-empty code marks an outcome the caller must act on, an empty one marks the single
// expected case.
func source(e config.CarryEntry, plan Plan) (src, reason, code string) {
	if e.FromWorkspace() {
		if e.OutsideWorkspace() {
			// The EXPLICIT outside spellings (absolute, ~/). Reading beyond the workspace is
			// machine authority like running a hook, so it takes the same approval; the entry
			// is on the manifest's trust surface, and the caller sets OutsideAllowed only when
			// that surface is approved. No within() here — the declaration plus the approval
			// IS the containment decision.
			if !plan.OutsideAllowed {
				return "", "source reaches outside the workspace and the manifest is not trusted; run \"hydra trust\"",
					output.CodeManifestUntrusted
			}
			abs, err := expandHome(e.From)
			if err != nil {
				return "", fmt.Sprintf("cannot resolve %s: %v", e.From, err), output.CodeCarryRefused
			}
			if _, err := os.Stat(abs); err != nil {
				return "", fmt.Sprintf("%s does not exist on this machine", e.From), output.CodeCarryRefused
			}
			return abs, "", ""
		}
		if plan.WorkspaceRoot == "" {
			// No workspace to resolve against is the ENVIRONMENT lacking one, not a refusal and
			// not a broken invariant: nothing was declared wrongly and nothing was denied, so it
			// stays an uncoded note. TestApply_MissingWorkspaceRootIsANoteNotAFailure pins this.
			return "", "no workspace root to resolve `from:` against", ""
		}
		abs := filepath.Join(plan.WorkspaceRoot, filepath.FromSlash(e.From))
		if !within(plan.WorkspaceRoot, abs) {
			return "", "source escapes the workspace; refused — name the target explicitly (an absolute or ~/ path, requires \"hydra trust\")",
				output.CodeCarryRefused
		}
		if _, err := os.Stat(abs); err != nil {
			// A `from:` entry names a fixed workspace path, so an absent file is NOT the
			// fresh-clone case below: the workspace exists and the declared file is missing from
			// it. The docs promise a warning naming the file so you know what to provide.
			return "", fmt.Sprintf("%s does not exist in the workspace", e.From), output.CodeCarryRefused
		}
		return abs, "", ""
	}
	// Bare form: the same relative path in the source worktree. On a fresh clone there is no
	// source worktree, which is the honest limit of replaying a manifest — structure, not
	// secrets. That one is expected, so it carries no code.
	if plan.SourceWorktree == "" {
		return "", "no source worktree to copy from (a fresh workspace carries structure, not secrets)", ""
	}
	abs := filepath.Join(plan.SourceWorktree, filepath.FromSlash(e.Path))
	if !within(plan.SourceWorktree, abs) {
		return "", "source escapes the source worktree; refused", output.CodeCarryRefused
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Sprintf("%s is not in %s", e.Path, filepath.Base(plan.SourceWorktree)), ""
	}
	return abs, "", ""
}

// expandHome resolves a leading ~/ against this machine's home. Only ~/ expands: $VARS and
// ~user would be a second and third expansion language for no named need, and the manifest
// validator already refused them at parse time.
func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}

// place writes one entry. dest is relative to root, so every operation is confined by the
// kernel: os.Root resolves each component itself and refuses anything leaving the root, which
// is what closes the symlink TOCTOU a filepath.Join plus a string check leaves open.
//
// dest is cleaned FIRST, and that is a security step rather than tidiness. CVE-2026-39822 was a
// root escape in os through a symlink plus a TRAILING SLASH: the guarantee this function leans on
// had a hole reachable from a manifest-authored `to:` value. go.mod pins a patched toolchain, but
// a control that only holds when the builder's Go is current is not a control — cleaning here
// means the escape has no spelling to arrive in.
func place(root *os.Root, src, dest, m string, outside bool) error {
	dest = filepath.Clean(dest)
	if dir := filepath.Dir(dest); dir != "." {
		if err := root.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}
	if m == config.CarryLink {
		// The link is created inside the root; where it POINTS is not the root's business,
		// and pointing outside is the whole purpose of carrying a shared file by link.
		//
		// INSIDE sources get a relative target, so the workspace survives being moved or
		// mounted elsewhere. OUTSIDE sources get the absolute path: the store lives at a fixed
		// machine location the workspace does not contain, so relativizing would break the
		// link the first time the worktree moves — and `ls -l` showing `/home/me/.arvia/…` is
		// the honest rendering of what the manifest declared.
		target := src
		if !outside {
			if rel, err := filepath.Rel(filepath.Dir(filepath.Join(root.Name(), dest)), src); err == nil {
				target = rel
			}
		}
		return root.Symlink(target, dest)
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyTree(root, src, dest)
	}
	return copyFile(root, src, dest, info.Mode())
}

func copyFile(root *os.Root, src, dest string, perm os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // G304: src is a manifest-declared path, containment checked in source()
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	// O_EXCL rather than O_TRUNC: the Lstat in Apply is a check, and between it and here
	// another process could have created the file. Failing beats silently overwriting a
	// secret someone just wrote.
	out, err := root.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm.Perm())
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errAlreadyPresent
		}
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// copyTree walks a carried directory. Reads use plain os because the source is outside the
// destination root; every WRITE goes through root, so a symlink appearing mid-walk cannot
// redirect one outside the worktree.
func copyTree(root *os.Root, src, dest string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return root.MkdirAll(target, 0o750)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// A symlink inside a carried directory is copied as a link, not followed: what it
			// points at may be outside the workspace entirely, and resolving it here would
			// turn a carried directory into an exfiltration path.
			link, readErr := os.Readlink(p)
			if readErr != nil {
				return readErr
			}
			return root.Symlink(link, target)
		}
		return copyFile(root, p, target, info.Mode())
	})
}

// relWithin reports whether a worktree-relative path stays inside it. os.Root would refuse an
// escape anyway; this exists so the refusal carries a reason instead of a syscall error.
func relWithin(rel string) bool {
	if filepath.IsAbs(rel) {
		return false
	}
	clean := filepath.Clean(rel)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

// Summary renders the counts for a text-mode line, or "" when nothing was carried.
func Summary(results []Result) string {
	var placed, skipped, missing int
	for _, r := range results {
		switch r.Disposition {
		case Placed:
			placed++
		case Skipped:
			skipped++
		case Missing:
			missing++
		}
	}
	if placed+skipped+missing == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	if placed > 0 {
		parts = append(parts, fmt.Sprintf("%d carried", placed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d already present", skipped))
	}
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("%d missing", missing))
	}
	return strings.Join(parts, ", ")
}

// within reports whether resolved is base or below it, AFTER following symlinks.
//
// filepath.Rel is the check rather than a string prefix, so `/tmp/ws-evil` is not mistaken for
// something inside `/tmp/ws`. Symlinks are resolved first because the joined path alone lies: a
// repository can COMMIT a symlink, so `from: link/id_rsa` with `link -> ~/.ssh` passes a textual
// check and reads a key from outside the workspace into the worktree. Both halves of that are
// attacker-supplied — the manifest and the repository content — which is the case this refuses.
//
// The write side is stronger: it goes through os.Root, so the kernel refuses an escape even if a
// symlink is swapped mid-operation. This check runs at resolve time, so that narrow TOCTOU stays
// open on the read side; closing it means reading through a root handle too.
func within(base, resolved string) bool {
	if base == "" {
		return false
	}
	// A path that does not exist cannot be followed, so the textual form is all there is. That is
	// not a hole: the caller stats the source immediately after and reports a missing one, and a
	// symlink that does not resolve cannot be read through either.
	realBase := base
	if r, err := filepath.EvalSymlinks(base); err == nil {
		realBase = r
	}
	realPath := resolved
	if r, err := filepath.EvalSymlinks(resolved); err == nil {
		realPath = r
	}
	rel, err := filepath.Rel(realBase, realPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
