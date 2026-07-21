from __future__ import annotations

from pathlib import Path

from pymgr.analysis import ProjectIndex
from pymgr.refactor import (
    apply_plan,
    plan_module_move,
    plan_symbol_rename,
    update_export,
)
from pymgr.workspace import Workspace


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
