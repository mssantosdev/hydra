# LinkedIn — engineering write-up

Register of what is true: 10 releases (v0.2.0 → v0.3.9), 16 real bugs found by 16
zero-context agents across 4 rounds, e2e 43 → 226 assertions, gate green throughout.
No users, no downloads, no benchmarks — none exist, so none are claimed.

---

## Option A — the one I'd post (≈240 words)

I spent this week building a Go CLI called hydra, and the most useful thing that happened
had nothing to do with writing it.

hydra manages git worktrees across several repositories. Git gives you one worktree in one
repo; a piece of work rarely stays in one. So it records which worktree, on which branch,
in which repository, belongs to which unit of work — and it *records* that rather than
inferring it from a branch name.

Then I stopped writing features and did something else: I gave 16 agents zero context and
the binary, and told them to figure it out and write down every place they got stuck.

They found 16 real bugs. Six were in code I'd written hours earlier.

But the pattern mattered more than the count. Five consecutive releases fixed five
instances of *the same class*: a command reporting `success` while something had failed.
Each fix was correct and local, and the class came back in whichever command aggregated
next.

The fix that finally worked wasn't a sixth fix. It was making `success` structurally
impossible to claim beside a failure, enforced at the one boundary every command's output
passes through — and then deriving the process exit status from that output rather than
from what the function returned.

Two boundaries. The class has nowhere left to appear.

The lesson I'm keeping: when the same bug keeps coming back, you're fixing instances of
something you haven't named yet.

github.com/mssantosdev/hydra

---

## Option B — shorter, sharper (≈130 words)

Five releases in a row, I fixed the same bug.

Different command each time. A tool reporting `success` while something had failed. Each
fix was correct, local, and complete. Each time it came back somewhere else.

The sixth fix wasn't a fix. It was an invariant: make `success` structurally impossible to
claim beside a failure, enforced at the single boundary every command's output passes
through. Then derive the process exit code from that output instead of from what the
function returned.

Two boundaries, and the class has nowhere left to appear.

I found all of it by handing 16 agents a binary with zero context and asking them to write
down every place they got stuck. 16 real bugs. Six in code I'd written that same day.

When a bug keeps coming back, you're fixing instances of something you haven't named.

github.com/mssantosdev/hydra

---

## Notes on both

- No adoption claims, no "production-ready", no stars/downloads. There are none.
- The `success`-beside-a-failure story is the honest centre: it is specific, verifiable in
  the CHANGELOG, and the lesson generalises beyond Go or CLIs.
- "16 agents, 16 bugs" is accurate but reads as a coincidence; it isn't one, and neither
  number is rounded.
- Deliberately omitted: the round-5 result where an agent reported 6 findings and 5 didn't
  reproduce. It's the most interesting methodological caveat but it needs a paragraph to
  land, and a post that hedges its own evidence mid-flow reads as unsure. It belongs in a
  longer article if one gets written.
- Tone check: no emoji, no "🚀", no "excited to share", no rhetorical questions.
