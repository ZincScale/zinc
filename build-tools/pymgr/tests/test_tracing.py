from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

from pymgr.cli import run
from pymgr.tracing import query_callers, query_uses, run_trace, trace_report
from pymgr.workspace import Workspace, WorkspaceError


def _trace_project(project: Path) -> Path:
    package = project / "src" / "acme"
    (package / "__init__.py").write_text("", encoding="utf-8")
    workload = package / "workload.py"
    workload.write_text(
        """import fractions
import json


def inner(value):
    if value == 1:
        try:
            raise ValueError("not recorded")
        except ValueError:
            pass
    return value * 2


def outer():
    total = 0
    for value in range(3):
        total += inner(value)
    return total


assert outer() == 6
assert json.loads("{}") == {}
assert fractions.Fraction(1, 2).numerator == 1
""",
        encoding="utf-8",
    )
    return workload


@pytest.mark.skipif(not hasattr(sys, "monitoring"), reason="requires Python 3.12+")
def test_trace_records_calls_exceptions_imports_and_targeted_loops(
    project: Path,
) -> None:
    workload = _trace_project(project)

    result = run_trace(
        Workspace(project),
        [sys.executable, str(workload)],
        [f"{workload}:16"],
    )

    assert result.returncode == 0
    assert (project / result.path).is_file()
    assert b"not recorded" not in (project / result.path).read_bytes()
    report = trace_report(Workspace(project), result.path)
    functions = {item["symbol"]: item for item in report["functions"]}
    assert functions["acme.workload.inner"]["calls"] == 3
    assert functions["acme.workload.outer"]["calls"] == 1
    assert any(
        edge["caller"] == "acme.workload.outer"
        and edge["callee"] == "acme.workload.inner"
        and edge["calls"] == 3
        for edge in report["edges"]
    )
    assert any(
        item["symbol"] == "acme.workload.inner"
        and item["type"] == "ValueError"
        and item["event"] == "handled"
        for item in report["exceptions"]
    )
    assert any(item["module"] == "fractions" for item in report["imports"])
    assert report["loops"][0]["entries"] == 1
    assert report["loops"][0]["header_hits"] >= 3
    assert report["loops"][0]["total_ns"] > 0
    assert report["metadata"]["python_version"] == sys.version.split()[0]


@pytest.mark.skipif(not hasattr(sys, "monitoring"), reason="requires Python 3.12+")
def test_uses_and_callers_keep_static_and_observed_evidence_separate(
    project: Path,
) -> None:
    workload = _trace_project(project)
    package = project / "src" / "acme"
    (package / "consumer.py").write_text(
        "from acme.workload import inner\n", encoding="utf-8"
    )
    traced = run_trace(Workspace(project), [sys.executable, str(workload)])

    uses = query_uses(Workspace(project), "acme.workload.inner", traced.path)
    callers = query_callers(Workspace(project), "acme.workload.inner", traced.path)

    assert uses["staticReferences"][0]["module"] == "acme.consumer"
    assert uses["staticReferences"][0]["evidence"] == "static-import"
    assert uses["observedCalls"][0] == {
        "caller": "acme.workload.outer",
        "callee": "acme.workload.inner",
        "calls": 3,
    }
    assert callers["observedCallers"] == uses["observedCalls"]
    assert "did not execute" in uses["absenceWarning"]


def test_trace_rejects_non_python_commands(project: Path) -> None:
    with pytest.raises(WorkspaceError, match="Python interpreter or Python console"):
        run_trace(Workspace(project), ["echo", "hello"])


@pytest.mark.skipif(not hasattr(sys, "monitoring"), reason="requires Python 3.12+")
def test_trace_cli_reports_latest_trace_as_json(project: Path, capsys) -> None:
    workload = _trace_project(project)

    assert (
        run(
            [
                "--root",
                str(project),
                "--json",
                "trace",
                "--",
                sys.executable,
                str(workload),
            ]
        )
        == 0
    )
    traced = json.loads(capsys.readouterr().out)
    assert traced["returncode"] == 0

    assert run(["--root", str(project), "--json", "trace", "report"]) == 0
    report = json.loads(capsys.readouterr().out)
    assert report["path"] == traced["path"]
    assert report["functions"]
