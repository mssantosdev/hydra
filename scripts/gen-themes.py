#!/usr/bin/env python3
"""Generate editor/terminal theme files from hydra's own palette.

The palette lives in exactly one place: the `Hydra` theme in
internal/ui/themes/themes.go. This script parses those ten roles out of the Go
source and emits every downstream theme from them.

Why a generator rather than checked-in hand-written files: the first Ghostty
theme for hydra was produced by an ad-hoc script that was not kept. When the
palette was then edited, the terminal silently drifted away from the tool and
from the documentation, and the mismatch was only caught by eye. A generator in
the repo makes drift impossible to introduce quietly: change themes.go, re-run
this, and every target moves together.

Usage:
    python3 scripts/gen-themes.py            # write into contrib/
    python3 scripts/gen-themes.py --check    # fail if contrib/ is stale
    python3 scripts/gen-themes.py --install  # also install for the local user

Targets:
    contrib/ghostty/hydra      Ghostty theme (16-colour palette + bg/fg/cursor/selection)
    contrib/omp/hydra.json     omp theme (66 colour tokens, see omp://theme.md)
"""

from __future__ import annotations

import argparse
import colorsys
import json
import pathlib
import re
import sys

REPO = pathlib.Path(__file__).resolve().parent.parent
SOURCE = REPO / "internal" / "ui" / "themes" / "themes.go"


# ---------------------------------------------------------------- colour maths


def rgb(hexv: str) -> tuple[int, int, int]:
    h = hexv.lstrip("#")
    return tuple(int(h[i : i + 2], 16) for i in (0, 2, 4))  # type: ignore[return-value]


def hexs(parts) -> str:
    return "#%02x%02x%02x" % tuple(max(0, min(255, round(c))) for c in parts)


def shift(hexv: str, dv: float = 0.0, ds: float = 0.0) -> str:
    """Move value/saturation in HSV, which preserves hue identity.

    Used for bright ANSI variants and for tinted background blocks, so a derived
    colour always reads as a relative of its source rather than a new hue.
    """
    r, g, b = (c / 255 for c in rgb(hexv))
    h, s, v = colorsys.rgb_to_hsv(r, g, b)
    s = max(0.0, min(1.0, s + ds))
    v = max(0.0, min(1.0, v + dv))
    return hexs(c * 255 for c in colorsys.hsv_to_rgb(h, s, v))


def mix(a: str, b: str, t: float) -> str:
    ra, ga, ba = rgb(a)
    rb, gb, bb = rgb(b)
    return hexs((ra + (rb - ra) * t, ga + (gb - ga) * t, ba + (bb - ba) * t))


def _lin(c: float) -> float:
    c /= 255
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def luminance(hexv: str) -> float:
    r, g, b = rgb(hexv)
    return 0.2126 * _lin(r) + 0.7152 * _lin(g) + 0.0722 * _lin(b)


def contrast(a: str, b: str) -> float:
    la, lb = luminance(a), luminance(b)
    hi, lo = max(la, lb), min(la, lb)
    return (hi + 0.05) / (lo + 0.05)


# ---------------------------------------------------------------- the source


ROLES = (
    "Background Foreground Primary Secondary Success Warning Error "
    "Muted Border Highlight"
).split()


def read_palette() -> dict[str, str]:
    """Parse the Hydra theme's roles out of the Go source.

    Deliberately strict: a missing or renamed role is an error rather than a
    silent default, because a theme generated from a partial palette looks
    plausible and is wrong.
    """
    src = SOURCE.read_text()
    try:
        start = src.index("Hydra = Theme{")
    except ValueError:
        sys.exit(f"{SOURCE}: no `Hydra = Theme{{` block found")
    block = src[start:]
    block = block[: block.index("\t}")]

    pal = dict(re.findall(r"(\w+):\s+\"(#[0-9a-fA-F]{6})\"", block))
    missing = [r for r in ROLES if r not in pal]
    if missing:
        sys.exit(f"{SOURCE}: Hydra theme is missing roles: {', '.join(missing)}")
    return {k: v.lower() for k, v in pal.items() if k in ROLES}


