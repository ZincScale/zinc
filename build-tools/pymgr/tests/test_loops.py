from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

from pymgr.cli import run
from pymgr.loop_compare import compare_loop, comparison_report
from pymgr.loops import LoopAnalyzer
from pymgr.workspace import Workspace, WorkspaceError


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


def _comparison_project(project: Path) -> tuple[Path, Path]:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    module = package / "compare.py"
    module.write_text(
        """def doubled(items):
    result = []
    for item in items:
        result.append(item * 2)
    return result
""",
        encoding="utf-8",
    )
    check = project / "check.py"
    check.write_text(
        """import sys
sys.path.insert(0, "src")
from acme.compare import doubled

assert doubled(range(100)) == [item * 2 for item in range(100)]
""",
        encoding="utf-8",
    )
    return module, check


def test_loop_compare_isolates_candidates_and_persists_reproducible_report(
    project: Path,
) -> None:
    module, check = _comparison_project(project)
    original = module.read_text(encoding="utf-8")

    comparison = compare_loop(
        Workspace(project),
        f"{module}:3",
        [sys.executable, check.name],
        warmups=0,
        runs=2,
        timeout_seconds=5,
    )

    assert module.read_text(encoding="utf-8") == original
    assert comparison.location == "src/acme/compare.py:3"
    assert comparison.command == (sys.executable, "check.py")
    assert comparison.ranking in {"original", "suggested", "inconclusive"}
    assert comparison.speedup is not None
    assert {candidate.name for candidate in comparison.candidates} == {
        "original",
        "suggested",
    }
    assert all(
        len(candidate.timings_seconds) == 2
        and candidate.min_seconds > 0
        and candidate.source_sha256
        for candidate in comparison.candidates
    )
    report = project / comparison.report
    payload = json.loads(report.read_text(encoding="utf-8"))
    assert payload["workload_id"] == comparison.workload_id
    assert payload["runs"] == 2
    assert payload["interpreter"]["controller_version"]
    assert comparison_report(Workspace(project))["path"] == comparison.report


def test_loop_compare_cli_emits_json_measurements(project: Path, capsys) -> None:
    module, check = _comparison_project(project)

    assert (
        run(
            [
                "--root",
                str(project),
                "--json",
                "loop",
                "compare",
                "--warmups",
                "0",
                "--runs",
                "2",
                f"{module}:3",
                "--",
                sys.executable,
                check.name,
            ]
        )
        == 0
    )
    payload = json.loads(capsys.readouterr().out)

    assert payload["ranking"] in {"original", "suggested", "inconclusive"}
    assert len(payload["candidates"]) == 2
    assert payload["command"] == [sys.executable, "check.py"]


def test_loop_compare_rejects_a_candidate_that_fails_the_equivalence_command(
    project: Path,
) -> None:
    module, check = _comparison_project(project)
    check.write_text(
        """from pathlib import Path

source = Path("src/acme/compare.py").read_text(encoding="utf-8")
raise SystemExit(0 if "result.append" in source else 7)
""",
        encoding="utf-8",
    )

    with pytest.raises(WorkspaceError, match="suggested candidate failed"):
        compare_loop(
            Workspace(project),
            f"{module}:3",
            [sys.executable, check.name],
            warmups=0,
            runs=2,
            timeout_seconds=5,
        )

    assert not (project / ".pymgr" / "loop-comparisons").exists()


def test_declared_dataframe_dependency_enables_conservative_adapter(
    project: Path,
) -> None:
    (project / "pyproject.toml").write_text(
        """[project]
name = "acme"
version = "0.1.0"
dependencies = ["pandas>=2"]

[tool.pymgr]
source-roots = ["src"]
""",
        encoding="utf-8",
    )
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    module = package / "table.py"
    module.write_text(
        """def show(frame):
    for index, row in frame.iterrows():
        print(index, row.value)
""",
        encoding="utf-8",
    )

    finding = LoopAnalyzer(Workspace(project)).explain(f"{module}:2")

    assert finding.construct == "pandas DataFrame.iterrows"
    assert finding.evidence == "heuristic"
    assert "vectorized pandas" in finding.recommendation
    assert "itertuples" in finding.alternatives[0]
    assert finding.performance == "unmeasured"
