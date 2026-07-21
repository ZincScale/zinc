from __future__ import annotations

import ast
import json
import sys
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path

from pymgr.workspace import (
    Workspace,
    WorkspaceError,
    _atomic_write,
    _normalize_distribution,
)


@dataclass(frozen=True)
class ImportRef:
    module: str
    names: tuple[str, ...]
    line: int
    type_only: bool = False
    dynamic: bool = False


@dataclass
class ModuleInfo:
    name: str
    path: Path
    source_root: Path
    is_package: bool
    owner: str
    owner_dependencies: frozenset[str]
    imports: list[ImportRef] = field(default_factory=list)
    exports: list[str] = field(default_factory=list)
    export_origins: dict[str, str] = field(default_factory=dict)
    definitions: dict[str, str] = field(default_factory=dict)
    dynamic_exports: bool = False
    mutates_sys_path: bool = False


@dataclass(frozen=True)
class AnalysisIssue:
    code: str
    severity: str
    module: str
    line: int
    message: str


def _module_name(source_root: Path, path: Path) -> tuple[str, bool]:
    relative = path.relative_to(source_root)
    parts = list(relative.with_suffix("").parts)
    is_package = parts[-1] == "__init__"
    if is_package:
        parts.pop()
    return ".".join(parts), is_package


def _resolve_relative(
    current: str, is_package: bool, level: int, module: str | None
) -> str:
    if level == 0:
        return module or ""
    package = current.split(".") if is_package else current.split(".")[:-1]
    ascend = level - 1
    if ascend > len(package):
        return module or ""
    base = package[: len(package) - ascend]
    if module:
        base.extend(module.split("."))
    return ".".join(base)


def _signature(node: ast.AST) -> str:
    if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
        prefix = "async def" if isinstance(node, ast.AsyncFunctionDef) else "def"
        rendered = ast.unparse(node.args)
        returns = f" -> {ast.unparse(node.returns)}" if node.returns else ""
        return f"{prefix} {node.name}({rendered}){returns}"
    if isinstance(node, ast.ClassDef):
        bases = [ast.unparse(item) for item in (*node.bases, *node.keywords)]
        suffix = f"({', '.join(bases)})" if bases else ""
        return f"class {node.name}{suffix}"
    if isinstance(node, (ast.Assign, ast.AnnAssign)):
        annotation = getattr(node, "annotation", None)
        return ast.unparse(annotation) if annotation else "value"
    return type(node).__name__


