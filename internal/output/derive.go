package output

// Coverage is what a command claims to have done over a set of items.
//
// Commands must derive aggregate outcomes from these counts, not from whether their own
// code path reached the end. The verdict must reflect every item the command claimed to
// cover:
//
//   - Claimed is how many items the command set out to evaluate.
//   - Inspected is how many received a definite verdict; fewer than Claimed is partial.
//   - Failed is how many produced a definite failure.
//
// Derive applies these rules once for all commands so aggregation cannot disagree with
// per-item results.
type Coverage struct {
	// Claimed is how many items the command set out to cover.
	Claimed int
	// Inspected is how many it actually reached a verdict on. Fewer than Claimed means
	// something was silently skipped, which is a partial result however well the rest went.
	Inspected int
	// Failed is how many produced a definite failure.
	Failed int
}

// Derive returns the only outcome consistent with a coverage report.
//
// A command may not disagree with this. `success` requires that everything claimed was
// inspected and nothing failed; anything less is `partial`; nothing surviving at all is
// `failure`.
func (c Coverage) Derive() Outcome {
	switch {
	case c.Failed > 0 && c.Failed >= c.Claimed:
		return OutcomeFailure
	case c.Failed > 0:
		return OutcomePartial
	case c.Inspected < c.Claimed:
		return OutcomePartial
	default:
		return OutcomeSuccess
	}
}

// Complete reports whether every claimed item was inspected without failure. Callers use
// it to decide whether to return an error at all.
func (c Coverage) Complete() bool { return c.Derive() == OutcomeSuccess }

// faultWarnings are warning prefixes that describe hydra's own integrity, as opposed to
// advisory notes about the caller's request.
//
// `success` may never co-exist with one of these. A registered worktree missing from disk,
// a bare repository absent from the manifest, or unreadable state are all facts about the
// workspace being wrong — reporting them beside `outcome: success` is the same lie in a
// quieter register, and a caller gating on outcome or exit status sails past it.
// The stable half is the CODE prefix: warnings that describe a fault now carry one, which a
// consumer can match on and which does not change with the system locale. The English
// markers are a fallback for the warnings that are not coded yet, and exist so promoting a
// warning to a fault never depends on remembering to update this list first.
var faultWarnings = []string{
	CodeBareMissing + ":",
	CodeWorktreeUnknown + ":",
	CodeGitFailed + ":",
	CodeWorktreeDirty + ":",
	"bare repository missing",
	"worktree missing",
	"not registered",
	"unreadable",
	"cannot change to",
	"fatal:",
}

// HasFault reports whether any warning describes a workspace integrity problem rather than
// an advisory note about the request.
func HasFault(warnings []string) bool {
	for _, w := range warnings {
		if containsAny(w, faultWarnings) {
			return true
		}
	}
	return false
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if indexFold(s, n) >= 0 {
			return true
		}
	}
	return false
}

// indexFold is a case-insensitive substring search, kept local so this file does not depend
// on strings for one call and stays cheap on the hot path.
func indexFold(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	for i := range len(a) {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

func lower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
