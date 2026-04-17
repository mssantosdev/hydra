---
name: review-checklist
description: Apply Hydra's review contract and return approval or actionable requested changes.
---
## Review contract

Your result must be one of:

- `approved`
- `changes_requested`

If changes are requested, include:

- findings
- required changes
- guidance
- validation expectations for resubmission

## Focus areas

- task definition and acceptance criteria from the assigned planning task file
- acceptance criteria
- regressions and risks
- tests
- docs/help drift when behavior changed
- reviewed SHA and whether the runtime file records it
- whether integration-style validation was performed where feasible

## Important

Do not fix the code directly.

Record the review outcome, reviewed SHA, next owner, and guidance in the runtime task file when operating in a write-capable review flow, or explicitly return the text needed for that update.
