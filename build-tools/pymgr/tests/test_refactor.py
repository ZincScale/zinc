from __future__ import annotations

from pathlib import Path

import pytest

from pymgr.analysis import ProjectIndex
from pymgr.refactor import (
    apply_plan,
    plan_module_move,
    plan_symbol_rename,
    run_import_tool,
    update_export,
)
from pymgr.workspace import Workspace, WorkspaceError


def _index(project: Path) -> ProjectIndex:
    return ProjectIndex(Workspace(project)).build()


def test_move_is_preview_then_updates_static_imports_and_preserves_comments(
    project: Path,
) -> None:
    package = project / "src" / "acme"
    public = package / "__init__.py"
    public.write_text(
        "from .old import Model as Model\n__all__ = ['Model']\n", encoding="utf-8"
    )
    old = package / "old.py"
    old.write_text("# model comment\nclass Model:\n    pass\n", encoding="utf-8")
    consumer = package / "service.py"
    consumer.write_text(
        "from acme.old import Model  # keep this comment\n\ndef build():\n    return Model()\n",
        encoding="utf-8",
    )

    plan = plan_module_move(_index(project), "acme.old", "acme.models")

    assert old.exists()
    assert not (package / "models.py").exists()
    assert any(change.path == consumer for change in plan.changes)

    apply_plan(plan)

    assert not old.exists()
    assert (
        (package / "models.py")
        .read_text(encoding="utf-8")
        .startswith("# model comment")
    )
    changed = consumer.read_text(encoding="utf-8")
    assert "from acme.models import Model" in changed
    assert "# keep this comment" in changed
    assert "from .models import Model as Model" in public.read_text(encoding="utf-8")


def test_qualified_symbol_rename_updates_definition_import_and_usage(
    project: Path,
) -> None:
    package = project / "src" / "acme"
    public = package / "__init__.py"
    public.write_text(
        "from .api import load as load\n__all__ = ['load']\n", encoding="utf-8"
    )
    api = package / "api.py"
    api.write_text("def load(value):\n    return value\n", encoding="utf-8")
    use = package / "use.py"
    use.write_text("from acme.api import load\n\nresult = load(1)\n", encoding="utf-8")

    plan = plan_symbol_rename(_index(project), "acme.api.load", "fetch")
    apply_plan(plan)

    assert "def fetch" in api.read_text(encoding="utf-8")
    changed = use.read_text(encoding="utf-8")
    assert "from acme.api import fetch" in changed
    assert "fetch(1)" in changed
    exported = public.read_text(encoding="utf-8")
    assert "from .api import fetch as fetch" in exported
    assert "__all__ = ['fetch']" in exported


def test_export_management_uses_explicit_reexport_form(project: Path) -> None:
    package = project / "src" / "acme"
    init = package / "__init__.py"
    init.write_text('"""Public API."""\n', encoding="utf-8")
    (package / "client.py").write_text("class Client:\n    pass\n", encoding="utf-8")

    changed = update_export(init, "acme", "Client", "acme.client", True)

    assert changed
    source = init.read_text(encoding="utf-8")
    assert "from .client import Client as Client" in source
    assert "__all__ = ['Client']" in source
    assert _index(project).modules["acme"].exports == ["Client"]


def test_module_can_export_its_own_definition(project: Path) -> None:
    module = project / "src" / "acme" / "service.py"
    module.write_text("def run():\n    return 1\n", encoding="utf-8")

    changed = update_export(module, "acme.service", "run", None, True)

    assert changed
    assert "__all__ = ['run']" in module.read_text(encoding="utf-8")
    assert _index(project).modules["acme.service"].exports == ["run"]


def test_unexport_removes_managed_reexport_import(project: Path) -> None:
    package = project / "src" / "acme"
    initializer = package / "__init__.py"
    initializer.write_text(
        "from .client import Client as Client\n\n__all__ = ['Client']\n",
        encoding="utf-8",
    )

    changed = update_export(initializer, "acme", "Client", None, False)

    assert changed
    source = initializer.read_text(encoding="utf-8")
    assert "from .client" not in source
    assert "__all__ = []" in source