def derived(pal: dict[str, str]) -> dict[str, str]:
    """Colours hydra has no role for, derived so they are never arbitrary."""
    # hydra defines no cyan. Interpolating Success->Primary keeps it inside the
    # palette's own span instead of importing a hue from somewhere else.
    cyan = shift(mix(pal["Success"], pal["Primary"], 0.5), dv=0.02, ds=0.06)
    return {
        "cyan": cyan,
        # dim sits below Muted; used for the quietest chrome.
        "dim": shift(pal["Muted"], dv=-0.10, ds=-0.02),
        # Selection must be visible against the ground. Border-as-selection was
        # 1.28:1 on this palette, i.e. invisible. Keep it in the ground's own
        # hue family so it introduces no colour cast.
        "selection": mix(pal["Border"], pal["Muted"], 0.35),
    }


# ---------------------------------------------------------------- ghostty


def ghostty(pal: dict[str, str], dv: dict[str, str]) -> str:
    p = {
        0: pal["Border"],
        1: pal["Error"],
        2: pal["Success"],
        3: pal["Warning"],
        4: pal["Primary"],
        5: pal["Secondary"],
        6: dv["cyan"],
        7: pal["Foreground"],
        8: pal["Muted"],
        9: shift(pal["Error"], dv=0.09),
        10: shift(pal["Success"], dv=0.11),
        11: shift(pal["Warning"], dv=0.08),
        12: shift(pal["Primary"], dv=0.10),
        13: shift(pal["Secondary"], dv=0.11),
        14: shift(dv["cyan"], dv=0.10),
        15: pal["Highlight"],
    }
    out = [
        "# hydra — GENERATED by scripts/gen-themes.py. Do not edit by hand.",
        "# Source of truth: internal/ui/themes/themes.go (theme \"hydra\").",
        "#",
        "# Install:  cp contrib/ghostty/hydra ~/.config/ghostty/themes/hydra",
        "#           then set `theme = hydra` in ~/.config/ghostty/config",
        "#",
        "# palette 6/14 (cyan) is the only derived pair: hydra has no cyan role,",
        "# so it is interpolated between Success and Primary.",
        "",
    ]
    out += [f"palette = {i}={p[i]}" for i in range(16)]
    out += [
        "",
        f"background = {pal['Background']}",
        f"foreground = {pal['Foreground']}",
        f"cursor-color = {pal['Highlight']}",
        f"cursor-text = {pal['Background']}",
        f"selection-background = {dv['selection']}",
        f"selection-foreground = {pal['Highlight']}",
        "",
    ]
    return "\n".join(out)


# ---------------------------------------------------------------- omp


def omp(pal: dict[str, str], dv: dict[str, str]) -> str:
    """Build an omp theme. Schema: omp://theme.md — 66 required colour tokens.

    Every token references a `vars` entry wherever one applies, so the mapping
    from a hydra role to an omp token is readable in the output rather than
    buried in resolved hex.
    """
    bg = pal["Background"]

    vars_ = {
        "bg": bg,
        "fg": pal["Foreground"],
        "primary": pal["Primary"],
        "secondary": pal["Secondary"],
        "success": pal["Success"],
        "warning": pal["Warning"],
        "error": pal["Error"],
        "muted": pal["Muted"],
        "border": pal["Border"],
        "highlight": pal["Highlight"],
        "cyan": dv["cyan"],
        "dim": dv["dim"],
        # Tinted blocks: the ground carried a little way toward a semantic hue,
        # so a tool result reads as its own state without becoming a colour field.
        "bgLift": shift(bg, dv=0.04),
        "bgSunk": shift(bg, dv=-0.03),
        "bgSuccess": mix(bg, pal["Success"], 0.14),
        "bgError": mix(bg, pal["Error"], 0.14),
        "bgUser": shift(bg, dv=0.06),
        "bgCustom": shift(bg, dv=0.03),
        "selected": dv["selection"],
    }

    colors = {
        # core text and borders
        "accent": "primary",
        "border": "border",
        "borderAccent": "primary",
        "borderMuted": "muted",
        "success": "success",
        "error": "error",
        "warning": "warning",
        "muted": "muted",
        "dim": "dim",
        "text": "fg",
        "thinkingText": "muted",
        # background blocks
        "selectedBg": "selected",
        "userMessageBg": "bgUser",
        "customMessageBg": "bgCustom",
        "toolPendingBg": "bgLift",
        "toolSuccessBg": "bgSuccess",
        "toolErrorBg": "bgError",
        "statusLineBg": "bgSunk",
        # message / tool text
        "userMessageText": "highlight",
        "customMessageText": "fg",
        "customMessageLabel": "secondary",
        "toolTitle": "highlight",
        "toolOutput": "muted",
        # markdown
        "mdHeading": "highlight",
        "mdLink": "primary",
        "mdLinkUrl": "muted",
        "mdCode": "warning",
        "mdCodeBlock": "fg",
        "mdCodeBlockBorder": "border",
        "mdQuote": "muted",
        "mdQuoteBorder": "border",
        "mdHr": "border",
        "mdListBullet": "primary",
        # diff
        "toolDiffAdded": "success",
        "toolDiffRemoved": "error",
        "toolDiffContext": "muted",
        # syntax — mapped so code reads with the same vocabulary as hydra's own
        # output: identifiers plain, references blue, states green/amber.
        "syntaxComment": "muted",
        "syntaxKeyword": "secondary",
        "syntaxFunction": "primary",
        "syntaxVariable": "fg",
        "syntaxString": "success",
        "syntaxNumber": "warning",
        "syntaxType": "cyan",
        "syntaxOperator": "cyan",
        "syntaxPunctuation": "muted",
        # thinking levels climb from quiet to loud
        "thinkingOff": "dim",
        "thinkingMinimal": "muted",
        "thinkingLow": "success",
        "thinkingMedium": "primary",
        "thinkingHigh": "cyan",
        "thinkingXhigh": "secondary",
        "thinkingMax": "error",
        "bashMode": "success",
        "pythonMode": "secondary",
        # status line
        "statusLineSep": "dim",
        "statusLineModel": "secondary",
        "statusLinePath": "primary",
        "statusLineGitClean": "success",
        "statusLineGitDirty": "warning",
        "statusLineContext": "cyan",
        "statusLineSpend": "primary",
        "statusLineStaged": "success",
        "statusLineDirty": "warning",
        "statusLineUntracked": "error",
        "statusLineOutput": "fg",
        "statusLineCost": "warning",
        "statusLineSubagents": "secondary",
    }

    theme = {
        "$schema": "https://oh-my-pi.dev/theme-schema.json",
        "name": "hydra",
        "vars": vars_,
        "colors": colors,
        "export": {
            "pageBg": vars_["bgSunk"],
            "cardBg": bg,
            "infoBg": vars_["bgLift"],
        },
    }
    return json.dumps(theme, indent=2) + "\n"


