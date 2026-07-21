from __future__ import annotations

import json
from pathlib import Path

from pymgr.analysis import ProjectIndex
from pymgr.workspace import Workspace


def test_module_import_export_and_cycle_analysis(project: Path) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text(
        'from .client import Client as Client\n\n__all__ = ["Client"]\n',
        encoding="utf-8",
    )
    (package / "client.py").write_text(
        "from acme.models import Model\n\nclass Client:\n    pass\n",
        encoding="utf-8",
    )
    (package / "models.py").write_text(
        "from acme.client import Client\n\nclass Model:\n    pass\n",
        encoding="utf-8",
    )

    index = ProjectIndex(Workspace(project)).build()

    assert sorted(index.modules) == ["acme", "acme.client", "acme.models"]
    assert index.modules["acme"].exports == ["Client"]
    assert index.modules["acme"].export_origins["Client"] == "acme.client.Client"
    assert index.graph()["acme.client"] == {"acme.models"}
    assert index.importers("acme.client")[0][0] in {"acme", "acme.models"}
    assert index.cycles() == [["acme.client", "acme.models"]]
    assert any(issue.code == "import-cycle" for issue in index.issues)


def test_type_only_dynamic_import_and_sys_path_are_classified(project: Path) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    (package / "plugin.py").write_text(
        """import importlib
import sys
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from acme.types import Model

sys.path.append("vendor")
plugin = importlib.import_module("plugins.active")
""",
        encoding="utf-8",
    )
    (package / "types.py").write_text("class Model:\n    pass\n", encoding="utf-8")

    index = ProjectIndex(Workspace(project)).build()
    module = index.modules["acme.plugin"]

    assert any(
        reference.type_only and reference.module == "acme.types"
        for reference in module.imports
    )
    assert any(
        reference.dynamic and reference.module == "plugins.active"
        for reference in module.imports
    )
    assert module.mutates_sys_path
    assert {issue.code for issue in index.issues} >= {"dynamic-import", "sys-path"}


def test_api_snapshot_and_diff(project: Path) -> None:
    package = project / "src" / "acme"
    init = package / "__init__.py"
    init.write_text(
        'from .api import run as run\n__all__ = ["run"]\n', encoding="utf-8"
    )
    (package / "api.py").write_text(
        "def run(value: int) -> str:\n    return str(value)\n", encoding="utf-8"
    )
    workspace = Workspace(project)
    workspace.state_dir.mkdir()

    index = ProjectIndex(workspace).build()
    snapshot = index.write_api_snapshot()
    payload = json.loads(snapshot.read_text(encoding="utf-8"))
    assert (
        payload["modules"]["acme"]["symbols"]["run"]["signature"]
        == "def run(value: int) -> str"
    )

    init.write_text("__all__ = []\n", encoding="utf-8")
    difference = ProjectIndex(workspace).build().api_diff()
    assert difference["removed"] == ["acme.run"]
    assert difference["changed"] == []


def test_regular_package_and_public_api_policy_diagnostics(project: Path) -> None:
    package = project / "src" / "acme"
    (package / "module.py").write_text(
        "from somewhere import *\nfrom somewhere import _private\n",
        encoding="utf-8",
    )
    (package / "api.py").write_text(
        "__all__ = ['missing', 'missing']\n", encoding="utf-8"
    )

    index = ProjectIndex(Workspace(project)).build()

    codes = {issue.code for issue in index.issues}
    assert codes >= {
        "missing-init",
        "star-import",
        "private-import",
        "duplicate-export",
        "missing-export",
    }


def test_uv_workspace_ownership_and_dependency_boundaries(tmp_path: Path) -> None:
    (tmp_path / "pyproject.toml").write_text(
        "[tool.uv.workspace]\nmembers = ['packages/*']\n",
        encoding="utf-8",
    )
    service = tmp_path / "packages" / "service"
    library = tmp_path / "packages" / "library"
    (service / "src" / "service").mkdir(parents=True)
    (library / "src" / "library").mkdir(parents=True)
    (service / "pyproject.toml").write_text(
        "[project]\nname = 'service-dist'\nversion = '0.1.0'\ndependencies = []\n",
        encoding="utf-8",
    )
    (library / "pyproject.toml").write_text(
        "[project]\nname = 'library-dist'\nversion = '0.1.0'\n",
        encoding="utf-8",
    )
    (service / "src" / "service" / "__init__.py").write_text("", encoding="utf-8")
    (service / "src" / "service" / "main.py").write_text(
        "from library.internal import Thing\n",
        encoding="utf-8",
    )
    (library / "src" / "library" / "__init__.py").write_text(
        "from .internal import Thing as Thing\n__all__ = ['Thing']\n",
        encoding="utf-8",
    )
    (library / "src" / "library" / "internal.py").write_text(
        "class Thing:\n    pass\n",
        encoding="utf-8",
    )

    index = ProjectIndex(Workspace(tmp_path)).build()

    assert index.modules["service.main"].owner == "service-dist"
    assert index.modules["library.internal"].owner == "library-dist"
    codes = {issue.code for issue in index.issues}
    assert "undeclared-workspace-dependency" in codes
    assert "workspace-private-import" in codes
