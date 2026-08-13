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
	Claimed   int
	Inspected int
	Failed    int
}

// Derive returns the only outcome consistent with a coverage report.
//
// A command may not disagree with this. `success` requires that everything claimed was
// inspected and nothing failed; anything less is `partial`; nothing surviving at all is
// `failure`.
func (c Coverage) Derive() Outcome {
	switch {
	case c.Claimed == 0:
		return OutcomeSuccess
	case c.Failed == c.Claimed:
		return OutcomeFailure
	case c.Failed > 0 || c.Inspected < c.Claimed:
		return OutcomePartial
	default:
		return OutcomeSuccess
	}
}

// Complete reports whether every claimed item was inspected without failure. Callers use
// it to decide whether to return an error at all.
func (c Coverage) Complete() bool { return c.Derive() == OutcomeSuccess }

// HasFault reports whether any diagnostic must prevent a success verdict. Severity alone
// decides: error and warning degrade; note never does.
func HasFault(warnings []*Diagnostic) bool {
	for _, w := range warnings {
		if w != nil && w.IsFault() {
			return true
		}
	}
	return false
}

// CountFaults counts diagnostics that must prevent a success verdict.
func CountFaults(warnings []*Diagnostic) int {
	n := 0
	for _, w := range warnings {
		if w != nil && w.IsFault() {
			n++
		}
	}
	return n
}