# ---------------------------------------------------------------- checks


def audit(pal: dict[str, str], dv: dict[str, str]) -> list[str]:
    """Report contrast problems rather than emitting them silently.

    These are warnings, not failures: a palette is the owner's decision. What
    is not acceptable is shipping one without knowing where it is weak.
    """
    bg = pal["Background"]
    notes = []
    for role in ("Foreground", "Primary", "Secondary", "Success", "Warning", "Error", "Highlight"):
        c = contrast(pal[role], bg)
        if c < 4.5:
            notes.append(f"{role} {pal[role]} on {bg} is {c:.2f}:1 (below WCAG AA 4.5)")
    m = contrast(pal["Muted"], bg)
    if m < 3.0:
        notes.append(f"Muted {pal['Muted']} on {bg} is {m:.2f}:1 (below 3.0, hard to read)")
    s = contrast(dv["selection"], bg)
    if s < 1.5:
        notes.append(f"selection {dv['selection']} on {bg} is {s:.2f}:1 — a selection this close to the ground is invisible")
    return notes


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="fail if contrib/ is stale")
    ap.add_argument("--install", action="store_true", help="also install for the current user")
    args = ap.parse_args()

    pal = read_palette()
    dv = derived(pal)

    targets = {
        REPO / "contrib" / "ghostty" / "hydra": ghostty(pal, dv),
        REPO / "contrib" / "omp" / "hydra.json": omp(pal, dv),
    }

    if args.check:
        stale = [p for p, body in targets.items() if not p.exists() or p.read_text() != body]
        if stale:
            for p in stale:
                print(f"stale: {p.relative_to(REPO)}", file=sys.stderr)
            print("run: python3 scripts/gen-themes.py", file=sys.stderr)
            return 1
        print("themes are up to date with internal/ui/themes/themes.go")
        return 0

    for p, body in targets.items():
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(body)
        print(f"wrote {p.relative_to(REPO)}")

    if args.install:
        home = pathlib.Path.home()
        installs = {
            home / ".config" / "ghostty" / "themes" / "hydra": targets[REPO / "contrib" / "ghostty" / "hydra"],
            home / ".omp" / "agent" / "themes" / "hydra.json": targets[REPO / "contrib" / "omp" / "hydra.json"],
        }
        for p, body in installs.items():
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(body)
            print(f"installed {p}")

    for note in audit(pal, dv):
        print(f"warning: {note}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
