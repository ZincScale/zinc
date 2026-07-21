from __future__ import annotations

import ast
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import tempfile
import tomllib
from contextlib import contextmanager
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable, Iterator, Sequence


class WorkspaceError(RuntimeError):
    """A workspace operation could not be completed safely."""


@dataclass
class State:
    generation: int = 0
    status: str = "dirty"
    python: str = ""
    python_version: str = ""
    lock_hash: str = ""
    synced_at: str = ""
    error: str = ""


@dataclass(frozen=True)
class Diagnostic:
    level: str
    subject: str
    observed: str
    expected: str
    repair: str


@dataclass(frozen=True)
class ProjectMember:
    root: Path
    name: str
    dependencies: frozenset[str]
    source_roots: tuple[Path, ...]
    packaged: bool


Runner = Callable[[Path, Sequence[str]], subprocess.CompletedProcess[str]]


def _default_runner(
    root: Path, args: Sequence[str]
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        list(args),
        cwd=root,
        check=False,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=30,
    )


def find_workspace(start: Path | None = None) -> Path:
    current = (start or Path.cwd()).resolve()
    if current.is_file():
        current = current.parent
    for candidate in (current, *current.parents):
        if (candidate / "pyproject.toml").is_file():
            return candidate
    raise WorkspaceError(f"no pyproject.toml found from {current}")


