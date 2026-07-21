from __future__ import annotations

from pathlib import Path

import pytest


@pytest.fixture
def project(tmp_path: Path) -> Path:
    (tmp_path / "pyproject.toml").write_text(
        """[project]
name = "acme"
version = "0.1.0"

[tool.pymgr]
source-roots = ["src"]
""",
        encoding="utf-8",
    )
    (tmp_path / "src" / "acme").mkdir(parents=True)
    return tmp_path
