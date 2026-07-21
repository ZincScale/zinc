from __future__ import annotations

import subprocess
from pathlib import Path

from pymgr.workspace import Workspace


def test_real_uv_sync_and_direct_python_execution(tmp_path: Path) -> None:
    (tmp_path / "pyproject.toml").write_text(
        """[project]
name = "pymgr-fixture"
version = "0.1.0"
requires-python = ">=3.12"

[build-system]
requires = ["uv_build>=0.11,<0.12"]
build-backend = "uv_build"

[tool.pymgr]
source-roots = ["src"]
""",
        encoding="utf-8",
    )
    package = tmp_path / "src" / "pymgr_fixture"
    package.mkdir(parents=True)
    (package / "__init__.py").write_text("", encoding="utf-8")
    (package / "__main__.py").write_text(
        "print('ordinary python still works')\n", encoding="utf-8"
    )

    workspace = Workspace(tmp_path)
    workspace.init()
    state = workspace.mutate("sync")
    checked = workspace.mutate("check")

    assert state.status == "ready"
    assert checked.generation == state.generation + 1
    assert workspace.doctor() == []
    resolved = workspace.probe_module("json")
    assert resolved["stdlib"] is True
    assert resolved["origin"].endswith("json/__init__.py")
    result = subprocess.run(
        [checked.python, "-m", "pymgr_fixture"],
        cwd=tmp_path,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
    )
    assert result.stdout.strip() == "ordinary python still works"
