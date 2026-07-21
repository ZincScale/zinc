from __future__ import annotations

import ast
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

import libcst as cst
from libcst.helpers import get_full_name_for_node
from libcst.metadata import QualifiedNameProvider

from pymgr.analysis import ProjectIndex
from pymgr.workspace import Workspace, WorkspaceError, _atomic_write


@dataclass(frozen=True)
class Change:
    path: Path
    before: str
    after: str


@dataclass(frozen=True)
class RefactorPlan:
    summary: str
    changes: tuple[Change, ...]
    warnings: tuple[str, ...] = ()
    source: Path | None = None
    destination: Path | None = None
    move_is_directory: bool = False


@dataclass(frozen=True)
class ImportToolResult:
    mode: str
    applied: bool
    files: tuple[Path, ...]
    output: str


def _dotted_expression(name: str) -> cst.BaseExpression:
    expression = cst.parse_expression(name)
    if not isinstance(expression, (cst.Name, cst.Attribute)):
        raise WorkspaceError(f"invalid module name: {name}")
    return expression


def _replace_prefix(name: str, old: str, new: str) -> str:
    if name == old:
        return new
    if name.startswith(old + "."):
        return new + name[len(old) :]
    return name


class _ImportMoveTransformer(cst.CSTTransformer):
    def __init__(
        self, old: str, new: str, current_module: str, is_package: bool
    ) -> None:
        self.old = old
        self.new = new
        self.current_module = current_module
        self.is_package = is_package

    def leave_ImportAlias(
        self, original_node: cst.ImportAlias, updated_node: cst.ImportAlias
    ) -> cst.ImportAlias:
        name = get_full_name_for_node(original_node.name)
        if not name:
            return updated_node
        replaced = _replace_prefix(name, self.old, self.new)
        if replaced == name:
            return updated_node
        return updated_node.with_changes(name=_dotted_expression(replaced))

    def leave_ImportFrom(
        self, original_node: cst.ImportFrom, updated_node: cst.ImportFrom
    ) -> cst.ImportFrom:
        name = _absolute_import_from(
            self.current_module, self.is_package, original_node
        )
        if not name:
            return updated_node
        replaced = _replace_prefix(name, self.old, self.new)
        if replaced == name:
            return updated_node
        if original_node.relative:
            dots, module = _relative_import_parts(
                self.current_module, self.is_package, replaced
            )
            return updated_node.with_changes(
                relative=tuple(cst.Dot() for _ in range(dots)), module=module
            )
        return updated_node.with_changes(module=_dotted_expression(replaced))


class _RelocateRelativeImportsTransformer(cst.CSTTransformer):
    def __init__(self, old_module: str, new_module: str, is_package: bool) -> None:
        self.old_module = old_module
        self.new_module = new_module
        self.is_package = is_package

    def leave_ImportFrom(
        self, original_node: cst.ImportFrom, updated_node: cst.ImportFrom
    ) -> cst.ImportFrom:
        if not original_node.relative:
            return updated_node
        absolute = _absolute_import_from(
            self.old_module, self.is_package, original_node
        )
        if not absolute:
            return updated_node
        dots, module = _relative_import_parts(
            self.new_module, self.is_package, absolute
        )
        return updated_node.with_changes(
            relative=tuple(cst.Dot() for _ in range(dots)), module=module
        )


