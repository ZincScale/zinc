from __future__ import annotations

import json
from pathlib import Path

from pymgr.cli import run
from pymgr.loops import LoopAnalyzer
from pymgr.workspace import Workspace


def _loop_project(project: Path) -> Path:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    module = package / "work.py"
    module.write_text(
        """def analyze(items):
    names = []
    for item in items:
        if item.active:
            names.append(item.name)

    total = 0
    for value in items:
        total += value.cost

    for index in range(len(items)):
        print(index, items[index])

    while items:
        items.pop()

    eager = any([candidate.active for candidate in items])
    return names, total, eager
""",
        encoding="utf-8",
    )
    return module


def test_loop_analyzer_classifies_constructs_without_speed_claims(
    project: Path,
) -> None:
    module = _loop_project(project)

    findings = LoopAnalyzer(Workspace(project)).scan([str(module)])
    by_line = {finding.line: finding for finding in findings}

    assert "list comprehension" in by_line[3].recommendation
    assert by_line[3].evidence == "semantic"
    assert by_line[3].suggested_code == (
        "names = [item.name for item in items if item.active]"
    )
    assert by_line[8].intent == "reduction"
    assert by_line[8].evidence == "heuristic"
    assert by_line[8].suggested_code == ("total = sum(value.cost for value in items)")
    assert "enumerate" in by_line[11].recommendation
    assert by_line[14].construct == "while"
    assert any(
        finding.line == 17
        and finding.construct == "list comprehension"
        and "generator expression" in finding.recommendation
        for finding in findings
    )
    assert all(finding.performance == "unmeasured" for finding in findings)
    language = " ".join(
        f"{finding.recommendation} {finding.reason}" for finding in findings
    ).lower()
    assert "faster" not in language
    assert "fastest" not in language


def test_loop_explain_finds_the_smallest_construct_at_location(project: Path) -> None:
    module = _loop_project(project)

    finding = LoopAnalyzer(Workspace(project)).explain(f"{module}:4")

    assert finding.line == 3
    assert finding.construct == "for"


def test_loop_cli_emits_machine_readable_evidence(project: Path, capsys) -> None:
    module = _loop_project(project)

    assert run(["--root", str(project), "--json", "loops", str(module)]) == 0
    payload = json.loads(capsys.readouterr().out)

    assert payload
    assert all(item["performance"] == "unmeasured" for item in payload)
    assert all(item["evidence"] in {"semantic", "heuristic"} for item in payload)
    assert any(item["suggested_code"] for item in payload)


def test_set_accumulation_and_index_only_traversal_get_specific_advice(
    project: Path,
) -> None:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    module = package / "patterns.py"
    module.write_text(
        """def patterns(items):
    unique = set()
    for item in items:
        unique.add(item.name)

    for index in range(len(items)):
        print(items[index])
""",
        encoding="utf-8",
    )

    findings = LoopAnalyzer(Workspace(project)).scan([str(module)])
    by_line = {finding.line: finding for finding in findings}

    assert "set comprehension" in by_line[3].recommendation
    assert by_line[6].recommendation == "Iterate over the collection elements directly"
