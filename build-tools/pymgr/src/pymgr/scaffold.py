from __future__ import annotations

import keyword
from dataclasses import dataclass
from pathlib import Path

from pymgr.workspace import Workspace, WorkspaceError, _atomic_write


@dataclass(frozen=True)
class ModuleCreation:
    module: str
    kind: str
    path: Path
    source_root: Path
    created: tuple[Path, ...]


def create_module(
    workspace: Workspace,
    module: str,
    *,
    package: bool = False,
    source_root: str | Path | None = None,
) -> ModuleCreation:
    parts = module.split(".")
    if not module or any(
        not part.isidentifier() or keyword.iskeyword(part) for part in parts
    ):
        raise WorkspaceError(
            "module name must contain dot-separated Python identifiers"
        )

    selected = _select_source_root(workspace, parts[0], source_root)
    package_parts = parts if package else parts[:-1]
    package_directories = [
        selected.joinpath(*parts[:index]) for index in range(1, len(package_parts) + 1)
    ]
    target = (
        selected.joinpath(*parts, "__init__.py")
        if package
        else selected.joinpath(*parts).with_suffix(".py")
    )
    conflicting = (
        selected.joinpath(*parts).with_suffix(".py")
        if package
        else selected.joinpath(*parts)
    )

    if target.exists():
        raise WorkspaceError(f"module already exists: {module} ({target})")
    if conflicting.exists():
        raise WorkspaceError(
            f"module path conflicts with existing {'module' if package else 'package'}: {conflicting}"
        )
    for directory in package_directories:
        if directory.exists() and not directory.is_dir():
            raise WorkspaceError(f"package path is not a directory: {directory}")
        module_conflict = directory.with_suffix(".py")
        if module_conflict.exists():
            raise WorkspaceError(
                f"package path conflicts with existing module: {module_conflict}"
            )

    created_files: list[Path] = []
    created_directories: list[Path] = []
    try:
        if not selected.exists():
            selected.mkdir(parents=True)
            created_directories.append(selected)
        for directory in package_directories:
            if not directory.exists():
                directory.mkdir()
                created_directories.append(directory)
            initializer = directory / "__init__.py"
            if not initializer.exists():
                _atomic_write(initializer, b"__all__ = []\n")
                created_files.append(initializer)
        if target not in created_files:
            _atomic_write(target, b"__all__ = []\n")
            created_files.append(target)
    except BaseException:
        for path in reversed(created_files):
            path.unlink(missing_ok=True)
        for directory in reversed(created_directories):
            try:
                directory.rmdir()
            except OSError:
                pass
        raise

    return ModuleCreation(
        module=module,
        kind="package" if package else "module",
        path=target,
        source_root=selected,
        created=tuple(created_files),
    )


def _select_source_root(
    workspace: Workspace, top_level: str, requested: str | Path | None
) -> Path:
    roots = workspace.source_roots()
    if requested is not None:
        candidate = Path(requested)
        if not candidate.is_absolute():
            candidate = workspace.root / candidate
        candidate = candidate.resolve()
        if candidate not in roots:
            choices = ", ".join(str(path) for path in roots)
            raise WorkspaceError(
                f"source root is not configured: {candidate}; choose one of {choices}"
            )
        return candidate

    matches = [
        root
        for root in roots
        if (root / top_level).exists() or (root / top_level).with_suffix(".py").exists()
    ]
    if len(matches) == 1:
        return matches[0]
    if len(matches) > 1:
        raise WorkspaceError(
            f"top-level module {top_level} exists in multiple source roots; use --source-root"
        )
    if len(roots) == 1:
        return roots[0]
    raise WorkspaceError(
        "workspace has multiple source roots; use --source-root to choose one"
    )
