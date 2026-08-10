#!/usr/bin/env python3
"""Load every manifest the docs show and check hydra sees every repository declared.

Doc examples drifted from the schema four times before this existed. A group left in the old
schema-2 nesting still parses -- as a group with no repositories -- so the failure is silent and
the example looks fine to a reader who does not run it.
"""
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile

import yaml

FILES = [
    "docs/configuration.md",
    "README.md",
    "docs/design/workspace-model.md",
    "docs/guide.html",
]


def blocks(path: pathlib.Path):
    text = path.read_text()
    if path.suffix == ".html":
        raw = re.findall(r'<div class="plate">(version:.*?)</div>', text, re.S)
        return [re.sub(r"<[^>]+>", "", b) for b in raw]
    return re.findall(r"```yaml\n(.*?)```", text, re.S)


def main() -> int:
    hydra = pathlib.Path("hydra").resolve()
    problems = 0
    for name in FILES:
        path = pathlib.Path(name)
        if not path.exists():
            continue
        for index, block in enumerate(blocks(path), 1):
            if "groups:" not in block or not block.lstrip().startswith("version:"):
                continue
            # Skeletons using <placeholders> or {} stand in for shape, not for a real manifest.
            after = block.split("groups:", 1)[1][:40]
            if "<" in block or "{" in after:
                continue
            try:
                doc = yaml.safe_load(block)
            except yaml.YAMLError as exc:
                print(f"{name} #{index}: invalid YAML: {exc}")
                problems += 1
                continue
            declared = 0
            for group, value in (doc.get("groups") or {}).items():
                if not isinstance(value, dict) or "repos" not in value:
                    print(f"{name} #{index}: group {group!r} has no `repos:` key (schema-2 nesting)")
                    problems += 1
                    continue
                declared += len(value["repos"] or {})
            workdir = tempfile.mkdtemp()
            os.makedirs(f"{workdir}/.hydra")
            pathlib.Path(f"{workdir}/.hydra/config.yaml").write_text(block)
            env = dict(os.environ, HYDRA_CONFIG_DIR=f"{workdir}/cfg")
            result = subprocess.run(
                [str(hydra), "repo", "list", "--output", "json"],
                cwd=workdir, capture_output=True, text=True, env=env,
            )
            try:
                seen = len(json.loads(result.stdout)["data"]["repos"])
            except (ValueError, KeyError):
                print(f"{name} #{index}: hydra could not load it: {result.stdout[:160]}")
                problems += 1
                continue
            if seen != declared:
                print(f"{name} #{index}: declares {declared} repo(s), hydra sees {seen}")
                problems += 1
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