def _atomic_write(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        if path.exists():
            os.fchmod(fd, stat.S_IMODE(path.stat().st_mode))
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    except BaseException:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
        raise


def _sha256(path: Path) -> str:
    if not path.exists():
        return ""
    return hashlib.sha256(path.read_bytes()).hexdigest()


class Workspace:
    def __init__(self, root: Path, runner: Runner | None = None) -> None:
        self.root = root.resolve()
        self.runner = runner or _default_runner
        self.state_dir = self.root / ".pymgr"
        self.state_path = self.state_dir / "state.json"
        self.lock_path = self.state_dir / "mutation.lock"

    @classmethod
    def discover(
        cls, start: Path | None = None, runner: Runner | None = None
    ) -> Workspace:
        return cls(find_workspace(start), runner)

    @classmethod
    def create(
        cls,
        target: Path,
        *,
        name: str | None = None,
        python: str = "3.12",
        description: str | None = None,
        runner: Runner | None = None,
    ) -> tuple[Workspace, State]:
        root = target.resolve()
        if root.exists():
            if not root.is_dir():
                raise WorkspaceError(f"new project target is not a directory: {root}")
            if any(root.iterdir()):
                raise WorkspaceError(
                    f"new project target must be absent or empty: {root}"
                )
        actual_runner = runner or _default_runner
        if shutil.which("uv") is None and actual_runner is _default_runner:
            raise WorkspaceError("uv is not installed or not on PATH")
        root.parent.mkdir(parents=True, exist_ok=True)
        command = ["uv", "init", "--package", "--python", python]
        if name:
            command.extend(["--name", name])
        if description:
            command.extend(["--description", description])
        command.append(str(root))
        try:
            result = actual_runner(root.parent, command)
        except subprocess.TimeoutExpired as error:
            raise WorkspaceError(
                f"uv init timed out after {error.timeout} seconds"
            ) from error
        if result.returncode:
            detail = (
                result.stderr.strip()
                or result.stdout.strip()
                or f"exit status {result.returncode}"
            )
            raise WorkspaceError(f"uv init failed: {detail}")

        workspace = cls(root, actual_runner)
        workspace.init()
        workspace._initialize_public_api()
        return workspace, workspace.mutate("sync")

    def config(self) -> dict:
        try:
            with (self.root / "pyproject.toml").open("rb") as stream:
                return tomllib.load(stream)
        except (OSError, tomllib.TOMLDecodeError) as error:
            raise WorkspaceError(f"cannot read pyproject.toml: {error}") from error

    def _member(self, root: Path, config: dict) -> ProjectMember:
        project = config.get("project", {})
        name = str(project.get("name", root.name))
        dependencies: set[str] = set()
        for requirement in project.get("dependencies", []):
            if not isinstance(requirement, str):
                continue
            match = re.match(r"[A-Za-z0-9][A-Za-z0-9._-]*", requirement)
            if match:
                dependencies.add(_normalize_distribution(match.group(0)))
        configured = (
            config.get("tool", {}).get("pymgr", {}).get("source-roots", ["src"])
        )
        if not isinstance(configured, list) or not all(
            isinstance(item, str) for item in configured
        ):
            raise WorkspaceError("[tool.pymgr].source-roots must be a list of paths")
        source_roots = tuple((root / item).resolve() for item in configured)
        for source_root in source_roots:
            if source_root != self.root and self.root not in source_root.parents:
                raise WorkspaceError(
                    f"source root escapes the workspace: {source_root}"
                )
        packaged = "build-system" in config or bool(
            config.get("tool", {}).get("uv", {}).get("package", False)
        )
        return ProjectMember(
            root, name, frozenset(dependencies), source_roots, packaged
        )

    def project_members(self) -> list[ProjectMember]:
        root_config = self.config()
        roots: list[Path] = []
        if "project" in root_config or "pymgr" in root_config.get("tool", {}):
            roots.append(self.root)
        workspace = root_config.get("tool", {}).get("uv", {}).get("workspace", {})
        patterns = workspace.get("members", [])
        excludes = workspace.get("exclude", [])
        if not isinstance(patterns, list) or not all(
            isinstance(item, str) for item in patterns
        ):
            raise WorkspaceError(
                "[tool.uv.workspace].members must be a list of paths or glob patterns"
            )
        if not isinstance(excludes, list) or not all(
            isinstance(item, str) for item in excludes
        ):
            raise WorkspaceError(
                "[tool.uv.workspace].exclude must be a list of paths or glob patterns"
            )
        excluded: set[Path] = set()
        for pattern in excludes:
            excluded.update(path.resolve() for path in self.root.glob(pattern))
        for pattern in patterns:
            for path in self.root.glob(pattern):
                resolved = path.resolve()
                if resolved not in excluded and (resolved / "pyproject.toml").is_file():
                    roots.append(resolved)

        members: list[ProjectMember] = []
        seen: set[Path] = set()
        for root in roots:
            if root in seen:
                continue
            seen.add(root)
            try:
                with (root / "pyproject.toml").open("rb") as stream:
                    config = tomllib.load(stream)
            except (OSError, tomllib.TOMLDecodeError) as error:
                raise WorkspaceError(
                    f"cannot read {root / 'pyproject.toml'}: {error}"
                ) from error
            members.append(self._member(root, config))
        if not members:
            raise WorkspaceError(
                "workspace has no Python project or uv workspace members"
            )
        return members

    def source_roots(self) -> list[Path]:
        return [
            source_root
            for member in self.project_members()
            for source_root in member.source_roots
        ]

    def load_state(self) -> State:
        if not self.state_path.exists():
            return State()
        try:
            raw = json.loads(self.state_path.read_text(encoding="utf-8"))
            return State(
                **{
                    key: raw.get(key, field.default)
                    for key, field in State.__dataclass_fields__.items()
                }
            )
        except (OSError, json.JSONDecodeError, TypeError) as error:
            raise WorkspaceError(f"cannot read {self.state_path}: {error}") from error

    def save_state(self, state: State) -> None:
        payload = (json.dumps(asdict(state), indent=2, sort_keys=True) + "\n").encode()
        _atomic_write(self.state_path, payload)

    def init(self) -> State:
        self.config()
        self.state_dir.mkdir(parents=True, exist_ok=True)
        _atomic_write(self.state_dir / ".gitignore", b"*\n!.gitignore\n")
        source = self.root / "pyproject.toml"
        text = source.read_text(encoding="utf-8")
        if "[tool.pymgr]" not in text:
            suffix = "" if text.endswith("\n") else "\n"
            text += suffix + '\n[tool.pymgr]\nsource-roots = ["src"]\n'
            _atomic_write(source, text.encode())
        state = self.load_state()
        self.save_state(state)
        return state

    def _initialize_public_api(self) -> None:
        for source_root in self.source_roots():
            if not source_root.is_dir():
                continue
            for package in source_root.iterdir():
                initializer = package / "__init__.py"
                if package.is_dir() and initializer.is_file():
                    text = initializer.read_text(encoding="utf-8")
                    try:
                        tree = ast.parse(text, filename=str(initializer))
                    except SyntaxError as error:
                        raise WorkspaceError(
                            f"cannot initialize public API in {initializer}: {error}"
                        ) from error
                    declares_all = any(
                        (
                            isinstance(node, (ast.Assign, ast.AnnAssign))
                            and any(
                                isinstance(target, ast.Name) and target.id == "__all__"
                                for target in (
                                    node.targets
                                    if isinstance(node, ast.Assign)
                                    else [node.target]
                                )
                            )
                        )
                        for node in tree.body
                    )
                    if not declares_all:
                        suffix = "" if not text or text.endswith("\n") else "\n"
                        _atomic_write(
                            initializer,
                            f"{text}{suffix}\n__all__ = []\n".lstrip("\n").encode(),
                        )

    @contextmanager
    def mutation_lock(self) -> Iterator[None]:
        self.state_dir.mkdir(parents=True, exist_ok=True)
        try:
            fd = os.open(self.lock_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except FileExistsError as error:
            detail = self.lock_path.read_text(
                encoding="utf-8", errors="replace"
            ).strip()
            raise WorkspaceError(
                f"workspace mutation already in progress ({detail or 'unknown owner'})"
            ) from error
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as stream:
                stream.write(
                    f"pid={os.getpid()} started={datetime.now(timezone.utc).isoformat()}\n"
                )
            yield
        finally:
            self.lock_path.unlink(missing_ok=True)

    def _run(self, args: Sequence[str]) -> str:
        try:
            result = self.runner(self.root, args)
        except subprocess.TimeoutExpired as error:
            raise WorkspaceError(
                f"{' '.join(args)} timed out after {error.timeout} seconds"
            ) from error
        if result.returncode:
            detail = (
                result.stderr.strip()
                or result.stdout.strip()
                or f"exit status {result.returncode}"
            )
            raise WorkspaceError(f"{' '.join(args)} failed: {detail}")
        return result.stdout.strip()

    def probe_module(self, module: str) -> dict:
        script = """
import importlib.metadata
import importlib.util
import json
import sys

name = sys.argv[1]
try:
    spec = importlib.util.find_spec(name)
except (ImportError, AttributeError, ValueError) as error:
    print(json.dumps({"error": str(error)}))
    raise SystemExit(0)
packages = importlib.metadata.packages_distributions()
top = name.split(".", 1)[0]
print(json.dumps({
    "module": name,
    "origin": None if spec is None else spec.origin,
    "locations": [] if spec is None or spec.submodule_search_locations is None else list(spec.submodule_search_locations),
    "loader": None if spec is None or spec.loader is None else type(spec.loader).__name__,
    "distributions": packages.get(top, []),
    "stdlib": top in sys.stdlib_module_names,
}))
"""
        output = self._run(
            ["uv", "run", "--locked", "--no-sync", "python", "-c", script, module]
        )
        try:
            return json.loads(output.splitlines()[-1])
        except (IndexError, json.JSONDecodeError) as error:
            raise WorkspaceError(
                f"module probe returned invalid output: {output!r}"
            ) from error

    def environment_inventory(self) -> dict:
        script = """
import importlib.metadata
import json
import pathlib
import re
import sys

distributions = {}
for distribution in importlib.metadata.distributions():
    name = distribution.metadata.get("Name")
    if not name:
        continue
    normalized = re.sub(r"[-_.]+", "-", name).lower()
    direct_url = None
    raw_url = distribution.read_text("direct_url.json")
    if raw_url:
        try:
            direct_url = json.loads(raw_url)
        except json.JSONDecodeError:
            direct_url = {"invalid": True}
    distributions[normalized] = {
        "name": name,
        "version": distribution.version,
        "location": str(pathlib.Path(distribution.locate_file("")).resolve()),
        "direct_url": direct_url,
    }
print(json.dumps({
    "python": sys.executable,
    "version": sys.version.split()[0],
    "prefix": sys.prefix,
    "sys_path": sys.path,
    "distributions": distributions,
}))
"""
        output = self._run(
            ["uv", "run", "--locked", "--no-sync", "python", "-c", script]
        )
        try:
            return json.loads(output.splitlines()[-1])
        except (IndexError, json.JSONDecodeError) as error:
            raise WorkspaceError(
                f"environment inventory returned invalid output: {output!r}"
            ) from error

    def _restore(self, snapshots: dict[Path, bytes | None]) -> None:
        for path, content in snapshots.items():
            if content is None:
                path.unlink(missing_ok=True)
            else:
                _atomic_write(path, content)

    def _validate_environment(self) -> tuple[str, str]:
        script = (
            "import importlib.metadata,json,sys;"
            "print(json.dumps({'python':sys.executable,'version':sys.version.split()[0],"
            "'distributions':len(list(importlib.metadata.distributions()))}))"
        )
        output = self._run(
            ["uv", "run", "--locked", "--no-sync", "python", "-c", script]
        )
        try:
            result = json.loads(output.splitlines()[-1])
            return str(result["python"]), str(result["version"])
        except (IndexError, KeyError, TypeError, json.JSONDecodeError) as error:
            raise WorkspaceError(
                f"environment validation returned invalid output: {output!r}"
            ) from error

    def mutate(self, operation: str, packages: Sequence[str] = ()) -> State:
        if shutil.which("uv") is None and self.runner is _default_runner:
            raise WorkspaceError("uv is not installed or not on PATH")
        metadata = [self.root / "pyproject.toml", self.root / "uv.lock"]
        snapshots = {
            path: path.read_bytes() if path.exists() else None for path in metadata
        }
        previous = self.load_state()

        with self.mutation_lock():
            working = State(**asdict(previous))
            working.status = "resolving"
            working.error = ""
            self.save_state(working)
            try:
                if operation == "sync":
                    self._run(["uv", "lock"])
                elif operation == "check":
                    self._run(["uv", "lock", "--check"])
                elif operation == "add":
                    if not packages:
                        raise WorkspaceError("add requires at least one package")
                    self._run(["uv", "add", "--no-sync", *packages])
                elif operation == "remove":
                    if not packages:
                        raise WorkspaceError("remove requires at least one package")
                    self._run(["uv", "remove", "--no-sync", *packages])
                elif operation == "update":
                    command = ["uv", "lock", "--upgrade"]
                    if packages:
                        command = ["uv", "lock"]
                        for package in packages:
                            command.extend(["--upgrade-package", package])
                    self._run(command)
                else:
                    raise WorkspaceError(f"unknown mutation operation: {operation}")

                working.status = "syncing"
                self.save_state(working)
                self._run(["uv", "sync", "--locked"])
                working.status = "validating"
                self.save_state(working)
                interpreter, version = self._validate_environment()
                working.generation = previous.generation + 1
                working.status = "ready"
                working.python = interpreter
                working.python_version = version
                working.lock_hash = _sha256(self.root / "uv.lock")
                working.synced_at = datetime.now(timezone.utc).isoformat()
                self.save_state(working)
                return working
            except BaseException as error:
                self._restore(snapshots)
                rollback_error = ""
                if snapshots[self.root / "uv.lock"] is not None:
                    try:
                        self._run(["uv", "sync", "--locked"])
                    except BaseException as rollback:
                        rollback_error = f"; rollback sync failed: {rollback}"
                failed = State(**asdict(previous))
                if rollback_error:
                    failed.status = "broken"
                failed.error = f"{error}{rollback_error}"
                self.save_state(failed)
                if isinstance(error, WorkspaceError):
                    raise WorkspaceError(f"{error}{rollback_error}") from error
                raise

    def status(self) -> tuple[State, bool]:
        state = self.load_state()
        current_hash = _sha256(self.root / "uv.lock")
        synchronized = (
            state.status == "ready"
            and state.lock_hash == current_hash
            and bool(current_hash)
        )
        return state, synchronized

    def run_command(self, command: Sequence[str]) -> subprocess.CompletedProcess[str]:
        workload = list(command[1:] if command and command[0] == "--" else command)
        if not workload:
            raise WorkspaceError("run requires a command after --")
        _, synchronized = self.status()
        if not synchronized:
            raise WorkspaceError(
                "workspace is not synchronized; run pymgr sync before pymgr run"
            )
        if shutil.which("uv") is None and self.runner is _default_runner:
            raise WorkspaceError("uv is not installed or not on PATH")
        arguments = ["uv", "run", "--locked", "--no-sync", *workload]
        if self.runner is not _default_runner:
            return self.runner(self.root, arguments)
        try:
            return subprocess.run(arguments, cwd=self.root, check=False, text=True)
        except OSError as error:
            raise WorkspaceError(f"cannot run {workload[0]}: {error}") from error

    def doctor(self) -> list[Diagnostic]:
        findings: list[Diagnostic] = []
        try:
            self.config()
        except WorkspaceError as error:
            findings.append(
                Diagnostic(
                    "error",
                    "pyproject.toml",
                    str(error),
                    "valid TOML",
                    "repair pyproject.toml",
                )
            )
            return findings

        if shutil.which("uv") is None:
            findings.append(
                Diagnostic("error", "uv", "not found", "uv on PATH", "install uv")
            )
        state = self.load_state()
        lock = self.root / "uv.lock"
        if not lock.exists():
            findings.append(
                Diagnostic(
                    "error",
                    "uv.lock",
                    "missing",
                    "checked-in lockfile",
                    "run pymgr sync",
                )
            )
        elif state.lock_hash != _sha256(lock):
            findings.append(
                Diagnostic(
                    "error",
                    "lock hash",
                    _sha256(lock),
                    state.lock_hash or "recorded synchronized hash",
                    "run pymgr sync",
                )
            )
        if state.status != "ready":
            findings.append(
                Diagnostic(
                    "error", "workspace state", state.status, "ready", "run pymgr sync"
                )
            )
        if state.python and not Path(state.python).exists():
            findings.append(
                Diagnostic(
                    "error",
                    "interpreter",
                    state.python,
                    "existing synchronized interpreter",
                    "run pymgr sync",
                )
            )
        for source_root in self.source_roots():
            if not source_root.is_dir():
                findings.append(
                    Diagnostic(
                        "error",
                        "source root",
                        str(source_root),
                        "existing directory",
                        "create it or update [tool.pymgr]",
                    )
                )
        if shutil.which("uv") is not None and lock.exists() and state.status == "ready":
            try:
                inventory = self.environment_inventory()
                if state.python and inventory.get("python") != state.python:
                    findings.append(
                        Diagnostic(
                            "error",
                            "active interpreter",
                            str(inventory.get("python")),
                            state.python,
                            "run pymgr sync",
                        )
                    )
                if (
                    state.python_version
                    and inventory.get("version") != state.python_version
                ):
                    findings.append(
                        Diagnostic(
                            "error",
                            "Python version",
                            str(inventory.get("version")),
                            state.python_version,
                            "run pymgr sync",
                        )
                    )
                installed = inventory.get("distributions", {})
                for member in self.project_members():
                    normalized = _normalize_distribution(member.name)
                    if member.packaged and normalized not in installed:
                        findings.append(
                            Diagnostic(
                                "error",
                                f"workspace member {member.name}",
                                "not installed",
                                "editable synchronized installation",
                                "run pymgr sync",
                            )
                        )
                    elif member.packaged:
                        direct_url = installed[normalized].get("direct_url") or {}
                        editable = direct_url.get("dir_info", {}).get("editable", False)
                        if not editable:
                            findings.append(
                                Diagnostic(
                                    "warning",
                                    f"workspace member {member.name}",
                                    str(
                                        direct_url
                                        or "installed without direct_url.json"
                                    ),
                                    "editable installation from the workspace",
                                    "run pymgr sync",
                                )
                            )
                try:
                    self._run(["uv", "pip", "check"])
                except WorkspaceError as error:
                    findings.append(
                        Diagnostic(
                            "error",
                            "installed distributions",
                            str(error),
                            "compatible installed dependency metadata",
                            "run pymgr sync",
                        )
                    )
            except WorkspaceError as error:
                findings.append(
                    Diagnostic(
                        "error",
                        "environment inventory",
                        str(error),
                        "queryable synchronized environment",
                        "run pymgr sync",
                    )
                )
        return findings


def _normalize_distribution(name: str) -> str:
    return re.sub(r"[-_.]+", "-", name).lower()
