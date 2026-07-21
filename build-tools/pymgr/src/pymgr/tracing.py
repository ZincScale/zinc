from __future__ import annotations

import hashlib
import json
import shlex
import shutil
import sqlite3
import subprocess
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Sequence

from pymgr.analysis import ProjectIndex
from pymgr.loops import LoopAnalyzer
from pymgr.workspace import Workspace, WorkspaceError


@dataclass(frozen=True)
class TraceRun:
    path: str
    command: tuple[str, ...]
    interpreter: str
    returncode: int


def run_trace(
    workspace: Workspace,
    command: Sequence[str],
    loop_locations: Sequence[str] = (),
) -> TraceRun:
    workload = tuple(command[1:] if command and command[0] == "--" else command)
    if not workload:
        raise WorkspaceError("trace requires a Python command after --")
    interpreter, mode, target, target_args = _resolve_python_command(workload)
    _require_monitoring(interpreter)
    loops = [LoopAnalyzer(workspace).explain(location) for location in loop_locations]
    identity = hashlib.sha256(
        json.dumps({"command": workload, "loops": list(loop_locations)}).encode()
    ).hexdigest()[:12]
    timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
    output = workspace.state_dir / "traces" / f"{timestamp}-{identity}.sqlite"
    agent = Path(__file__).with_name("trace_agent.py")
    child = [
        interpreter,
        str(agent),
        "--output",
        str(output),
        "--root",
        str(workspace.root),
    ]
    for source_root in workspace.source_roots():
        child.extend(["--source-root", str(source_root)])
    for finding in loops:
        path = workspace.root / finding.path
        child.extend(
            [
                "--loop",
                json.dumps([str(path.resolve()), finding.line, finding.end_line]),
            ]
        )
    child.extend(["--mode", mode, target, "--", *target_args])
    result = subprocess.run(child, cwd=workspace.root, check=False)
    if not output.exists():
        raise WorkspaceError("trace child exited without producing a report")
    return TraceRun(
        path=str(output.relative_to(workspace.root)),
        command=workload,
        interpreter=interpreter,
        returncode=result.returncode,
    )


def trace_report(workspace: Workspace, path: str | Path | None = None) -> dict:
    trace_path = _trace_path(workspace, path)
    with _open_trace(trace_path) as database:
        metadata = dict(database.execute("SELECT key, value FROM metadata"))
        functions = [
            dict(row)
            for row in database.execute(
                "SELECT symbol, file, line, calls, total_ns, returns "
                "FROM functions ORDER BY total_ns DESC, symbol"
            )
        ]
        edges = [
            dict(row)
            for row in database.execute(
                "SELECT caller, callee, calls FROM edges ORDER BY calls DESC, caller, callee"
            )
        ]
        exceptions = [
            dict(row)
            for row in database.execute(
                "SELECT symbol, type, event, count FROM exceptions "
                "ORDER BY count DESC, symbol, type, event"
            )
        ]
        imports = [
            dict(row)
            for row in database.execute(
                "SELECT module, count FROM imports ORDER BY count DESC, module"
            )
        ]
        loops = [
            dict(row)
            for row in database.execute(
                "SELECT location, entries, header_hits, total_ns FROM loops "
                "ORDER BY total_ns DESC, location"
            )
        ]
    return {
        "path": str(trace_path.relative_to(workspace.root)),
        "metadata": metadata,
        "functions": functions,
        "edges": edges,
        "exceptions": exceptions,
        "imports": imports,
        "loops": loops,
    }


def query_uses(
    workspace: Workspace,
    symbol: str,
    path: str | Path | None = None,
) -> dict:
    if "." not in symbol:
        raise WorkspaceError("uses requires a qualified module or symbol")
    index = ProjectIndex(workspace).build()
    module_name, member = symbol.rsplit(".", 1)
    static = []
    for module in index.modules.values():
        for reference in module.imports:
            if reference.module == symbol or (
                reference.module == module_name and member in reference.names
            ):
                static.append(
                    {
                        "module": module.name,
                        "path": str(module.path),
                        "line": reference.line,
                        "evidence": "static-import",
                    }
                )
    dynamic = [
        {
            "module": issue.module,
            "line": issue.line,
            "message": issue.message,
        }
        for issue in index.issues
        if issue.code == "dynamic-import"
    ]
    observed = []
    trace = _optional_trace_path(workspace, path)
    if trace:
        with _open_trace(trace) as database:
            observed = [
                dict(row)
                for row in database.execute(
                    "SELECT caller, callee, calls FROM edges WHERE callee = ? "
                    "ORDER BY calls DESC, caller",
                    (symbol,),
                )
            ]
    return {
        "symbol": symbol,
        "staticReferences": static,
        "observedCalls": observed,
        "unresolvedDynamicReferences": dynamic,
        "trace": str(trace.relative_to(workspace.root)) if trace else None,
        "absenceWarning": "An absent observed call only means the traced workload did not execute it.",
    }