class _QualifiedRenameTransformer(cst.CSTTransformer):
    METADATA_DEPENDENCIES = (QualifiedNameProvider,)

    def __init__(
        self,
        provider_name: str,
        qualified_name: str,
        new_name: str,
        current_module: str,
        is_package: bool,
    ) -> None:
        self.provider_name = provider_name
        self.module_name, self.old_name = qualified_name.rsplit(".", 1)
        self.new_name = new_name
        self.current_module = current_module
        self.is_package = is_package

    def leave_Name(self, original_node: cst.Name, updated_node: cst.Name) -> cst.Name:
        names = self.get_metadata(QualifiedNameProvider, original_node, set())
        if original_node.value == self.old_name and any(
            name.name == self.provider_name for name in names
        ):
            return updated_node.with_changes(value=self.new_name)
        return updated_node

    def leave_ImportFrom(
        self, original_node: cst.ImportFrom, updated_node: cst.ImportFrom
    ) -> cst.ImportFrom:
        if not isinstance(original_node.names, tuple):
            return updated_node
        if (
            _absolute_import_from(self.current_module, self.is_package, original_node)
            != self.module_name
        ):
            return updated_node
        aliases: list[cst.ImportAlias] = []
        changed = False
        for alias in updated_node.names:
            if get_full_name_for_node(alias.name) == self.old_name:
                asname = alias.asname
                if asname and asname.name.value == self.old_name:
                    asname = asname.with_changes(name=cst.Name(self.new_name))
                aliases.append(
                    alias.with_changes(name=cst.Name(self.new_name), asname=asname)
                )
                changed = True
            else:
                aliases.append(alias)
        return (
            updated_node.with_changes(names=tuple(aliases)) if changed else updated_node
        )


class _AllRenameTransformer(cst.CSTTransformer):
    def __init__(self, old_name: str, new_name: str) -> None:
        self.old_name = old_name
        self.new_name = new_name

    def leave_Assign(
        self, original_node: cst.Assign, updated_node: cst.Assign
    ) -> cst.Assign:
        if (
            len(original_node.targets) != 1
            or not isinstance(original_node.targets[0].target, cst.Name)
            or original_node.targets[0].target.value != "__all__"
            or not isinstance(original_node.value, (cst.List, cst.Tuple))
        ):
            return updated_node
        values = [_string_value(element) for element in original_node.value.elements]
        if any(value is None for value in values):
            return updated_node
        renamed = [
            self.new_name if value == self.old_name else value for value in values
        ]
        if renamed == values:
            return updated_node
        return updated_node.with_changes(value=cst.parse_expression(repr(renamed)))


def _absolute_import_from(
    current_module: str, is_package: bool, node: cst.ImportFrom
) -> str:
    suffix = get_full_name_for_node(node.module) if node.module else ""
    if not node.relative:
        return suffix or ""
    package = (
        current_module.split(".") if is_package else current_module.split(".")[:-1]
    )
    ascend = len(node.relative) - 1
    if ascend > len(package):
        return suffix or ""
    base = package[: len(package) - ascend]
    if suffix:
        base.extend(suffix.split("."))
    return ".".join(base)


def _relative_import_parts(
    current_module: str, is_package: bool, target: str
) -> tuple[int, cst.BaseExpression | None]:
    package = (
        current_module.split(".") if is_package else current_module.split(".")[:-1]
    )
    target_parts = target.split(".")
    common = 0
    for left, right in zip(package, target_parts):
        if left != right:
            break
        common += 1
    if common == 0:
        return 0, _dotted_expression(target)
    dots = len(package) - common + 1
    remainder = ".".join(target_parts[common:])
    return dots, _dotted_expression(remainder) if remainder else None


def _parse_module(source: str, path: Path) -> cst.Module:
    try:
        return cst.parse_module(source)
    except cst.ParserSyntaxError as error:
        raise WorkspaceError(f"cannot parse {path}: {error}") from error


def _validate_python(source: str, path: Path) -> None:
    try:
        ast.parse(source, filename=str(path))
    except SyntaxError as error:
        raise WorkspaceError(
            f"refactor produced invalid Python for {path}: {error}"
        ) from error


