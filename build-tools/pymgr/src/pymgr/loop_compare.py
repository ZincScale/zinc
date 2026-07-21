from __future__ import annotations

import ast
import hashlib
import json
import platform
import shutil
import statistics
import subprocess
import sys
import tempfile
import textwrap
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Sequence

from pymgr.loops import LoopAnalyzer, LoopFinding, _statement_contexts
from pymgr.workspace import Workspace, WorkspaceError


@dataclass(frozen=True)
class CandidateMeasurement:
    name: str
    source_sha256: str
    timings_seconds: tuple[float, ...]
    mean_seconds: float
    median_seconds: float
    stdev_seconds: float
    min_seconds: float
    max_seconds: float


@dataclass(frozen=True)
class LoopComparison:
    location: str
    construct: str
    recommendation: str
    command: tuple[str, ...]
    warmups: int
    runs: int
    timeout_seconds: float
    measured_at: str
    interpreter: dict[str, str]
    workload_id: str
    candidates: tuple[CandidateMeasurement, ...]
    ranking: str
    speedup: float | None
    report: str
    caveat: str = (
        "Measures the complete child-process workload, including startup; "
        "it does not predict production performance."
    )


def compare_loop(
    workspace: Workspace,
    location: str,
    command: Sequence[str],
    *,
    warmups: int = 1,
    runs: int = 5,
    timeout_seconds: float = 60.0,
) -> LoopComparison:
    if warmups < 0:
        raise WorkspaceError("loop comparison warmups cannot be negative")
    if runs < 2:
        raise WorkspaceError("loop comparison requires at least two measured runs")
    if timeout_seconds <= 0:
        raise WorkspaceError("loop comparison timeout must be positive")
    workload = tuple(command[1:] if command and command[0] == "--" else command)
    if not workload:
        raise WorkspaceError("loop compare requires a workload command after --")

    finding = LoopAnalyzer(workspace).explain(location)
    if not finding.suggested_code:
        raise WorkspaceError(
            f"{finding.location} has no safely generated comparison candidate"
        )
    source_path = workspace.root / finding.path
    original = source_path.read_text(encoding="utf-8")
    suggested = _apply_suggestion(original, finding)
    original_hash = _digest(original.encode())
    suggested_hash = _digest(suggested.encode())
    workload_id = _digest(
        json.dumps(
            {
                "location": finding.location,
                "command": workload,
                "original": original_hash,
                "suggested": suggested_hash,
            },
            sort_keys=True,
        ).encode()
    )[:16]

    samples: dict[str, list[float]] = {"original": [], "suggested": []}
    with tempfile.TemporaryDirectory(prefix="pymgr-loop-compare-") as temporary:
        temporary_root = Path(temporary)
        templates = {
            "original": temporary_root / "original-template",
            "suggested": temporary_root / "suggested-template",
        }
        for name, template in templates.items():
            _copy_workspace(workspace.root, template)
            if name == "suggested":
                candidate_path = template / finding.path
                candidate_path.write_text(suggested, encoding="utf-8")

        sequence = ("original", "suggested")
        for warmup in range(warmups):
            for name in sequence[warmup % 2 :] + sequence[: warmup % 2]:
                _run_candidate(
                    templates[name],
                    temporary_root,
                    name,
                    workload,
                    timeout_seconds,
                    measured=False,
                )
        for iteration in range(runs):
            order = sequence if iteration % 2 == 0 else tuple(reversed(sequence))
            for name in order:
                duration = _run_candidate(
                    templates[name],
                    temporary_root,
                    name,
                    workload,
                    timeout_seconds,
                    measured=True,
                )
                samples[name].append(duration)

    candidates = (
        _measurement("original", original_hash, samples["original"]),
        _measurement("suggested", suggested_hash, samples["suggested"]),
    )
    ranking, speedup = _rank(candidates)
    measured_at = datetime.now(timezone.utc).isoformat()
    report_path = _report_path(workspace, workload_id, measured_at)
    comparison = LoopComparison(
        location=finding.location,
        construct=finding.construct,
        recommendation=finding.recommendation,
        command=workload,
        warmups=warmups,
        runs=runs,
        timeout_seconds=timeout_seconds,
        measured_at=measured_at,
        interpreter=_interpreter_identity(workload),
        workload_id=workload_id,
        candidates=candidates,
        ranking=ranking,
        speedup=speedup,
        report=str(report_path.relative_to(workspace.root)),
    )
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(
        json.dumps(asdict(comparison), indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    return comparison


def _apply_suggestion(source: str, finding: LoopFinding) -> str:
    tree = ast.parse(source, filename=finding.path)
    contexts = _statement_contexts(tree)
    nodes = [
        node
        for node in ast.walk(tree)
        if isinstance(node, ast.For) and node.lineno == finding.line
    ]
    if len(nodes) != 1:
        raise WorkspaceError(
            f"cannot identify one replaceable loop at {finding.location}"
        )
    node = nodes[0]
    siblings, index = contexts.get(id(node), ([], -1))
    previous = siblings[index - 1] if index > 0 else None
    if not isinstance(previous, ast.Assign):
        raise WorkspaceError(
            f"{finding.location} suggestion is explanatory and cannot be benchmarked safely"
        )
    if "..." in finding.suggested_code:
        raise WorkspaceError(
            f"{finding.location} suggestion is incomplete and cannot be benchmarked"
        )

    lines = source.splitlines(keepends=True)
    start = previous.lineno - 1
    end = node.end_lineno or node.lineno
    indentation = lines[node.lineno - 1][: node.col_offset]
    replacement = textwrap.indent(finding.suggested_code, indentation) + "\n"
    result = "".join(lines[:start]) + replacement + "".join(lines[end:])
    try:
        ast.parse(result, filename=finding.path)
    except SyntaxError as error:
        raise WorkspaceError(
            f"generated loop candidate is not valid Python: {error}"
        ) from error
    return result


def _copy_workspace(source: Path, destination: Path) -> None:
    ignored = shutil.ignore_patterns(".git", ".pymgr", ".venv", "__pycache__", "*.pyc")
    try:
        shutil.copytree(source, destination, symlinks=True, ignore=ignored)
    except OSError as error:
        raise WorkspaceError(
            f"cannot create isolated comparison workspace: {error}"
        ) from error


def _run_candidate(
    template: Path,
    temporary_root: Path,
    name: str,
    command: tuple[str, ...],
    timeout_seconds: float,
    *,
    measured: bool,
) -> float:
    with tempfile.TemporaryDirectory(prefix=f"{name}-", dir=temporary_root) as run:
        run_root = Path(run) / "workspace"
        shutil.copytree(template, run_root, symlinks=True)
        started = time.perf_counter_ns()
        try:
            result = subprocess.run(
                command,
                cwd=run_root,
                check=False,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                timeout=timeout_seconds,
            )
            finished = time.perf_counter_ns()
        except FileNotFoundError as error:
            raise WorkspaceError(
                f"comparison command not found: {command[0]}"
            ) from error
        except subprocess.TimeoutExpired as error:
            raise WorkspaceError(
                f"{name} comparison command timed out after {timeout_seconds:g} seconds"
            ) from error
    duration = (finished - started) / 1_000_000_000
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip() or "no output"
        stage = "measured run" if measured else "warm-up"
        raise WorkspaceError(
            f"{name} candidate failed its {stage} equivalence command "
            f"with exit status {result.returncode}: {detail}"
        )
    return duration


def _measurement(
    name: str, source_hash: str, samples: Sequence[float]
) -> CandidateMeasurement:
    return CandidateMeasurement(
        name=name,
        source_sha256=source_hash,
        timings_seconds=tuple(samples),
        mean_seconds=statistics.fmean(samples),
        median_seconds=statistics.median(samples),
        stdev_seconds=statistics.stdev(samples),
        min_seconds=min(samples),
        max_seconds=max(samples),
    )


def _rank(
    candidates: tuple[CandidateMeasurement, CandidateMeasurement],
) -> tuple[str, float | None]:
    original, suggested = candidates
    slower = max(original.median_seconds, suggested.median_seconds)
    faster = min(original.median_seconds, suggested.median_seconds)
    speedup = slower / faster if faster else None
    difference = abs(original.median_seconds - suggested.median_seconds)
    uncertainty = max(
        original.stdev_seconds,
        suggested.stdev_seconds,
        slower * 0.01,
    )
    if difference <= uncertainty:
        return "inconclusive", speedup
    winner = (
        "original"
        if original.median_seconds < suggested.median_seconds
        else "suggested"
    )
    return winner, speedup


def _interpreter_identity(command: Sequence[str]) -> dict[str, str]:
    executable = shutil.which(command[0]) or command[0]
    identity = {
        "controller_executable": sys.executable,
        "controller_version": platform.python_version(),
        "controller_implementation": platform.python_implementation(),
        "controller_compiler": platform.python_compiler(),
        "workload_executable": executable,
    }
    name = Path(executable).name.lower()
    if name.startswith(("python", "pypy")):
        script = (
            "import json,platform,sys;"
            "print(json.dumps({'version':platform.python_version(),"
            "'implementation':platform.python_implementation(),"
            "'compiler':platform.python_compiler(),"
            "'executable':sys.executable}))"
        )
        try:
            result = subprocess.run(
                [executable, "-c", script],
                check=False,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                timeout=5,
            )
            if result.returncode == 0:
                observed = json.loads(result.stdout)
                identity.update(
                    {f"workload_{key}": str(value) for key, value in observed.items()}
                )
        except (OSError, subprocess.TimeoutExpired, json.JSONDecodeError):
            pass
    return identity


def _report_path(workspace: Workspace, workload_id: str, measured_at: str) -> Path:
    timestamp = measured_at.replace(":", "-").replace("+", "_")
    return workspace.state_dir / "loop-comparisons" / f"{timestamp}-{workload_id}.json"


def _digest(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()
