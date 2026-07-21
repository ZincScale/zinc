from __future__ import annotations

from pathlib import Path

import pytest

from pymgr.analysis import ProjectIndex
from pymgr.scaffold import create_module
from pymgr.workspace import Workspace, WorkspaceError


def test_create_module_builds_missing_regular_package_chain(project: Path) -> None:
    workspace = Workspace(project)

    result = create_module(workspace, "acme.services.billing")

    assert result.kind == "module"
    assert result.path == project / "src" / "acme" / "services" / "billing.py"
    assert (project / "src" / "acme" / "__init__.py").read_text(
        encoding="utf-8"
    ) == "__all__ = []\n"
    assert (project / "src" / "acme" / "services" / "__init__.py").read_text(
        encoding="utf-8"
    ) == "__all__ = []\n"
    assert result.path.read_text(encoding="utf-8") == "__all__ = []\n"
    assert "acme.services.billing" in ProjectIndex(workspace).build().modules


def test_create_package_builds_initializer_as_target(project: Path) -> None:
    result = create_module(Workspace(project), "acme.integrations.stripe", package=True)

    assert result.kind == "package"
    assert result.path == (
        project / "src" / "acme" / "integrations" / "stripe" / "__init__.py"
    )
    assert result.path.read_text(encoding="utf-8") == "__all__ = []\n"


def test_create_module_rejects_invalid_names_and_conflicts(project: Path) -> None:
    workspace = Workspace(project)

    with pytest.raises(WorkspaceError, match="Python identifiers"):
        create_module(workspace, "acme.bad-name")

    create_module(workspace, "acme.service")
    with pytest.raises(WorkspaceError, match="already exists"):
        create_module(workspace, "acme.service")
    with pytest.raises(WorkspaceError, match="existing module"):
        create_module(workspace, "acme.service", package=True)


def test_create_submodule_rejects_module_as_parent(project: Path) -> None:
    (project / "src" / "tools.py").write_text("__all__ = []\n", encoding="utf-8")

    with pytest.raises(WorkspaceError, match="package path conflicts"):
        create_module(Workspace(project), "tools.formatters")


def test_multiple_source_roots_require_an_explicit_choice(tmp_path: Path) -> None:
    (tmp_path / "pyproject.toml").write_text(
        """[project]
name = "acme"
version = "0.1.0"

[tool.pymgr]
source-roots = ["src", "generated"]
""",
        encoding="utf-8",
    )
    workspace = Workspace(tmp_path)

    with pytest.raises(WorkspaceError, match="multiple source roots"):
        create_module(workspace, "acme.service")

    result = create_module(workspace, "acme.service", source_root="generated")
    assert result.path == tmp_path / "generated" / "acme" / "service.py"