def run_import_tool(
    workspace: Workspace, mode: str, paths: Iterable[str], apply: bool
) -> ImportToolResult:
    if mode not in {"fix", "organize"}:
        raise WorkspaceError(f"unknown import operation: {mode}")
    requested = list(paths)
    targets = (
        [_resolve_workspace_path(workspace, path) for path in requested]
        if requested
        else workspace.source_roots()
    )
    files = tuple(_python_files(targets))
    before = {path: path.read_bytes() for path in files}
    ruff = Path(sys.executable).parent / (
        "ruff.exe" if sys.platform == "win32" else "ruff"
    )
    executable = str(ruff) if ruff.is_file() else shutil.which("ruff")
    if not executable:
        raise WorkspaceError("the isolated pymgr installation does not contain Ruff")
    rule = "I" if mode == "organize" else "F401"
    command = [
        executable,
        "check",
        "--select",
        rule,
        "--no-cache",
        "--color",
        "never",
    ]
    command.append("--fix-only" if apply else "--diff")
    command.extend(str(path) for path in targets)
    try:
        result = subprocess.run(
            command,
            cwd=workspace.root,
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=60,
        )
    except subprocess.TimeoutExpired as error:
        raise WorkspaceError(
            f"Ruff import {mode} timed out after {error.timeout} seconds"
        ) from error
    preview_diff = not apply and result.returncode == 1 and "Would fix" in result.stdout
    if result.returncode and not preview_diff:
        if apply:
            for path, content in before.items():
                _atomic_write(path, content)
        detail = result.stderr.strip() or result.stdout.strip()
        raise WorkspaceError(f"Ruff import {mode} failed: {detail}")
    changed = tuple(
        path for path, content in before.items() if path.read_bytes() != content
    )
    if apply:
        try:
            for path in changed:
                _validate_python(path.read_text(encoding="utf-8"), path)
        except BaseException:
            for path, content in before.items():
                _atomic_write(path, content)
            raise
    return ImportToolResult(mode, apply, changed, result.stdout.strip())


def _resolve_workspace_path(workspace: Workspace, value: str) -> Path:
    path = (workspace.root / value).resolve()
    if path != workspace.root and workspace.root not in path.parents:
        raise WorkspaceError(f"import operation path escapes the workspace: {value}")
    if not path.exists():
        raise WorkspaceError(f"import operation path does not exist: {value}")
    return path


def _python_files(paths: Iterable[Path]) -> Iterable[Path]:
    seen: set[Path] = set()
    for path in paths:
        candidates = [path] if path.is_file() else sorted(path.rglob("*.py"))
        for candidate in candidates:
            if candidate.suffix == ".py" and candidate not in seen:
                seen.add(candidate)
                yield candidate


def plan_module_move(index: ProjectIndex, old: str, new: str) -> RefactorPlan:
    if old not in index.modules:
        raise WorkspaceError(f"unknown local module: {old}")
    if new in index.modules:
        raise WorkspaceError(f"destination module already exists: {new}")
    old_info = index.modules[old]
    source = old_info.path.parent if old_info.is_package else old_info.path
    if old_info.is_package:
        indexed = {
            module.path.resolve()
            for module in index.modules.values()
            if module.path == source or source in module.path.parents
        }
        unindexed = [
            path for path in source.rglob("*.py") if path.resolve() not in indexed
        ]
        if unindexed:
            raise WorkspaceError(
                f"package contains unindexed Python source: {unindexed[0]}"
            )
    destination = old_info.source_root.joinpath(*new.split("."))
    if not old_info.is_package:
        destination = destination.with_suffix(".py")
    if old_info.is_package and source in destination.parents:
        raise WorkspaceError("cannot move a package inside itself")
    if destination.exists():
        raise WorkspaceError(f"destination path already exists: {destination}")

    warnings: list[str] = []
    changes: list[Change] = []
    for module in index.modules.values():
        before = module.path.read_text(encoding="utf-8")
        tree = ast.parse(before, filename=str(module.path))
        for node in ast.walk(tree):
            if (
                isinstance(node, ast.Constant)
                and isinstance(node.value, str)
                and old in node.value
            ):
                warnings.append(
                    f"{module.path}:{getattr(node, 'lineno', 1)} string reference requires review"
                )
        relocated_name = module.name
        if module.name == old or (
            old_info.is_package and module.name.startswith(old + ".")
        ):
            relocated_name = _replace_prefix(module.name, old, new)
        transformed = _parse_module(before, module.path)
        if relocated_name != module.name:
            transformed = transformed.visit(
                _RelocateRelativeImportsTransformer(
                    module.name, relocated_name, module.is_package
                )
            )
        after = transformed.visit(
            _ImportMoveTransformer(old, new, relocated_name, module.is_package)
        ).code
        if after != before:
            _validate_python(after, module.path)
            changes.append(Change(module.path, before, after))

    return RefactorPlan(
        summary=f"move {old} to {new}: {len(changes)} file(s) update imports",
        changes=tuple(changes),
        warnings=tuple(sorted(set(warnings))),
        source=source,
        destination=destination,
        move_is_directory=old_info.is_package,
    )


