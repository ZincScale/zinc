"""zinc.toml project loader.

Stub — driven by build/run/test commands as they come online.
"""
from __future__ import annotations

import tomllib
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class Project:
    root: Path
    name: str
    version: str
    main: str
    go_version: str
    deps: dict[str, str] = field(default_factory=dict)
    replace: dict[str, str] = field(default_factory=dict)


def load(root: Path) -> Project:
    toml_path = root / "zinc.toml"
    if not toml_path.exists():
        raise FileNotFoundError(f"no zinc.toml at {root}")
    with toml_path.open("rb") as fh:
        data = tomllib.load(fh)
    proj = data.get("project", {})
    go = data.get("go", {})
    return Project(
        root=root,
        name=proj.get("name", root.name),
        version=proj.get("version", "0.0.0"),
        main=proj.get("main", "main.zn"),
        go_version=go.get("version", "1.26"),
        deps=data.get("deps", {}),
        replace=data.get("replace", {}),
    )
