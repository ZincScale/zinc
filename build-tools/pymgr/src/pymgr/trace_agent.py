from __future__ import annotations

import argparse
import json
import os
import platform
import runpy
import sqlite3
import sys
import time
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path


class Monitor:
    def __init__(
        self,
        root: Path,
        source_roots: list[Path],
        loops: list[tuple[Path, int, int]],
    ) -> None:
        self.root = root.resolve()
        self.source_roots = [path.resolve() for path in source_roots]
        self.loop_specs = loops
        self.agent_path = Path(__file__).resolve()
        self.tool_id = self._claim_tool_id()
        self.started: dict[int, tuple[str, int]] = {}
        self.functions: dict[str, list[int | str]] = {}
        self.edges: Counter[tuple[str, str]] = Counter()
        self.exceptions: Counter[tuple[str, str, str]] = Counter()
        self.imports: Counter[str] = Counter()
        self.loop_counts: dict[str, list[int]] = defaultdict(lambda: [0, 0, 0])
        self.active_loops: dict[tuple[int, str], int] = {}
        self.code_loops: dict[object, list[tuple[str, int, int]]] = {}

    def _claim_tool_id(self) -> int:
        monitoring = sys.monitoring
        for tool_id in (3, 4, 5):
            if monitoring.get_tool(tool_id) is None:
                monitoring.use_tool_id(tool_id, "pymgr")
                return tool_id
        raise RuntimeError("no sys.monitoring tool identifier is available")

    def install(self) -> None:
        monitoring = sys.monitoring
        events = monitoring.events
        monitoring.register_callback(self.tool_id, events.PY_START, self.on_start)
        monitoring.register_callback(self.tool_id, events.PY_RETURN, self.on_return)
        monitoring.register_callback(self.tool_id, events.PY_UNWIND, self.on_unwind)
        monitoring.register_callback(self.tool_id, events.RAISE, self.on_raise)
        monitoring.register_callback(
            self.tool_id, events.EXCEPTION_HANDLED, self.on_handled
        )
        monitoring.register_callback(self.tool_id, events.LINE, self.on_line)
        monitoring.set_events(
            self.tool_id,
            events.PY_START
            | events.PY_UNWIND
            | events.RAISE
            | events.EXCEPTION_HANDLED,
        )
        sys.addaudithook(self.on_audit)

    def close(self) -> None:
        sys.monitoring.free_tool_id(self.tool_id)

    def on_start(self, code, _offset):
        if not self._is_project(code.co_filename):
            return
        frame = sys._getframe(1)
        symbol = self._symbol(code)
        caller = frame.f_back
        caller_symbol = (
            self._symbol(caller.f_code)
            if caller is not None and self._is_project(caller.f_code.co_filename)
            else "<external>"
        )
        self.edges[(caller_symbol, symbol)] += 1
        self.started[id(frame)] = (symbol, time.perf_counter_ns())
        values = self.functions.setdefault(
            symbol,
            [str(Path(code.co_filename).resolve()), code.co_firstlineno, 0, 0, 0],
        )
        values[2] = int(values[2]) + 1

        local_events = sys.monitoring.events.PY_RETURN
        targets = self._loops_for_code(code)
        if targets:
            self.code_loops[code] = targets
            local_events |= sys.monitoring.events.LINE
        sys.monitoring.set_local_events(self.tool_id, code, local_events)

    def on_return(self, _code, _offset, _retval):
        frame = sys._getframe(1)
        self._finish_frame(frame, returned=True)

    def on_unwind(self, code, _offset, _exception):
        if self._is_project(code.co_filename):
            self._finish_frame(sys._getframe(1), returned=False)

    def on_raise(self, code, _offset, exception):
        if self._is_project(code.co_filename):
            self.exceptions[
                (self._symbol(code), type(exception).__name__, "raised")
            ] += 1

    def on_handled(self, code, _offset, exception):
        if self._is_project(code.co_filename):
            self.exceptions[
                (self._symbol(code), type(exception).__name__, "handled")
            ] += 1

    def on_line(self, code, line):
        targets = self.code_loops.get(code, ())
        if not targets:
            return
        frame_id = id(sys._getframe(1))
        now = time.perf_counter_ns()
        for location, start, end in targets:
            key = (frame_id, location)
            if line == start:
                counts = self.loop_counts[location]
                counts[1] += 1
                if key not in self.active_loops:
                    counts[0] += 1
                    self.active_loops[key] = now
            elif key in self.active_loops and not start <= line <= end:
                self._finish_loop(key, now)

    def on_audit(self, event, args):
        if event == "import" and args and isinstance(args[0], str):
            self.imports[args[0]] += 1

    def _finish_frame(self, frame, *, returned: bool) -> None:
        observed = self.started.pop(id(frame), None)
        now = time.perf_counter_ns()
        if observed:
            symbol, started = observed
            values = self.functions[symbol]
            values[3] = int(values[3]) + (now - started)
            values[4] = int(values[4]) + (1 if returned else 0)
        for key in [key for key in self.active_loops if key[0] == id(frame)]:
            self._finish_loop(key, now)

    def _finish_loop(self, key: tuple[int, str], now: int) -> None:
        started = self.active_loops.pop(key)
        counts = self.loop_counts[key[1]]
        counts[2] += now - started

    def _loops_for_code(self, code) -> list[tuple[str, int, int]]:
        filename = Path(code.co_filename).resolve()
        return [
            (f"{path}:{start}", start, end)
            for path, start, end in self.loop_specs
            if path == filename
        ]

    def _is_project(self, filename: str) -> bool:
        try:
            path = Path(filename).resolve()
        except OSError:
            return False
        return (
            path != self.agent_path
            and (path == self.root or self.root in path.parents)
            and not any(part in {".pymgr", ".venv"} for part in path.parts)
        )

    def _symbol(self, code) -> str:
        path = Path(code.co_filename).resolve()
        module = ""
        for source_root in sorted(
            self.source_roots, key=lambda item: len(item.parts), reverse=True
        ):
            if path == source_root or source_root in path.parents:
                relative = path.relative_to(source_root).with_suffix("")
                parts = list(relative.parts)
                if parts and parts[-1] == "__init__":
                    parts.pop()
                module = ".".join(parts)
                break
        if not module:
            module = ".".join(path.relative_to(self.root).with_suffix("").parts)
        return module if code.co_name == "<module>" else f"{module}.{code.co_qualname}"


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--source-root", type=Path, action="append", default=[])
    parser.add_argument("--loop", action="append", default=[])
    parser.add_argument("--mode", choices=["script", "module", "code"], required=True)
    parser.add_argument("target")
    parser.add_argument("args", nargs=argparse.REMAINDER)
    return parser