def plan_symbol_rename(
    index: ProjectIndex, qualified_name: str, new_name: str
) -> RefactorPlan:
    if "." not in qualified_name:
        raise WorkspaceError(
            "rename requires a fully qualified symbol such as acme.models.Client"
        )
    module_name, old_name = qualified_name.rsplit(".", 1)
    module = index.modules.get(module_name)
    if module is None:
        raise WorkspaceError(f"unknown local module: {module_name}")
    if old_name not in module.definitions and old_name not in module.exports:
        raise WorkspaceError(f"unknown top-level symbol: {qualified_name}")
    if not new_name.isidentifier():
        raise WorkspaceError(f"invalid Python identifier: {new_name}")

    changes: list[Change] = []
    warnings: list[str] = []
    for item in index.modules.values():
        before = item.path.read_text(encoding="utf-8")
        syntax = ast.parse(before, filename=str(item.path))
        all_literals = {
            id(value)
            for assignment in ast.walk(syntax)
            if isinstance(assignment, ast.Assign)
            and any(
                isinstance(target, ast.Name) and target.id == "__all__"
                for target in assignment.targets
            )
            for value in ast.walk(assignment.value)
            if isinstance(value, ast.Constant) and isinstance(value.value, str)
        }
        for node in ast.walk(syntax):
            if (
                isinstance(node, ast.Constant)
                and isinstance(node.value, str)
                and (node.value == old_name or qualified_name in node.value)
                and id(node) not in all_literals
            ):
                warnings.append(
                    f"{item.path}:{getattr(node, 'lineno', 1)} string reference requires review"
                )
        parsed = _parse_module(before, item.path)
        wrapper = cst.MetadataWrapper(parsed)
        provider_name = old_name if item.name == module_name else qualified_name
        transformed = wrapper.visit(
            _QualifiedRenameTransformer(
                provider_name, qualified_name, new_name, item.name, item.is_package
            )
        )
        origin = item.export_origins.get(old_name, f"{item.name}.{old_name}")
        if old_name in item.exports and origin == qualified_name:
            transformed = transformed.visit(_AllRenameTransformer(old_name, new_name))
        after = transformed.code
        if after != before:
            _validate_python(after, item.path)
            changes.append(Change(item.path, before, after))
    if not changes:
        raise WorkspaceError(
            f"no statically resolved references found for {qualified_name}"
        )
    return RefactorPlan(
        summary=f"rename {qualified_name} to {new_name}: {len(changes)} file(s)",
        changes=tuple(changes),
        warnings=tuple(sorted(set(warnings))),
    )


def apply_plan(plan: RefactorPlan) -> None:
    backups = {change.path: change.before.encode() for change in plan.changes}
    created_packages: list[Path] = []
    moved = False
    try:
        for change in plan.changes:
            _atomic_write(change.path, change.after.encode())
        if plan.source and plan.destination:
            plan.destination.parent.mkdir(parents=True, exist_ok=True)
            source_root = _common_source_root(plan.source, plan.destination)
            current = plan.destination.parent
            while current != source_root and source_root in current.parents:
                marker = current / "__init__.py"
                if not marker.exists():
                    _atomic_write(marker, b"")
                    created_packages.append(marker)
                current = current.parent
            plan.source.replace(plan.destination)
            moved = True
        for path in _result_paths(plan):
            _validate_python(path.read_text(encoding="utf-8"), path)
    except BaseException:
        if moved and plan.source and plan.destination and plan.destination.exists():
            plan.destination.replace(plan.source)
        for path, content in backups.items():
            _atomic_write(path, content)
        for marker in created_packages:
            marker.unlink(missing_ok=True)
        raise


