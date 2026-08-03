// Package skills carries hydra's own agent skill as embedded content.
//
// The directive lives here rather than in internal/skill because go:embed cannot
// reach outside its package directory, and skills/hydra/SKILL.md is the single
// source of truth: it is what a consuming workspace receives from
// `hydra skill --install`, and what AGENTS.md links.
package skills

import _ "embed"

// SkillMD is the verbatim contents of skills/hydra/SKILL.md.
//
//go:embed hydra/SKILL.md
var SkillMD string