class _Visitor(ast.NodeVisitor):
    def __init__(self, module: str, is_package: bool) -> None:
        self.module = module
        self.is_package = is_package
        self.imports: list[ImportRef] = []
        self.exports: list[str] = []
        self.export_origins: dict[str, str] = {}
        self.definitions: dict[str, str] = {}
        self.dynamic_exports = False
        self.mutates_sys_path = False
        self._type_checking = 0
        self._depth = 0

    def visit_If(self, node: ast.If) -> None:
        is_type_checking = (
            isinstance(node.test, ast.Name)
            and node.test.id == "TYPE_CHECKING"
            or isinstance(node.test, ast.Attribute)
            and isinstance(node.test.value, ast.Name)
            and node.test.value.id == "typing"
            and node.test.attr == "TYPE_CHECKING"
        )
        if is_type_checking:
            self._type_checking += 1
            for child in node.body:
                self.visit(child)
            self._type_checking -= 1
            for child in node.orelse:
                self.visit(child)
            return
        self.generic_visit(node)

    def visit_Import(self, node: ast.Import) -> None:
        for alias in node.names:
            self.imports.append(
                ImportRef(alias.name, (), node.lineno, bool(self._type_checking))
            )
            if self._depth == 0 and alias.asname == alias.name.rsplit(".", 1)[-1]:
                self.export_origins[alias.asname] = alias.name

    def visit_ImportFrom(self, node: ast.ImportFrom) -> None:
        target = _resolve_relative(
            self.module, self.is_package, node.level, node.module
        )
        names = tuple(alias.name for alias in node.names)
        self.imports.append(
            ImportRef(target, names, node.lineno, bool(self._type_checking))
        )
        if self._depth == 0:
            for alias in node.names:
                public = alias.asname or alias.name
                if alias.asname == alias.name:
                    self.export_origins[public] = f"{target}.{alias.name}".strip(".")

    def visit_FunctionDef(self, node: ast.FunctionDef) -> None:
        if self._depth == 0:
            self.definitions[node.name] = _signature(node)
        self._depth += 1
        self.generic_visit(node)
        self._depth -= 1

    visit_AsyncFunctionDef = visit_FunctionDef

    def visit_ClassDef(self, node: ast.ClassDef) -> None:
        if self._depth == 0:
            self.definitions[node.name] = _signature(node)
        self._depth += 1
        self.generic_visit(node)
        self._depth -= 1

    def visit_Assign(self, node: ast.Assign) -> None:
        if self._depth == 0:
            for target in node.targets:
                if isinstance(target, ast.Name):
                    if target.id == "__all__":
                        self._read_all(node.value)
                    else:
                        self.definitions[target.id] = _signature(node)
        self.generic_visit(node)

    def visit_AnnAssign(self, node: ast.AnnAssign) -> None:
        if self._depth == 0 and isinstance(node.target, ast.Name):
            self.definitions[node.target.id] = _signature(node)
        self.generic_visit(node)

    def _read_all(self, value: ast.AST) -> None:
        try:
            result = ast.literal_eval(value)
        except (ValueError, TypeError):
            self.dynamic_exports = True
            return
        if isinstance(result, (list, tuple)) and all(
            isinstance(item, str) for item in result
        ):
            self.exports = list(result)
        else:
            self.dynamic_exports = True

    def visit_Call(self, node: ast.Call) -> None:
        function = node.func
        dynamic = (
            isinstance(function, ast.Name)
            and function.id == "__import__"
            or isinstance(function, ast.Attribute)
            and isinstance(function.value, ast.Name)
            and function.value.id == "importlib"
            and function.attr == "import_module"
        )
        if dynamic:
            target = "<dynamic>"
            if (
                node.args
                and isinstance(node.args[0], ast.Constant)
                and isinstance(node.args[0].value, str)
            ):
                target = node.args[0].value
            self.imports.append(
                ImportRef(target, (), node.lineno, bool(self._type_checking), True)
            )

        if isinstance(function, ast.Attribute) and function.attr in {
            "append",
            "extend",
            "insert",
            "remove",
        }:
            receiver = function.value
            if (
                isinstance(receiver, ast.Attribute)
                and isinstance(receiver.value, ast.Name)
                and receiver.value.id == "sys"
                and receiver.attr == "path"
            ):
                self.mutates_sys_path = True
        self.generic_visit(node)


