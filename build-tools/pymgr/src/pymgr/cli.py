from __future__ import annotations

import argparse
import json
import sys
from dataclasses import asdict
from pathlib import Path
from typing import Sequence

from pymgr.analysis import ProjectIndex
from pymgr.refactor import (
    apply_plan,
    plan_module_move,
    plan_symbol_rename,
    update_export,
)
from pymgr.workspace import Workspace, WorkspaceError


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="pymgr", description="Optional Python project development coordinator"
    )
    parser.add_argument("--root", type=Path, help="workspace root or a path inside it")
    parser.add_argument(
        "--json", action="store_true", help="emit machine-readable output"
    )
    commands = parser.add_subparsers(dest="command", required=True)

    commands.add_parser("init")
    sync = commands.add_parser("sync")
    sync.add_argument(
        "--check",
        action="store_true",
        help="verify the checked-in lockfile without updating it",
    )
    commands.add_parser("status")
    commands.add_parser("doctor")

    add = commands.add_parser("add")
    add.add_argument("packages", nargs="+")
    remove = commands.add_parser("remove")
    remove.add_argument("packages", nargs="+")
    update = commands.add_parser("update")
    update.add_argument("packages", nargs="*")
    update.add_argument(
        "--all", action="store_true", help="upgrade all locked packages"
    )

    commands.add_parser("modules")
    resolve = commands.add_parser("resolve")
    resolve.add_argument("target")
    imports = commands.add_parser("imports")
    imports.add_argument("module")
    importers = commands.add_parser("importers")
    importers.add_argument("module")
    graph = commands.add_parser("graph")
    graph.add_argument("kind", choices=["imports"])
    commands.add_parser("cycles")

    exports = commands.add_parser("exports")
    exports.add_argument("package")
    export = commands.add_parser("export")
    export.add_argument("package")
    export.add_argument("name")
    export.add_argument("--from", dest="from_module", required=True)
    unexport = commands.add_parser("unexport")
    unexport.add_argument("package")
    unexport.add_argument("name")

    api = commands.add_parser("api")
    api.add_argument("action", choices=["snapshot", "diff", "check"])

    move = commands.add_parser("move")
    move.add_argument("old_module")
    move.add_argument("new_module")
    move.add_argument("--apply", action="store_true", help="apply the displayed plan")
    rename = commands.add_parser("rename")
    rename.add_argument("qualified_symbol")
    rename.add_argument("new_name")
    rename.add_argument("--apply", action="store_true", help="apply the displayed plan")
    return parser


def _emit(payload: object, as_json: bool) -> None:
    if as_json:
        print(json.dumps(payload, indent=2, sort_keys=True, default=str))
        return
    if isinstance(payload, str):
        print(payload)
    elif isinstance(payload, list):
        for item in payload:
            print(item)
    else:
        print(json.dumps(payload, indent=2, sort_keys=True, default=str))


def _index(workspace: Workspace) -> ProjectIndex:
    return ProjectIndex(workspace).build()


def _module_payload(index: ProjectIndex) -> dict:
    return {
        "modules": [
            {
                "name": module.name,
                "path": str(module.path),
                "package": module.is_package,
                "owner": module.owner,
                "exports": module.exports,
            }
            for module in sorted(index.modules.values(), key=lambda item: item.name)
        ],
        "issues": [asdict(issue) for issue in index.issues],
    }


def _resolve(index: ProjectIndex, workspace: Workspace, target: str) -> dict:
    module = index.modules.get(target)
    if module:
        return {
            "target": target,
            "kind": "module",
            "origin": str(module.path),
            "package": module.is_package,
            "exports": module.exports,
        }
    parts = target.split(".")
    for split in range(len(parts) - 1, 0, -1):
        module_name = ".".join(parts[:split])
        symbol = ".".join(parts[split:])
        module = index.modules.get(module_name)
        if module and symbol in module.exports:
            return {
                "target": target,
                "kind": "export",
                "origin": module.export_origins.get(symbol, f"{module_name}.{symbol}"),
                "declared_by": str(module.path),
            }
        if module and symbol in module.definitions:
            return {
                "target": target,
                "kind": "definition",
                "origin": str(module.path),
                "signature": module.definitions[symbol],
                "public": symbol in module.exports,
            }
    for split in range(len(parts), 0, -1):
        module_name = ".".join(parts[:split])
        result = workspace.probe_module(module_name)
        if result.get("origin") or result.get("locations"):
            result["target"] = target
            result["kind"] = (
                "environment-module" if split == len(parts) else "environment-symbol"
            )
            if split != len(parts):
                result["symbol"] = ".".join(parts[split:])
            return result
    raise WorkspaceError(f"cannot resolve module or symbol: {target}")


