from __future__ import annotations

import json
from pathlib import Path

from pymgr.cli import run


def test_cli_lists_modules_and_applies_explicit_move(project: Path, capsys) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    source = package / "old.py"
    source.write_text("class Item:\n    pass\n", encoding="utf-8")
    consumer = package / "use.py"
    consumer.write_text("from acme.old import Item\n", encoding="utf-8")

    assert run(["--root", str(project), "--json", "modules"]) == 0
    payload = json.loads(capsys.readouterr().out)
    assert [item["name"] for item in payload["modules"]] == [
        "acme",
        "acme.old",
        "acme.use",
    ]

    assert run(["--root", str(project), "--json", "move", "acme.old", "acme.new"]) == 0
    preview = json.loads(capsys.readouterr().out)
    assert preview["applied"] is False
    assert source.exists()

    assert (
        run(
            [
                "--root",
                str(project),
                "--json",
                "move",
                "acme.old",
                "acme.new",
                "--apply",
            ]
        )
        == 0
    )
    applied = json.loads(capsys.readouterr().out)
    assert applied["applied"] is True
    assert not source.exists()
    assert (package / "new.py").exists()
    assert "from acme.new import Item" in consumer.read_text(encoding="utf-8")