def test_package_can_export_a_submodule(project: Path) -> None:
    package = project / "src" / "acme"
    initializer = package / "__init__.py"
    initializer.write_text(
        "def main():\n    return 1\n\n__all__ = []\n", encoding="utf-8"
    )
    (package / "billing.py").write_text("__all__ = []\n", encoding="utf-8")

    changed = update_export(
        initializer,
        "acme",
        "billing",
        "acme.billing",
        True,
        export_module=True,
    )

    assert changed
    source = initializer.read_text(encoding="utf-8")
    assert "from . import billing as billing" in source
    assert source.index("from . import") < source.index("def main")
    assert "__all__ = ['billing']" in source
    assert _index(project).modules["acme"].export_origins["billing"] == "acme.billing"


def test_cross_package_move_rewrites_relative_imports(project: Path) -> None:
    source_root = project / "src"
    package = source_root / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    (package / "shared.py").write_text(
        "def helper():\n    return 1\n", encoding="utf-8"
    )
    worker = package / "worker.py"
    worker.write_text(
        "from .shared import helper\n\ndef run():\n    return helper()\n",
        encoding="utf-8",
    )

    plan = plan_module_move(_index(project), "acme.worker", "other.worker")
    apply_plan(plan)

    moved = source_root / "other" / "worker.py"
    assert moved.exists()
    assert "from acme.shared import helper" in moved.read_text(encoding="utf-8")
    assert (source_root / "other" / "__init__.py").exists()


def test_package_directory_move_updates_descendants_and_importers(
    project: Path,
) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    legacy = package / "legacy"
    legacy.mkdir()
    (legacy / "__init__.py").write_text(
        "from .models import Model as Model\n__all__ = ['Model']\n",
        encoding="utf-8",
    )
    (legacy / "models.py").write_text("class Model:\n    pass\n", encoding="utf-8")
    consumer = package / "use.py"
    consumer.write_text("from acme.legacy.models import Model\n", encoding="utf-8")

    plan = plan_module_move(_index(project), "acme.legacy", "acme.modern")
    assert plan.move_is_directory
    apply_plan(plan)

    assert not legacy.exists()
    modern = package / "modern"
    assert (modern / "models.py").exists()
    assert "from .models import Model as Model" in (modern / "__init__.py").read_text(
        encoding="utf-8"
    )
    assert "from acme.modern.models import Model" in consumer.read_text(
        encoding="utf-8"
    )


def test_import_organization_is_preview_first_and_delegates_to_ruff(
    project: Path,
) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    module = package / "order.py"
    original = "import sys\nimport os\n\nprint(os.name, sys.version)\n"
    module.write_text(original, encoding="utf-8")
    workspace = Workspace(project)

    preview = run_import_tool(workspace, "organize", [str(module)], False)

    assert not preview.applied
    assert preview.output
    assert module.read_text(encoding="utf-8") == original

    applied = run_import_tool(workspace, "organize", [str(module)], True)

    assert applied.files == (module,)
    assert module.read_text(encoding="utf-8").startswith("import os\nimport sys\n")


def test_package_move_refuses_destination_inside_source(project: Path) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    legacy = package / "legacy"
    legacy.mkdir()
    (legacy / "__init__.py").write_text("", encoding="utf-8")

    with pytest.raises(WorkspaceError, match="inside itself"):
        plan_module_move(_index(project), "acme.legacy", "acme.legacy.child")


def test_package_move_refuses_unindexed_python_source(project: Path) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    legacy = package / "legacy"
    legacy.mkdir()
    (legacy / "__init__.py").write_text("", encoding="utf-8")
    (legacy / "broken.py").write_text("def broken(:\n", encoding="utf-8")

    with pytest.raises(WorkspaceError, match="unindexed Python source"):
        plan_module_move(_index(project), "acme.legacy", "acme.modern")


def test_import_fix_applies_only_ruff_safe_fixes(project: Path) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    module = package / "unused.py"
    module.write_text("import os\n\nvalue = 1\n", encoding="utf-8")
    workspace = Workspace(project)

    preview = run_import_tool(workspace, "fix", [str(module)], False)
    assert "Would fix" in preview.output
    assert "import os" in module.read_text(encoding="utf-8")

    applied = run_import_tool(workspace, "fix", [str(module)], True)

    assert applied.files == (module,)
    assert "import os" not in module.read_text(encoding="utf-8")