def _plan_payload(plan, applied: bool) -> dict:
    return {
        "summary": plan.summary,
        "applied": applied,
        "files": [str(change.path) for change in plan.changes],
        "move": {"from": str(plan.source), "to": str(plan.destination)}
        if plan.source
        else None,
        "warnings": list(plan.warnings),
    }


def run(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    workspace = Workspace.discover(args.root)

    if args.command == "init":
        state = workspace.init()
        _emit({"root": str(workspace.root), "state": asdict(state)}, args.json)
        return 0
    if args.command in {"sync", "add", "remove", "update"}:
        packages = getattr(args, "packages", ())
        if args.command == "update" and not packages and not args.all:
            raise WorkspaceError("update requires a package or explicit --all")
        if args.command == "update" and packages and args.all:
            raise WorkspaceError("update accepts package names or --all, not both")
        operation = "check" if args.command == "sync" and args.check else args.command
        state = workspace.mutate(operation, packages)
        _emit(asdict(state), args.json)
        return 0
    if args.command == "status":
        state, synchronized = workspace.status()
        payload = {**asdict(state), "synchronized": synchronized}
        _emit(payload, args.json)
        return 0 if synchronized else 1
    if args.command == "doctor":
        findings = workspace.doctor()
        if args.json:
            _emit([asdict(item) for item in findings], True)
        elif findings:
            for item in findings:
                print(
                    f"{item.level}: {item.subject}: observed {item.observed}; expected {item.expected}; {item.repair}"
                )
        else:
            print(
                "ready: workspace metadata, lockfile, interpreter, and source roots agree"
            )
        return 1 if any(item.level == "error" for item in findings) else 0

    index = _index(workspace)
    if args.command == "modules":
        _emit(_module_payload(index), args.json)
    elif args.command == "resolve":
        _emit(_resolve(index, workspace, args.target), args.json)
    elif args.command == "imports":
        module = index.modules.get(args.module)
        if not module:
            raise WorkspaceError(f"unknown local module: {args.module}")
        _emit([asdict(reference) for reference in module.imports], args.json)
    elif args.command == "importers":
        payload = [
            {"module": module, **asdict(reference)}
            for module, reference in index.importers(args.module)
        ]
        _emit(payload, args.json)
    elif args.command == "graph":
        payload = {
            name: sorted(targets) for name, targets in sorted(index.graph().items())
        }
        _emit(payload, args.json)
    elif args.command == "cycles":
        _emit(index.cycles(), args.json)
        return 1 if index.cycles() else 0
    elif args.command == "exports":
        module = index.modules.get(args.package)
        if not module or not module.is_package:
            raise WorkspaceError(f"unknown local package: {args.package}")
        _emit(
            [
                {
                    "name": name,
                    "origin": module.export_origins.get(name, f"{module.name}.{name}"),
                }
                for name in module.exports
            ],
            args.json,
        )
    elif args.command in {"export", "unexport"}:
        module = index.modules.get(args.package)
        if not module or not module.is_package:
            raise WorkspaceError(f"unknown local package: {args.package}")
        changed = update_export(
            module.path,
            args.package,
            args.name,
            getattr(args, "from_module", None),
            args.command == "export",
        )
        _emit({"changed": changed, "path": str(module.path)}, args.json)
    elif args.command == "api":
        if args.action == "snapshot":
            path = index.write_api_snapshot()
            _emit({"snapshot": str(path)}, args.json)
        else:
            difference = index.api_diff()
            _emit(difference, args.json)
            if args.action == "check" and (
                difference["removed"] or difference["changed"]
            ):
                return 1
    elif args.command == "move":
        plan = plan_module_move(index, args.old_module, args.new_module)
        if args.apply:
            apply_plan(plan)
        _emit(_plan_payload(plan, args.apply), args.json)
    elif args.command == "rename":
        plan = plan_symbol_rename(index, args.qualified_symbol, args.new_name)
        if args.apply:
            apply_plan(plan)
        _emit(_plan_payload(plan, args.apply), args.json)
    else:
        raise WorkspaceError(f"unsupported command: {args.command}")
    return 0


def main() -> int:
    try:
        return run()
    except WorkspaceError as error:
        print(f"error: {error}", file=sys.stderr)
        return 2