class ProjectIndex:
    def __init__(self, workspace: Workspace) -> None:
        self.workspace = workspace
        self.modules: dict[str, ModuleInfo] = {}
        self.issues: list[AnalysisIssue] = []

    def build(self) -> ProjectIndex:
        self.modules.clear()
        self.issues.clear()
        for member in self.workspace.project_members():
            for source_root in member.source_roots:
                if not source_root.is_dir():
                    continue
                for path in sorted(source_root.rglob("*.py")):
                    self._add_module(
                        path, source_root, member.name, member.dependencies
                    )
        self._diagnose()
        return self

    def _add_module(
        self,
        path: Path,
        source_root: Path,
        owner: str,
        owner_dependencies: frozenset[str],
    ) -> None:
        name, is_package = _module_name(source_root, path)
        if not name:
            return
        if name in self.modules:
            self.issues.append(
                AnalysisIssue(
                    "duplicate-module",
                    "error",
                    name,
                    1,
                    f"also provided by {self.modules[name].path}",
                )
            )
            return
        try:
            tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        except (OSError, SyntaxError) as error:
            line = getattr(error, "lineno", 1) or 1
            self.issues.append(AnalysisIssue("syntax", "error", name, line, str(error)))
            return
        visitor = _Visitor(name, is_package)
        visitor.visit(tree)
        self.modules[name] = ModuleInfo(
            name=name,
            path=path,
            source_root=source_root,
            is_package=is_package,
            owner=owner,
            owner_dependencies=owner_dependencies,
            imports=visitor.imports,
            exports=visitor.exports,
            export_origins=visitor.export_origins,
            definitions=visitor.definitions,
            dynamic_exports=visitor.dynamic_exports,
            mutates_sys_path=visitor.mutates_sys_path,
        )

    def _local_target(self, reference: ImportRef) -> str | None:
        if reference.module in self.modules:
            return reference.module
        for name in reference.names:
            candidate = f"{reference.module}.{name}".strip(".")
            if candidate in self.modules:
                return candidate
        return None

    def graph(self) -> dict[str, set[str]]:
        result = {name: set() for name in self.modules}
        for name, module in self.modules.items():
            for reference in module.imports:
                target = self._local_target(reference)
                if target:
                    result[name].add(target)
        return result

    def importers(self, target: str) -> list[tuple[str, ImportRef]]:
        result: list[tuple[str, ImportRef]] = []
        for module_name, module in self.modules.items():
            for reference in module.imports:
                local = self._local_target(reference)
                if local == target or reference.module == target:
                    result.append((module_name, reference))
        return result

    def cycles(self) -> list[list[str]]:
        graph = self.graph()
        index = 0
        indexes: dict[str, int] = {}
        lowlinks: dict[str, int] = {}
        stack: list[str] = []
        on_stack: set[str] = set()
        result: list[list[str]] = []

        def visit(node: str) -> None:
            nonlocal index
            indexes[node] = lowlinks[node] = index
            index += 1
            stack.append(node)
            on_stack.add(node)
            for target in graph[node]:
                if target not in indexes:
                    visit(target)
                    lowlinks[node] = min(lowlinks[node], lowlinks[target])
                elif target in on_stack:
                    lowlinks[node] = min(lowlinks[node], indexes[target])
            if lowlinks[node] == indexes[node]:
                component: list[str] = []
                while True:
                    member = stack.pop()
                    on_stack.remove(member)
                    component.append(member)
                    if member == node:
                        break
                if len(component) > 1 or node in graph[node]:
                    result.append(sorted(component))

        for module in graph:
            if module not in indexes:
                visit(module)
        return sorted(result)

    def _diagnose(self) -> None:
        top_level = {name.split(".", 1)[0] for name in self.modules}
        for name in sorted(top_level.intersection(sys.stdlib_module_names)):
            self.issues.append(
                AnalysisIssue(
                    "stdlib-shadow",
                    "error",
                    name,
                    1,
                    f"project module shadows stdlib {name}",
                )
            )
        for module in self.modules.values():
            parent = module.path.parent
            while parent != module.source_root and module.source_root in parent.parents:
                marker = parent / "__init__.py"
                if not marker.exists():
                    self.issues.append(
                        AnalysisIssue(
                            "missing-init",
                            "error",
                            module.name,
                            1,
                            f"regular-package policy requires {marker}",
                        )
                    )
                parent = parent.parent
            if module.mutates_sys_path:
                self.issues.append(
                    AnalysisIssue(
                        "sys-path",
                        "error",
                        module.name,
                        1,
                        "project code mutates sys.path",
                    )
                )
            if module.dynamic_exports:
                self.issues.append(
                    AnalysisIssue(
                        "dynamic-all",
                        "warning",
                        module.name,
                        1,
                        "__all__ is not a static list or tuple",
                    )
                )
            for name, count in Counter(module.exports).items():
                if count > 1:
                    self.issues.append(
                        AnalysisIssue(
                            "duplicate-export",
                            "error",
                            module.name,
                            1,
                            f"{name} appears {count} times in __all__",
                        )
                    )
            for name in module.exports:
                if name not in module.definitions and name not in module.export_origins:
                    self.issues.append(
                        AnalysisIssue(
                            "missing-export",
                            "error",
                            module.name,
                            1,
                            f"__all__ names undefined symbol {name}",
                        )
                    )
            for reference in module.imports:
                if "*" in reference.names:
                    self.issues.append(
                        AnalysisIssue(
                            "star-import",
                            "error",
                            module.name,
                            reference.line,
                            f"star import from {reference.module}",
                        )
                    )
                private_names = [
                    name for name in reference.names if name.startswith("_")
                ]
                if private_names:
                    self.issues.append(
                        AnalysisIssue(
                            "private-import",
                            "warning",
                            module.name,
                            reference.line,
                            f"private import from {reference.module}: {', '.join(private_names)}",
                        )
                    )
                if reference.dynamic:
                    self.issues.append(
                        AnalysisIssue(
                            "dynamic-import",
                            "warning",
                            module.name,
                            reference.line,
                            f"dynamic import: {reference.module}",
                        )
                    )
                target_name = self._local_target(reference)
                target = self.modules.get(target_name) if target_name else None
                if target and target.owner != module.owner:
                    required = _normalize_distribution(target.owner)
                    if required not in module.owner_dependencies:
                        self.issues.append(
                            AnalysisIssue(
                                "undeclared-workspace-dependency",
                                "error",
                                module.name,
                                reference.line,
                                f"imports {target.name} from {target.owner} without declaring {target.owner}",
                            )
                        )
                    public_package = target.name.split(".", 1)[0]
                    public = self.modules.get(public_package)
                    through_public_package = reference.module == public_package
                    imported_names_are_public = bool(public) and all(
                        name in public.exports
                        for name in reference.names
                        if name != "*"
                    )
                    if target.name != public_package and not (
                        through_public_package and imported_names_are_public
                    ):
                        self.issues.append(
                            AnalysisIssue(
                                "workspace-private-import",
                                "warning",
                                module.name,
                                reference.line,
                                f"import bypasses the public {public_package} package surface",
                            )
                        )
        for cycle in self.cycles():
            self.issues.append(
                AnalysisIssue(
                    "import-cycle",
                    "error",
                    cycle[0],
                    1,
                    " -> ".join([*cycle, cycle[0]]),
                )
            )

    def api_payload(self) -> dict:
        modules: dict[str, dict] = {}
        for name, module in sorted(self.modules.items()):
            if not module.exports:
                continue
            symbols: dict[str, dict[str, str]] = {}
            for symbol in module.exports:
                origin = module.export_origins.get(symbol, f"{name}.{symbol}")
                signature = module.definitions.get(symbol, "")
                if not signature and "." in origin:
                    origin_module, origin_name = origin.rsplit(".", 1)
                    target = self.modules.get(origin_module)
                    if target:
                        signature = target.definitions.get(origin_name, "")
                symbols[symbol] = {"origin": origin, "signature": signature}
            modules[name] = {"symbols": symbols}
        return {"schema": 1, "modules": modules}

    def write_api_snapshot(self) -> Path:
        path = self.workspace.state_dir / "api.json"
        payload = (
            json.dumps(self.api_payload(), indent=2, sort_keys=True) + "\n"
        ).encode()
        _atomic_write(path, payload)
        return path

    def api_diff(self) -> dict[str, list[str]]:
        path = self.workspace.state_dir / "api.json"
        if not path.exists():
            raise WorkspaceError("no API snapshot; run pymgr api snapshot")
        previous = json.loads(path.read_text(encoding="utf-8"))
        current = self.api_payload()
        before = _flatten_api(previous)
        after = _flatten_api(current)
        return {
            "removed": sorted(before.keys() - after.keys()),
            "added": sorted(after.keys() - before.keys()),
            "changed": sorted(
                key
                for key in before.keys() & after.keys()
                if before[key].get("signature") != after[key].get("signature")
            ),
            "moved": sorted(
                key
                for key in before.keys() & after.keys()
                if before[key].get("origin") != after[key].get("origin")
            ),
        }


def _flatten_api(payload: dict) -> dict[str, dict]:
    result: dict[str, dict] = {}
    for module, details in payload.get("modules", {}).items():
        for symbol, signature in details.get("symbols", {}).items():
            result[f"{module}.{symbol}"] = signature
    return result