def _run_target(mode: str, target: str, args: list[str]) -> None:
    if args and args[0] == "--":
        args = args[1:]
    sys.argv = [target, *args]
    if mode == "module":
        runpy.run_module(target, run_name="__main__", alter_sys=True)
    elif mode == "script":
        runpy.run_path(target, run_name="__main__")
    else:
        exec(compile(target, "<string>", "exec"), {"__name__": "__main__"})


def _write_trace(
    output: Path,
    monitor: Monitor,
    metadata: dict[str, str],
) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    with sqlite3.connect(output) as database:
        database.executescript(
            """
            CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
            CREATE TABLE functions (
                symbol TEXT PRIMARY KEY, file TEXT NOT NULL, line INTEGER NOT NULL,
                calls INTEGER NOT NULL, total_ns INTEGER NOT NULL, returns INTEGER NOT NULL
            );
            CREATE TABLE edges (
                caller TEXT NOT NULL, callee TEXT NOT NULL, calls INTEGER NOT NULL,
                PRIMARY KEY (caller, callee)
            );
            CREATE TABLE exceptions (
                symbol TEXT NOT NULL, type TEXT NOT NULL, event TEXT NOT NULL,
                count INTEGER NOT NULL, PRIMARY KEY (symbol, type, event)
            );
            CREATE TABLE imports (module TEXT PRIMARY KEY, count INTEGER NOT NULL);
            CREATE TABLE loops (
                location TEXT PRIMARY KEY, entries INTEGER NOT NULL,
                header_hits INTEGER NOT NULL, total_ns INTEGER NOT NULL
            );
            """
        )
        database.executemany("INSERT INTO metadata VALUES (?, ?)", metadata.items())
        database.executemany(
            "INSERT INTO functions VALUES (?, ?, ?, ?, ?, ?)",
            ((symbol, *values) for symbol, values in monitor.functions.items()),
        )
        database.executemany(
            "INSERT INTO edges VALUES (?, ?, ?)",
            (
                (caller, callee, calls)
                for (caller, callee), calls in monitor.edges.items()
            ),
        )
        database.executemany(
            "INSERT INTO exceptions VALUES (?, ?, ?, ?)",
            (
                (symbol, kind, event, count)
                for (symbol, kind, event), count in monitor.exceptions.items()
            ),
        )
        database.executemany(
            "INSERT INTO imports VALUES (?, ?)", monitor.imports.items()
        )
        database.executemany(
            "INSERT INTO loops VALUES (?, ?, ?, ?)",
            ((location, *counts) for location, counts in monitor.loop_counts.items()),
        )


def main() -> int:
    args = _parser().parse_args()
    loops = []
    for item in args.loop:
        path, start, end = json.loads(item)
        loops.append((Path(path).resolve(), int(start), int(end)))
    monitor = Monitor(args.root, args.source_root, loops)
    started_at = datetime.now(timezone.utc).isoformat()
    exit_code = 0
    monitor.install()
    try:
        _run_target(args.mode, args.target, args.args)
    except SystemExit as error:
        exit_code = (
            error.code if isinstance(error.code, int) else (1 if error.code else 0)
        )
    except BaseException:
        exit_code = 1
        raise
    finally:
        monitor.close()
        _write_trace(
            args.output,
            monitor,
            {
                "schema_version": "1",
                "started_at": started_at,
                "finished_at": datetime.now(timezone.utc).isoformat(),
                "root": str(args.root.resolve()),
                "python": sys.executable,
                "python_version": platform.python_version(),
                "implementation": platform.python_implementation(),
                "pid": str(os.getpid()),
                "mode": args.mode,
                "target": args.target,
                "arguments": json.dumps(args.args),
                "exit_code": str(exit_code),
            },
        )
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
