---
name: runtime-state-discipline
description: Keep coordination runtime state aligned with review, approval, and merge reality.
---
## Core rule

Use `coordination/` as the operational source of truth for live task state. Do not rely on chat summaries alone when runtime state is stale.

## Required runtime fields

Every official task handoff should keep these fields current when the file layout supports them:

- current owner
- status
- reviewer state
- merger state
- current branch HEAD under review or approved SHA
- next expected action
- latest reviewer decision
- validation run summary

## Before review

- implementer records review-ready state
- implementer records validation commands run
- official review should target a committed branch HEAD SHA whenever practical
- if review is against a working tree, say so explicitly in the handoff summary

## After review

- reviewer records `approved` or `changes_requested`
- reviewer records the reviewed SHA
- reviewer records the next owner and required validation expectations

## Before merge

- merger confirms runtime state says `approved`
- merger confirms the approved SHA exists on the branch being merged
- merger blocks if runtime state and branch reality disagree

## After integration

- merger records integrated branches or SHAs
- merger records integration-only fixes separately from approved lane SHAs
- merger records integration validation and residual risks