def query_callers(
    workspace: Workspace,
    symbol: str,
    path: str | Path | None = None,
) -> dict:
    result = query_uses(workspace, symbol, path)
    return {
        "symbol": symbol,
        "staticCallers": result["staticReferences"],
        "observedCallers": result["observedCalls"],
        "trace": result["trace"],
        "absenceWarning": result["absenceWarning"],
    }


def _resolve_python_command(
    command: tuple[str, ...],
) -> tuple[str, str, str, tuple[str, ...]]:
    executable = shutil.which(command[0]) or command[0]
    name = Path(executable).name.lower()
    if name.startswith(("python", "pypy")):
        arguments = list(command[1:])
        if not arguments:
            raise WorkspaceError("trace cannot wrap an interactive interpreter")
        if arguments[0] == "-m" and len(arguments) >= 2:
            return executable, "module", arguments[1], tuple(arguments[2:])
        if arguments[0] == "-c" and len(arguments) >= 2:
            return executable, "code", arguments[1], tuple(arguments[2:])
        if arguments[0].startswith("-"):
            raise WorkspaceError(
                "trace supports python script.py, python -m, or python -c"
            )
        return executable, "script", arguments[0], tuple(arguments[1:])
    path = Path(executable)
    try:
        first_line = path.open(encoding="utf-8", errors="replace").readline().strip()
    except OSError as error:
        raise WorkspaceError(
            f"cannot inspect trace command {command[0]}: {error}"
        ) from error
    if not first_line.startswith("#!") or "python" not in first_line.lower():
        raise WorkspaceError(
            "trace command must be a Python interpreter or Python console script"
        )
    shebang = shlex.split(first_line[2:])
    if Path(shebang[0]).name == "env" and len(shebang) > 1:
        interpreter = shutil.which(shebang[1]) or shebang[1]
    else:
        interpreter = shebang[0]
    return interpreter, "script", str(path), tuple(command[1:])


def _require_monitoring(interpreter: str) -> None:
    result = subprocess.run(
        [
            interpreter,
            "-c",
            "import json,sys; print(json.dumps({'version':sys.version.split()[0], 'monitoring':hasattr(sys,'monitoring')}))",
        ],
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=10,
    )
    try:
        payload = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise WorkspaceError(
            f"cannot inspect trace interpreter: {result.stderr.strip()}"
        ) from error
    if result.returncode or not payload.get("monitoring"):
        raise WorkspaceError(
            f"trace requires Python 3.12+ with sys.monitoring; observed {payload.get('version', 'unknown')}"
        )


def _trace_path(workspace: Workspace, path: str | Path | None) -> Path:
    trace = _optional_trace_path(workspace, path)
    if trace is None:
        raise WorkspaceError("no trace reports found; run pymgr trace -- <command>")
    return trace


def _optional_trace_path(workspace: Workspace, path: str | Path | None) -> Path | None:
    if path:
        candidate = Path(path)
        if not candidate.is_absolute():
            candidate = workspace.root / candidate
        candidate = candidate.resolve()
        traces = (workspace.state_dir / "traces").resolve()
        if candidate.parent != traces:
            raise WorkspaceError("trace path must be inside .pymgr/traces")
        if not candidate.is_file():
            raise WorkspaceError(f"trace report does not exist: {path}")
        return candidate
    candidates = sorted((workspace.state_dir / "traces").glob("*.sqlite"))
    return candidates[-1] if candidates else None


def _open_trace(path: Path) -> sqlite3.Connection:
    try:
        database = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
        database.row_factory = sqlite3.Row
        version = database.execute(
            "SELECT value FROM metadata WHERE key = 'schema_version'"
        ).fetchone()
    except sqlite3.Error as error:
        raise WorkspaceError(f"cannot read trace report {path}: {error}") from error
    if version is None or version[0] != "1":
        database.close()
        raise WorkspaceError(f"unsupported trace schema in {path}")
    return database