def _common_source_root(source: Path, destination: Path) -> Path:
    common = Path(
        *Path(source).parts[: _common_prefix_length(source.parts, destination.parts)]
    )
    return common


def _common_prefix_length(left: tuple[str, ...], right: tuple[str, ...]) -> int:
    length = 0
    for first, second in zip(left, right):
        if first != second:
            break
        length += 1
    return length


def _result_paths(plan: RefactorPlan) -> Iterable[Path]:
    for change in plan.changes:
        if plan.source and plan.destination and plan.move_is_directory:
            try:
                relative = change.path.relative_to(plan.source)
            except ValueError:
                yield change.path
            else:
                yield plan.destination / relative
        elif plan.source and plan.destination and change.path == plan.source:
            yield plan.destination
        else:
            yield change.path
    if (
        plan.destination
        and not plan.move_is_directory
        and not any(change.path == plan.source for change in plan.changes)
    ):
        yield plan.destination


def _string_value(element: cst.BaseElement) -> str | None:
    if not isinstance(element, cst.Element) or not isinstance(
        element.value, cst.SimpleString
    ):
        return None
    try:
        value = ast.literal_eval(element.value.value)
    except (SyntaxError, ValueError):
        return None
    return value if isinstance(value, str) else None


def update_export(
    path: Path, package: str, name: str, from_module: str | None, add: bool
) -> bool:
    before = path.read_text(encoding="utf-8") if path.exists() else ""
    module = _parse_module(before, path)
    body = list(module.body)
    all_index: int | None = None
    exports: list[str] = []
    for index, statement in enumerate(body):
        if (
            not isinstance(statement, cst.SimpleStatementLine)
            or len(statement.body) != 1
        ):
            continue
        small = statement.body[0]
        if not isinstance(small, cst.Assign) or len(small.targets) != 1:
            continue
        if (
            not isinstance(small.targets[0].target, cst.Name)
            or small.targets[0].target.value != "__all__"
        ):
            continue
        if not isinstance(small.value, (cst.List, cst.Tuple)):
            raise WorkspaceError(f"{path}: __all__ must be a literal list or tuple")
        values = [_string_value(element) for element in small.value.elements]
        if any(value is None for value in values):
            raise WorkspaceError(f"{path}: __all__ contains a dynamic entry")
        exports = [value for value in values if value is not None]
        all_index = index
        break

    if add:
        if name not in exports:
            exports.append(name)
    else:
        exports = [item for item in exports if item != name]

    assignment = cst.parse_statement(f"__all__ = {exports!r}\n")
    if all_index is None:
        body.append(assignment)
        all_index = len(body) - 1
    else:
        body[all_index] = assignment

    if add and from_module:
        relative = _relative_import(package, from_module)
        canonical = f"from {relative} import {name} as {name}\n"
        already_present = False
        for statement in body:
            if isinstance(statement, cst.SimpleStatementLine):
                for small in statement.body:
                    if isinstance(small, cst.ImportFrom):
                        imported = _import_from_name(small)
                        if imported == relative and isinstance(small.names, tuple):
                            for alias in small.names:
                                alias_name = get_full_name_for_node(alias.name)
                                as_name = (
                                    alias.asname.name.value if alias.asname else None
                                )
                                if alias_name == name and as_name == name:
                                    already_present = True
        if not already_present:
            body.insert(all_index, cst.parse_statement(canonical))

    after = module.with_changes(body=tuple(body)).code
    _validate_python(after, path)
    if after == before:
        return False
    _atomic_write(path, after.encode())
    return True


def _relative_import(package: str, module: str) -> str:
    prefix = package + "."
    if module.startswith(prefix):
        return "." + module[len(prefix) :]
    return module


def _import_from_name(node: cst.ImportFrom) -> str:
    relative = "." * len(node.relative)
    module = get_full_name_for_node(node.module) if node.module else ""
    return relative + (module or "")
